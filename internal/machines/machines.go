// Package machines is the accumulator registry of machines we know about.
//
// It is intentionally NOT a curated fleet list. Machines are keyed by name
// (case-insensitive unique) and added on demand: when a fault is reported for
// a name we've never seen, FindOrCreate inserts it. The reporting UI offers
// existing names as type-ahead suggestions (Search) but accepts free text, so
// a human reuses an existing machine or coins a new one in the same field.
package machines

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"starfighter-workflow/internal/db"
)

var (
	ErrNotFound  = errors.New("machines: not found")
	ErrEmptyName = errors.New("machines: name is required")
)

type Machine struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedBy *int64    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	db *db.DB
}

func NewService(d *db.DB) *Service { return &Service{db: d} }

// FindOrCreate returns the machine with the given name, creating it if absent.
// Match is exact and case-insensitive (after trimming). `created` reports
// whether a new row was inserted. createdBy may be 0 for anonymous/harvested
// callers (stored as NULL).
func (s *Service) FindOrCreate(ctx context.Context, name string, createdBy int64) (m *Machine, created bool, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false, ErrEmptyName
	}
	if existing, err := s.findByName(ctx, name); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	now := time.Now().Unix()
	var createdByArg any
	if createdBy > 0 {
		createdByArg = createdBy
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO machines(name, created_by, created_at) VALUES (?, ?, ?)`,
		name, createdByArg, now)
	if err != nil {
		// Lost a race with a concurrent insert of the same name — re-find.
		if strings.Contains(err.Error(), "UNIQUE") {
			if existing, ferr := s.findByName(ctx, name); ferr == nil {
				return existing, false, nil
			}
		}
		return nil, false, fmt.Errorf("insert machine: %w", err)
	}
	id, _ := res.LastInsertId()
	out := &Machine{ID: id, Name: name, CreatedAt: time.Unix(now, 0)}
	if createdBy > 0 {
		out.CreatedBy = &createdBy
	}
	return out, true, nil
}

// Search returns up to limit machines for a type-ahead. Empty query returns
// the alphabetically-first machines; otherwise it substring-matches the name.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]Machine, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	query = strings.TrimSpace(query)

	var (
		rows *sql.Rows
		err  error
	)
	if query == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, created_by, created_at FROM machines
			 ORDER BY name COLLATE NOCASE LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, created_by, created_at FROM machines
			 WHERE name LIKE ? ESCAPE '\' ORDER BY name COLLATE NOCASE LIMIT ?`,
			"%"+escapeLike(query)+"%", limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Machine{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// Get returns one machine by id.
func (s *Service) Get(ctx context.Context, id int64) (*Machine, error) {
	m, err := scan(s.db.QueryRowContext(ctx,
		`SELECT id, name, created_by, created_at FROM machines WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Service) findByName(ctx context.Context, name string) (*Machine, error) {
	m, err := scan(s.db.QueryRowContext(ctx,
		`SELECT id, name, created_by, created_at FROM machines WHERE name = ? COLLATE NOCASE`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func scan(sc interface{ Scan(...any) error }) (*Machine, error) {
	var (
		m         Machine
		createdBy sql.NullInt64
		createdAt int64
	)
	if err := sc.Scan(&m.ID, &m.Name, &createdBy, &createdAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		m.CreatedBy = &createdBy.Int64
	}
	m.CreatedAt = time.Unix(createdAt, 0)
	return &m, nil
}

// escapeLike neutralizes LIKE wildcards in user input so a query of "100%"
// doesn't match everything. Pairs with ESCAPE '\' in the query.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
