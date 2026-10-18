//go:build integration

package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

// TestStoreIntegration exercises the store against a real PostgreSQL.
//
// Run with: make test-integration
// (PULSEWATCH_TEST_DSN must point to a disposable test database).
func TestStoreIntegration(t *testing.T) {
	dsn := os.Getenv("PULSEWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("PULSEWATCH_TEST_DSN is not set")
	}
	ctx := context.Background()

	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `TRUNCATE targets RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	target, err := s.UpsertTarget(ctx, "https://example.com", "example", 30*time.Second)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Upsert must be idempotent by URL.
	again, err := s.UpsertTarget(ctx, "https://example.com", "example renamed", 60*time.Second)
	if err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	if again.ID != target.ID {
		t.Fatalf("upsert created a new target: id %d vs %d", again.ID, target.ID)
	}
	if again.Name != "example renamed" || again.Interval != 60*time.Second {
		t.Fatalf("upsert did not update the target: %+v", again)
	}

	base := time.Now().Add(-time.Minute)
	checks := []struct {
		latency int64
		ok      bool
	}{
		{10, true},
		{20, true},
		{30, false},
	}
	for i, tc := range checks {
		err := s.InsertCheck(ctx, model.Check{
			TargetID:   target.ID,
			CheckedAt:  base.Add(time.Duration(i) * time.Second),
			OK:         tc.ok,
			StatusCode: 200,
			LatencyMS:  tc.latency,
		})
		if err != nil {
			t.Fatalf("insert check: %v", err)
		}
	}

	stats, err := s.GetStats(ctx, target.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalChecks != 3 {
		t.Fatalf("total = %d, want 3", stats.TotalChecks)
	}
	if stats.FailedChecks != 1 {
		t.Fatalf("failed = %d, want 1", stats.FailedChecks)
	}
	if stats.UptimePct < 66.6 || stats.UptimePct > 66.7 {
		t.Fatalf("uptime = %v, want ~66.67", stats.UptimePct)
	}
	if stats.AvgLatencyMS != 20 {
		t.Fatalf("avg latency = %v, want 20", stats.AvgLatencyMS)
	}
	// PERCENTILE_CONT(0.95) over {10,20,30} interpolates to 29.
	if stats.P95LatencyMS < 28 || stats.P95LatencyMS > 30 {
		t.Fatalf("p95 latency = %v, want ~29", stats.P95LatencyMS)
	}

	listed, err := s.ListChecks(ctx, target.ID, 10)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("checks = %d, want 3", len(listed))
	}
	if !listed[0].CheckedAt.After(listed[2].CheckedAt) {
		t.Fatal("checks are not ordered newest-first")
	}

	if err := s.RemoveTarget(ctx, target.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := s.GetTarget(ctx, target.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after removal, got %v", err)
	}
}
