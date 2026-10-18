package checker

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Th3K3rn3l/PulseWatch/internal/alerter"
	"github.com/Th3K3rn3l/PulseWatch/internal/metrics"
	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

type fakeProber struct {
	latencyMS int

	mu        sync.Mutex
	active    int
	maxActive int
	calls     int
}

func (f *fakeProber) Probe(ctx context.Context, t model.Target) model.Check {
	f.mu.Lock()
	f.active++
	f.calls++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()

	select {
	case <-time.After(time.Duration(f.latencyMS) * time.Millisecond):
	case <-ctx.Done():
	}

	f.mu.Lock()
	f.active--
	f.mu.Unlock()

	return model.Check{
		TargetID:  t.ID,
		CheckedAt: time.Now(),
		OK:        true,
		LatencyMS: int64(f.latencyMS),
	}
}

func (f *fakeProber) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeStore struct {
	mu     sync.Mutex
	checks []model.Check
}

func (f *fakeStore) InsertCheck(_ context.Context, c model.Check) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, c)
	return nil
}

func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.checks)
}

func newTestManager(t *testing.T, prober Prober, store Store, cfg Config) (*Manager, *metrics.Metrics) {
	t.Helper()
	m := metrics.New(prometheus.NewRegistry())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(store, prober, alerter.New(3, m, log), m, log, cfg), m
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met in time")
}

func TestManagerRunsAllTargetsWithBoundedWorkers(t *testing.T) {
	const targets = 20

	prober := &fakeProber{latencyMS: 10}
	store := &fakeStore{}
	mgr, _ := newTestManager(t, prober, store, Config{Workers: 4, QueueSize: 128})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Run(ctx, nil)

	for i := 1; i <= targets; i++ {
		mgr.Start(model.Target{ID: int64(i), URL: "https://example.com", Interval: time.Hour})
	}

	waitFor(t, 5*time.Second, func() bool { return store.count() >= targets })
	mgr.Shutdown()

	prober.mu.Lock()
	defer prober.mu.Unlock()
	if prober.calls < targets {
		t.Fatalf("prober calls = %d, want >= %d", prober.calls, targets)
	}
	if prober.maxActive > 4 {
		t.Fatalf("max concurrent probes = %d, want <= 4", prober.maxActive)
	}
}

func TestManagerDropsJobsWhenQueueIsFull(t *testing.T) {
	prober := &fakeProber{latencyMS: 200}
	store := &fakeStore{}
	mgr, m := newTestManager(t, prober, store, Config{Workers: 1, QueueSize: 1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Run(ctx, nil)

	for i := 1; i <= 10; i++ {
		mgr.Start(model.Target{ID: int64(i), URL: "https://example.com", Interval: time.Hour})
	}

	waitFor(t, 2*time.Second, func() bool { return testutil.ToFloat64(m.JobsDropped) > 0 })
	mgr.Shutdown()
}

func TestManagerStopRemovesRunner(t *testing.T) {
	prober := &fakeProber{latencyMS: 1}
	store := &fakeStore{}
	mgr, _ := newTestManager(t, prober, store, Config{Workers: 2, QueueSize: 16})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Run(ctx, []model.Target{{ID: 1, URL: "https://example.com", Interval: time.Hour}})

	waitFor(t, 5*time.Second, func() bool { return store.count() >= 1 })

	mgr.Stop(1)
	mgr.mu.Lock()
	_, running := mgr.runners[1]
	mgr.mu.Unlock()
	if running {
		t.Fatal("target is still running after Stop")
	}
	mgr.Shutdown()
}

func TestManagerRestartsTargetWithNewInterval(t *testing.T) {
	prober := &fakeProber{latencyMS: 1}
	store := &fakeStore{}
	mgr, _ := newTestManager(t, prober, store, Config{Workers: 2, QueueSize: 64})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Run(ctx, nil)

	target := model.Target{ID: 1, URL: "https://example.com", Interval: 10 * time.Second}
	mgr.Start(target)
	waitFor(t, 5*time.Second, func() bool { return prober.count() >= 1 })

	// AddTarget on an existing URL updates the interval in the DB and calls
	// Start again: the old 10s ticker must be replaced by the new one.
	target.Interval = 25 * time.Millisecond
	mgr.Start(target)
	waitFor(t, 5*time.Second, func() bool { return prober.count() >= 5 })

	mgr.Shutdown()
}
