package storage

import (
	"context"
	"fmt"

	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

// InsertCheck stores a single check result.
func (s *Store) InsertCheck(ctx context.Context, c model.Check) error {
	const q = `
		INSERT INTO checks (target_id, checked_at, ok, status_code, latency_ms, error)
		VALUES ($1, $2, $3, $4, $5, $6)`

	if _, err := s.pool.Exec(ctx, q,
		c.TargetID, c.CheckedAt, c.OK, c.StatusCode, c.LatencyMS, c.Error,
	); err != nil {
		return fmt.Errorf("insert check: %w", err)
	}
	return nil
}

// ListChecks returns the latest checks of a target, newest first.
func (s *Store) ListChecks(ctx context.Context, targetID int64, limit int) ([]model.Check, error) {
	const q = `
		SELECT id, target_id, checked_at, ok, status_code, latency_ms, error
		FROM checks
		WHERE target_id = $1
		ORDER BY checked_at DESC
		LIMIT $2`

	rows, err := s.pool.Query(ctx, q, targetID, limit)
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}
	defer rows.Close()

	checks := make([]model.Check, 0)
	for rows.Next() {
		var c model.Check
		if err := rows.Scan(&c.ID, &c.TargetID, &c.CheckedAt, &c.OK,
			&c.StatusCode, &c.LatencyMS, &c.Error); err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		checks = append(checks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checks: %w", err)
	}
	return checks, nil
}
