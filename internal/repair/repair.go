// Package repair owns the repair-request domain: persistence plus the workflow
// state machine (see states.go).
//
// This is a deliberately simple "current state" model — no event/audit log.
// The request row (status, owner, timestamps) is the single source of truth,
// and the state machine answers "what can I do next" for any authenticated
// user at any time.
package repair

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
	ErrNotFound        = errors.New("repair: request not found")
	ErrInvalidStatus   = errors.New("repair: invalid status")
	ErrInvalidPriority = errors.New("repair: invalid priority")
	ErrBadTransition   = errors.New("repair: illegal status transition")
	ErrValidation      = errors.New("repair: missing required field")
	// ErrClaimFailed means the request was no longer unclaimed when this
	// caller tried to claim it — another user won the race. The caller is
	// expected to surface this on screen.
	ErrClaimFailed = errors.New("repair: request was already claimed")
	// ErrNotOwnable means take-over was attempted on a request that isn't in
	// an owned state (it's still unclaimed, or already closed).
	ErrNotOwnable = errors.New("repair: request cannot be taken over in its current state")
	// ErrUseClaim means a plain transition tried to do received -> in_repair,
	// which must go through Claim so ownership is set atomically.
	ErrUseClaim = errors.New("repair: take ownership via claim, not a plain transition")
)

var validPriorities = map[string]bool{
	"low": true, "normal": true, "high": true, "urgent": true,
}

type Request struct {
	ID             int64  `json:"id"`
	MachineID      int64  `json:"machine_id"`
	MachineName    string `json:"machine_name"` // joined from machines for display
	ProblemSummary string `json:"problem_summary"`
	ProblemDetail  string `json:"problem_detail"`
	Status         Status `json:"status"`
	Priority       string `json:"priority"`
	// Owner. AssignedTo is NULL while 'received'. AssignedUsername is joined
	// in for display so non-admin users (who can't list users) can still see
	// who owns a request.
	AssignedTo       *int64     `json:"assigned_to,omitempty"`
	AssignedUsername *string    `json:"assigned_username,omitempty"`
	CreatedBy        *int64     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ClosedAt         *time.Time `json:"closed_at,omitempty"`
}

// CreateInput carries the fields a caller may set when logging a request. The
// machine is referenced by id — callers resolve the user-typed machine name to
// an id via the machines registry (find-or-create) before calling Create.
type CreateInput struct {
	MachineID      int64
	ProblemSummary string
	ProblemDetail  string
	Priority       string // optional; defaults to "normal"
}

type Service struct {
	db *db.DB
}

func NewService(d *db.DB) *Service { return &Service{db: d} }

const selectCols = `
	SELECT r.id, m.id, m.name, r.problem_summary, r.problem_detail,
	       r.status, r.priority, r.assigned_to, u.username, r.created_by,
	       r.created_at, r.updated_at, r.closed_at
	FROM repair_requests r
	JOIN machines m ON m.id = r.machine_id
	LEFT JOIN users u ON u.id = r.assigned_to`

