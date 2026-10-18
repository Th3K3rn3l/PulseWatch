package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

// GetStats aggregates checks of a target over the window [since, now].
//
// The aggregation runs inside PostgreSQL as a single query: FILTER counts
// the share of successful checks and PERCENTILE_CONT computes the p95
// latency. The (target_id, checked_at DESC) index serves the window scan.
func (s *Store) GetStats(ctx context.Context, targetID int64, since time.Time) (model.Stats, error) {
	const q = `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE NOT ok),
			COALESCE(ROUND(100.0 * COUNT(*) FILTER (WHERE ok) / NULLIF(COUNT(*), 0), 2), 0)::float8,
			COALESCE(AVG(latency_ms), 0)::float8,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)::float8
		FROM checks
		WHERE target_id = $1 AND checked_at >= $2`

	st := model.Stats{TargetID: targetID}
	err := s.pool.QueryRow(ctx, q, targetID, since).
		Scan(&st.TotalChecks, &st.FailedChecks, &st.UptimePct, &st.AvgLatencyMS, &st.P95LatencyMS)
	if err != nil {
		return model.Stats{}, fmt.Errorf("get stats: %w", err)
	}
	return st, nil
}
