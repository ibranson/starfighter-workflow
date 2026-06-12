// Package nodestate is a tiny durable key/value store over the node_state
// table. Used for daemon bookkeeping such as the last-seen version string
// that drives the post-deploy session wipe.
package nodestate

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"starfighter-workflow/internal/db"
)

type Store struct {
	db *db.DB
}

func New(d *db.DB) *Store { return &Store{db: d} }

// Get returns the value for key. ok is false if the key is absent.
func (s *Store) Get(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT value FROM node_state WHERE key = ?`, key,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// Set upserts the value for key.
func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO node_state(key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, time.Now().Unix())
	return err
}
