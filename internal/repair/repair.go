// Package repair owns the repair-request domain: persistence, the workflow
// state machine (see states.go), and the immutable per-request event log.
//
// Every mutation that changes a request appends a row to request_events so the
// history tab and audit trail are always complete. The HTTP layer is a thin
// wrapper over this service.
package repair

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
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
)

// Priorities accepted by the API (mirrors the CHECK constraint in 0001_init).
var validPriorities = map[string]bool{
	"low": true, "normal": true, "high": true, "urgent": true,
}

type Request struct {
	ID              int64      `json:"id"`
	GameTitle       string     `json:"game_title"`
	CabinetRef      string     `json:"cabinet_ref"`
	ProblemSummary  string     `json:"problem_summary"`
	ProblemDetail   string     `json:"problem_detail"`
	ReporterName    string     `json:"reporter_name"`
	ReporterContact string     `json:"reporter_contact"`
	Status          Status     `json:"status"`
	Priority        string     `json:"priority"`
	AssignedTo      *int64     `json:"assigned_to,omitempty"`
	CreatedBy       *int64     `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ClosedAt        *time.Time `json:"closed_at,omitempty"`
}

type Event struct {
	ID        int64     `json:"id"`
	RequestID int64     `json:"request_id"`
	ActorID   *int64    `json:"actor_id,omitempty"`
	Kind      string    `json:"kind"`
	FromValue *string   `json:"from_value,omitempty"`
	ToValue   *string   `json:"to_value,omitempty"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateInput carries the fields a caller may set when opening a request.
type CreateInput struct {
	GameTitle       string
	CabinetRef      string
	ProblemSummary  string
	ProblemDetail   string
	ReporterName    string
	ReporterContact string
	Priority        string // optional; defaults to "normal"
}

type Service struct {
	db *db.DB
}

func NewService(d *db.DB) *Service { return &Service{db: d} }

// Create opens a new request in StatusReceived and logs the 'created' event.
func (s *Service) Create(ctx context.Context, in CreateInput, actorID int64) (*Request, error) {
	in.GameTitle = strings.TrimSpace(in.GameTitle)
	in.ProblemSummary = strings.TrimSpace(in.ProblemSummary)
	if in.GameTitle == "" || in.ProblemSummary == "" {
		return nil, fmt.Errorf("%w: game_title and problem_summary are required", ErrValidation)
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !validPriorities[in.Priority] {
		return nil, ErrInvalidPriority
	}

	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO repair_requests
			(game_title, cabinet_ref, problem_summary, problem_detail,
			 reporter_name, reporter_contact, status, priority,
			 created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		in.GameTitle, strings.TrimSpace(in.CabinetRef), in.ProblemSummary,
		strings.TrimSpace(in.ProblemDetail), strings.TrimSpace(in.ReporterName),
		strings.TrimSpace(in.ReporterContact), string(StatusReceived), in.Priority,
		actorID, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert request: %w", err)
	}
	id, _ := res.LastInsertId()

	if err := appendEvent(ctx, tx, id, &actorID, "created", nil, ptr(string(StatusReceived)), ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// ListFilter narrows the List query. Zero value lists everything.
type ListFilter struct {
	Status     string // exact status, or "" for any
	AssignedTo *int64 // filter by assignee
	OpenOnly   bool   // exclude terminal statuses
}

// List returns requests newest-activity-first, subject to filter.
func (s *Service) List(ctx context.Context, f ListFilter) ([]Request, error) {
	var (
		where []string
		args  []any
	)
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.AssignedTo != nil {
		where = append(where, "assigned_to = ?")
		args = append(args, *f.AssignedTo)
	}
	if f.OpenOnly {
		where = append(where, "closed_at IS NULL")
	}
	q := `SELECT id, game_title, cabinet_ref, problem_summary, problem_detail,
	             reporter_name, reporter_contact, status, priority, assigned_to,
	             created_by, created_at, updated_at, closed_at
	      FROM repair_requests`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY updated_at DESC, id DESC"

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
	row := s.db.QueryRowContext(ctx, `
		SELECT id, game_title, cabinet_ref, problem_summary, problem_detail,
		       reporter_name, reporter_contact, status, priority, assigned_to,
		       created_by, created_at, updated_at, closed_at
		FROM repair_requests WHERE id = ?`, id)
	r, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// Events returns the full event log for a request, oldest first.
func (s *Service) Events(ctx context.Context, id int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, request_id, actor_id, kind, from_value, to_value, note, created_at
		FROM request_events WHERE request_id = ? ORDER BY id ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var (
			e         Event
			actorID   sql.NullInt64
			fromVal   sql.NullString
			toVal     sql.NullString
			createdAt int64
		)
		if err := rows.Scan(&e.ID, &e.RequestID, &actorID, &e.Kind, &fromVal, &toVal, &e.Note, &createdAt); err != nil {
			return nil, err
		}
		if actorID.Valid {
			e.ActorID = &actorID.Int64
		}
		if fromVal.Valid {
			e.FromValue = &fromVal.String
		}
		if toVal.Valid {
			e.ToValue = &toVal.String
		}
		e.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Transition moves a request to a new status if the state machine permits it,
// stamping closed_at when entering a terminal state and logging the change.
func (s *Service) Transition(ctx context.Context, id int64, to Status, actorID int64, note string) (*Request, error) {
	if !validStatus(to) {
		return nil, ErrInvalidStatus
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !CanTransition(cur.Status, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrBadTransition, cur.Status, to)
	}

	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if IsTerminal(to) {
		_, err = tx.ExecContext(ctx,
			`UPDATE repair_requests SET status = ?, updated_at = ?, closed_at = ? WHERE id = ?`,
			string(to), now, now, id)
	} else {
		// Re-opening from a terminal state isn't reachable (terminals have
		// no outgoing transitions), so clearing closed_at here is only for
		// the on_hold/active paths where it's already NULL — harmless.
		_, err = tx.ExecContext(ctx,
			`UPDATE repair_requests SET status = ?, updated_at = ?, closed_at = NULL WHERE id = ?`,
			string(to), now, id)
	}
	if err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}
	if err := appendEvent(ctx, tx, id, &actorID, "status_change", ptr(string(cur.Status)), ptr(string(to)), strings.TrimSpace(note)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Assign sets (or clears, when assignee is nil) the assignee and logs it.
func (s *Service) Assign(ctx context.Context, id int64, assignee *int64, actorID int64, note string) (*Request, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE repair_requests SET assigned_to = ?, updated_at = ? WHERE id = ?`,
		assignee, now, id); err != nil {
		return nil, fmt.Errorf("update assignee: %w", err)
	}
	from := nullableID(cur.AssignedTo)
	to := nullableID(assignee)
	if err := appendEvent(ctx, tx, id, &actorID, "assignment", from, to, strings.TrimSpace(note)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// SetPriority changes the priority and logs it.
func (s *Service) SetPriority(ctx context.Context, id int64, priority string, actorID int64, note string) (*Request, error) {
	if !validPriorities[priority] {
		return nil, ErrInvalidPriority
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE repair_requests SET priority = ?, updated_at = ? WHERE id = ?`,
		priority, now, id); err != nil {
		return nil, fmt.Errorf("update priority: %w", err)
	}
	if err := appendEvent(ctx, tx, id, &actorID, "priority_change", ptr(cur.Priority), ptr(priority), strings.TrimSpace(note)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// AddNote appends a free-text note to the request's history without otherwise
// changing it (touches updated_at so it sorts to the top).
func (s *Service) AddNote(ctx context.Context, id int64, actorID int64, note string) (*Event, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, fmt.Errorf("%w: note is empty", ErrValidation)
	}
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE repair_requests SET updated_at = ? WHERE id = ?`, time.Now().Unix(), id); err != nil {
		return nil, err
	}
	if err := appendEvent(ctx, tx, id, &actorID, "note", nil, nil, note); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	evs, err := s.Events(ctx, id)
	if err != nil || len(evs) == 0 {
		return nil, err
	}
	return &evs[len(evs)-1], nil
}

// appendEvent inserts one immutable audit row inside the caller's tx.
func appendEvent(ctx context.Context, tx *sql.Tx, requestID int64, actorID *int64, kind string, from, to *string, note string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO request_events
			(request_id, actor_id, kind, from_value, to_value, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		requestID, actorID, kind, from, to, note, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func scanRequest(sc interface{ Scan(...any) error }) (*Request, error) {
	var (
		r          Request
		status     string
		assignedTo sql.NullInt64
		createdBy  sql.NullInt64
		createdAt  int64
		updatedAt  int64
		closedAt   sql.NullInt64
	)
	if err := sc.Scan(
		&r.ID, &r.GameTitle, &r.CabinetRef, &r.ProblemSummary, &r.ProblemDetail,
		&r.ReporterName, &r.ReporterContact, &status, &r.Priority, &assignedTo,
		&createdBy, &createdAt, &updatedAt, &closedAt,
	); err != nil {
		return nil, err
	}
	r.Status = Status(status)
	if assignedTo.Valid {
		r.AssignedTo = &assignedTo.Int64
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

func ptr(s string) *string { return &s }

func nullableID(id *int64) *string {
	if id == nil {
		return nil
	}
	s := strconv.FormatInt(*id, 10)
	return &s
}
