// Package auth handles user accounts, password hashing, and session tokens.
//
// Two roles exist:
//   - "admin": can do everything a user can, PLUS create and manage users.
//   - "user":  full access to the repair workflow (create/edit/transition
//     requests), but cannot manage other users.
//
// The same credentials cover both the embedded SPA (cookie session) and any
// future API client (the cookie value doubles as a bearer token).
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"starfighter-workflow/internal/db"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	// SessionTTL is the sliding inactivity window. Each authenticated
	// request extends expires_at to now+SessionTTL; a gap longer than this
	// drops the session. 12 hours comfortably covers a repair-shop shift
	// without a mid-day re-login, while still expiring an unattended
	// browser overnight.
	SessionTTL = 12 * time.Hour

	bcryptCost = 12

	minPasswordLen = 8
)

var (
	ErrUserExists       = errors.New("auth: user already exists")
	ErrInvalidLogin     = errors.New("auth: invalid username or password")
	ErrSessionNotFound  = errors.New("auth: session not found or expired")
	ErrUserNotFound     = errors.New("auth: user not found")
	ErrLastAdmin        = errors.New("auth: cannot remove or demote the last admin")
	ErrBadRole          = errors.New("auth: role must be admin or user")
	ErrPasswordTooShort = fmt.Errorf("auth: password must be at least %d characters", minPasswordLen)
)

func validRole(role string) bool { return role == RoleAdmin || role == RoleUser }

type User struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	DisplayName string     `json:"display_name"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

type Service struct {
	db *db.DB
}

func NewService(d *db.DB) *Service { return &Service{db: d} }

// CreateUser inserts a new user with a bcrypt-hashed password.
func (s *Service) CreateUser(ctx context.Context, username, password, role, displayName string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, errors.New("auth: username required")
	}
	if !validRole(role) {
		return nil, ErrBadRole
	}
	if len(password) < minPasswordLen {
		return nil, ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username, password_hash, role, display_name, created_at) VALUES (?, ?, ?, ?, ?)`,
		username, string(hash), role, strings.TrimSpace(displayName), now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	id, _ := res.LastInsertId()
	return &User{
		ID:          id,
		Username:    username,
		Role:        role,
		DisplayName: strings.TrimSpace(displayName),
		CreatedAt:   time.Unix(now, 0),
	}, nil
}

// CountUsers returns the number of registered users; drives first-boot setup.
func (s *Service) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// List returns every registered user, ordered by username.
func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, role, display_name, created_at, last_login_at
		FROM users
		ORDER BY username COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// FindByUsername resolves a username (case-insensitive) to a User.
func (s *Service) FindByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, role, display_name, created_at, last_login_at
		FROM users WHERE username = ? COLLATE NOCASE
	`, strings.TrimSpace(username))
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

// Delete removes a user. Sessions cascade. Refuses to delete the last admin.
func (s *Service) Delete(ctx context.Context, id int64) error {
	u, err := s.findByID(ctx, id)
	if err != nil {
		return err
	}
	if u.Role == RoleAdmin {
		n, err := s.countAdmins(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastAdmin
		}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ChangePassword sets a fresh bcrypt hash. Existing sessions stay valid.
func (s *Service) ChangePassword(ctx context.Context, id int64, password string) error {
	if len(password) < minPasswordLen {
		return ErrPasswordTooShort
	}
	if _, err := s.findByID(ctx, id); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
	return err
}

// ChangeRole flips a user between admin and user. Refuses to demote the last
// admin.
func (s *Service) ChangeRole(ctx context.Context, id int64, role string) error {
	if !validRole(role) {
		return ErrBadRole
	}
	u, err := s.findByID(ctx, id)
	if err != nil {
		return err
	}
	if u.Role == RoleAdmin && role != RoleAdmin {
		n, err := s.countAdmins(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastAdmin
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

func (s *Service) findByID(ctx context.Context, id int64) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, role, display_name, created_at, last_login_at
		FROM users WHERE id = ?
	`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

func (s *Service) countAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM users WHERE role = ?`, RoleAdmin,
	).Scan(&n)
	return n, err
}

// Login verifies credentials and issues a session token on success.
func (s *Service) Login(ctx context.Context, username, password string) (token string, user *User, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, display_name, created_at FROM users WHERE username = ? COLLATE NOCASE`,
		strings.TrimSpace(username),
	)
	var (
		id          int64
		uname       string
		hash        string
		role        string
		displayName string
		createdAt   int64
	)
	if err := row.Scan(&id, &uname, &hash, &role, &displayName, &createdAt); err != nil {
		return "", nil, ErrInvalidLogin
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", nil, ErrInvalidLogin
	}

	tok, err := newSessionToken()
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tok, id, now.Unix(), now.Add(SessionTTL).Unix(),
	); err != nil {
		return "", nil, fmt.Errorf("insert session: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = ? WHERE id = ?`, now.Unix(), id,
	); err != nil {
		_ = err // non-fatal; login already succeeded
	}

	return tok, &User{
		ID:          id,
		Username:    uname,
		Role:        role,
		DisplayName: displayName,
		CreatedAt:   time.Unix(createdAt, 0),
	}, nil
}

// Lookup resolves a session token to its user, sliding the expiry forward by
// SessionTTL on every successful validation. The UPDATE both rejects expired
// tokens (no row matches expires_at > now) and extends live ones atomically.
func (s *Service) Lookup(ctx context.Context, token string) (*User, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET expires_at = ? WHERE token = ? AND expires_at > ?`,
		now.Add(SessionTTL).Unix(), token, now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("slide session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSessionNotFound
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, u.display_name, u.created_at, u.last_login_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?
	`, token)
	u, err := scanUser(row)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	return u, nil
}

// WipeAllSessions deletes every session row, returning the count. Called at
// startup on a detected version change so everyone re-authenticates against
// new code after a deploy.
func (s *Service) WipeAllSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Logout removes a session by token. No-op if absent.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// PurgeExpired removes expired sessions. Call periodically.
func (s *Service) PurgeExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	return err
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanUser(sc scanner) (*User, error) {
	var (
		u           User
		createdAt   int64
		lastLoginAt sql.NullInt64
	)
	if err := sc.Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &createdAt, &lastLoginAt); err != nil {
		return nil, err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	if lastLoginAt.Valid {
		t := time.Unix(lastLoginAt.Int64, 0)
		u.LastLoginAt = &t
	}
	return &u, nil
}

func newSessionToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