// Create logs a new request in StatusReceived (unclaimed). MachineID must
// already be resolved (find-or-create) by the caller.
func (s *Service) Create(ctx context.Context, in CreateInput, createdBy int64) (*Request, error) {
	in.ProblemSummary = strings.TrimSpace(in.ProblemSummary)
	if in.MachineID <= 0 || in.ProblemSummary == "" {
		return nil, fmt.Errorf("%w: machine and problem_summary are required", ErrValidation)
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !validPriorities[in.Priority] {
		return nil, ErrInvalidPriority
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO repair_requests
			(machine_id, problem_summary, problem_detail,
			 status, priority, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.MachineID, in.ProblemSummary, strings.TrimSpace(in.ProblemDetail),
		string(StatusReceived), in.Priority, createdBy, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert request: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

// ListFilter narrows the List query. Zero value lists everything.
type ListFilter struct {
	Status     string
	AssignedTo *int64
	OpenOnly   bool
}

// List returns requests newest-activity-first, subject to filter.
func (s *Service) List(ctx context.Context, f ListFilter) ([]Request, error) {
	var (
		where []string
		args  []any
	)
	if f.Status != "" {
		where = append(where, "r.status = ?")
		args = append(args, f.Status)
	}
	if f.AssignedTo != nil {
		where = append(where, "r.assigned_to = ?")
		args = append(args, *f.AssignedTo)
	}
	if f.OpenOnly {
		where = append(where, "r.closed_at IS NULL")
	}
	q := selectCols
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY r.updated_at DESC, r.id DESC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Get returns a single request by id.
func (s *Service) Get(ctx context.Context, id int64) (*Request, error) {
	r, err := scanRequest(s.db.QueryRowContext(ctx, selectCols+" WHERE r.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// Claim performs the received -> in_repair move and sets the actor as owner,
// atomically and with first-wins semantics: the UPDATE only matches a row
// still in 'received', so exactly one concurrent caller succeeds. A caller
// that gets ErrClaimFailed must report it (the request is already owned).
func (s *Service) Claim(ctx context.Context, id, actor int64) (*Request, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE repair_requests SET status = ?, assigned_to = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		string(StatusInRepair), actor, now, id, string(StatusReceived),
	)
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Distinguish "no such request" from "already claimed".
		if _, err := s.Get(ctx, id); err != nil {
			return nil, err // ErrNotFound
		}
		return nil, ErrClaimFailed
	}
	return s.Get(ctx, id)
}

// TakeOver pulls ownership of an already-owned request to the actor. Allowed
// only while the request is in an owned, non-terminal state (in_repair or
// awaiting_parts). Pull-only: a user makes themselves the owner; ownership is
// never pushed to anyone else. No status change.
func (s *Service) TakeOver(ctx context.Context, id, actor int64) (*Request, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE repair_requests SET assigned_to = ?, updated_at = ?
		 WHERE id = ? AND status IN (?, ?)`,
		actor, now, id, string(StatusInRepair), string(StatusAwaitingParts),
	)
	if err != nil {
		return nil, fmt.Errorf("take over: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.Get(ctx, id); err != nil {
			return nil, err // ErrNotFound
		}
		return nil, ErrNotOwnable
	}
	return s.Get(ctx, id)
}

// Transition moves a request along the state machine for every edge EXCEPT
// the claim edge (received -> in_repair), which must go through Claim. Stamps
// closed_at when entering a terminal state.
func (s *Service) Transition(ctx context.Context, id int64, to Status) (*Request, error) {
	if !validStatus(to) {
		return nil, ErrInvalidStatus
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.Status == StatusReceived && to == StatusInRepair {
		return nil, ErrUseClaim
	}
	if !CanTransition(cur.Status, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrBadTransition, cur.Status, to)
	}
	now := time.Now().Unix()
	if IsTerminal(to) {
		_, err = s.db.ExecContext(ctx,
			`UPDATE repair_requests SET status = ?, updated_at = ?, closed_at = ? WHERE id = ?`,
			string(to), now, now, id)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE repair_requests SET status = ?, updated_at = ?, closed_at = NULL WHERE id = ?`,
			string(to), now, id)
	}
	if err != nil {
		return nil, fmt.Errorf("transition: %w", err)
	}
	return s.Get(ctx, id)
}

// SetPriority changes the triage priority. Plain field update, no transition.
func (s *Service) SetPriority(ctx context.Context, id int64, priority string) (*Request, error) {
	if !validPriorities[priority] {
		return nil, ErrInvalidPriority
	}
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE repair_requests SET priority = ?, updated_at = ? WHERE id = ?`,
		priority, time.Now().Unix(), id); err != nil {
		return nil, fmt.Errorf("set priority: %w", err)
	}
	return s.Get(ctx, id)
}

func scanRequest(sc interface{ Scan(...any) error }) (*Request, error) {
	var (
		r          Request
		status     string
		assignedTo sql.NullInt64
		assignedU  sql.NullString
		createdBy  sql.NullInt64
		createdAt  int64
		updatedAt  int64
		closedAt   sql.NullInt64
	)
	if err := sc.Scan(
		&r.ID, &r.MachineID, &r.MachineName, &r.ProblemSummary, &r.ProblemDetail,
		&status, &r.Priority, &assignedTo, &assignedU, &createdBy,
		&createdAt, &updatedAt, &closedAt,
	); err != nil {
		return nil, err
	}
	r.Status = Status(status)
	if assignedTo.Valid {
		r.AssignedTo = &assignedTo.Int64
	}
	if assignedU.Valid {
		r.AssignedUsername = &assignedU.String
	}
	if createdBy.Valid {
		r.CreatedBy = &createdBy.Int64
	}
	r.CreatedAt = time.Unix(createdAt, 0)
	r.UpdatedAt = time.Unix(updatedAt, 0)
	if closedAt.Valid {
		t := time.Unix(closedAt.Int64, 0)
		r.ClosedAt = &t
	}
	return &r, nil
}
