package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

// UpsertTarget inserts a target or updates its name and interval if a target
// with the same URL already exists. Insert and update are a single atomic
// statement, so concurrent AddTarget calls cannot create duplicates.
func (s *Store) UpsertTarget(ctx context.Context, url, name string, interval time.Duration) (model.Target, error) {
	const q = `
		INSERT INTO targets (url, name, interval_seconds)
		VALUES ($1, $2, $3)
		ON CONFLICT (url) DO UPDATE
			SET name = EXCLUDED.name,
			    interval_seconds = EXCLUDED.interval_seconds
		RETURNING id, url, name, interval_seconds, created_at`

	var (
		t               model.Target
		intervalSeconds int
	)
	err := s.pool.QueryRow(ctx, q, url, name, int(interval.Seconds())).
		Scan(&t.ID, &t.URL, &t.Name, &intervalSeconds, &t.CreatedAt)
	if err != nil {
		return model.Target{}, fmt.Errorf("upsert target: %w", err)
	}
	t.Interval = time.Duration(intervalSeconds) * time.Second
	return t, nil
}

// GetTarget returns a target by id, or ErrNotFound.
func (s *Store) GetTarget(ctx context.Context, id int64) (model.Target, error) {
	const q = `SELECT id, url, name, interval_seconds, created_at FROM targets WHERE id = $1`

	var (
		t               model.Target
		intervalSeconds int
	)
	err := s.pool.QueryRow(ctx, q, id).
		Scan(&t.ID, &t.URL, &t.Name, &intervalSeconds, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Target{}, ErrNotFound
	}
	if err != nil {
		return model.Target{}, fmt.Errorf("get target: %w", err)
	}
	t.Interval = time.Duration(intervalSeconds) * time.Second
	return t, nil
}

// RemoveTarget deletes a target; its checks go away via ON DELETE CASCADE.
func (s *Store) RemoveTarget(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM targets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("remove target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTargets returns all targets ordered by id.
func (s *Store) ListTargets(ctx context.Context) ([]model.Target, error) {
	const q = `SELECT id, url, name, interval_seconds, created_at FROM targets ORDER BY id`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	defer rows.Close()

	targets := make([]model.Target, 0)
	for rows.Next() {
		var (
			t               model.Target
			intervalSeconds int
		)
		if err := rows.Scan(&t.ID, &t.URL, &t.Name, &intervalSeconds, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}
		t.Interval = time.Duration(intervalSeconds) * time.Second
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate targets: %w", err)
	}
	return targets, nil
}
