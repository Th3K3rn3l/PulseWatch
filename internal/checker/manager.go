package checker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Th3K3rn3l/PulseWatch/internal/alerter"
	"github.com/Th3K3rn3l/PulseWatch/internal/metrics"
	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

// Config tunes the worker pool.
type Config struct {
	Workers   int
	QueueSize int
}

// Store is the subset of storage used by the checker
// (consumer-side interface).
type Store interface {
	InsertCheck(ctx context.Context, check model.Check) error
}

// Manager schedules checks per target and executes them on a worker pool.
//
// Layout: one goroutine with a ticker per target (producer), a shared
// buffered jobs channel and N workers. If the queue is full, a check is
// skipped and pulsewatch_jobs_dropped_total grows — the scheduler never
// blocks on slow targets (backpressure).
type Manager struct {
	cfg     Config
	store   Store
	prober  Prober
	alerter *alerter.Alerter
	metrics *metrics.Metrics
	log     *slog.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	started bool

	jobs chan model.Target

	mu      sync.Mutex
	runners map[int64]context.CancelFunc
	runWg   sync.WaitGroup
	workWg  sync.WaitGroup
}

// New creates a Manager. Call Run before Start.
func New(store Store, prober Prober, al *alerter.Alerter, m *metrics.Metrics, log *slog.Logger, cfg Config) *Manager {
	return &Manager{
		cfg:     cfg,
		store:   store,
		prober:  prober,
		alerter: al,
		metrics: m,
		log:     log,
		jobs:    make(chan model.Target, cfg.QueueSize),
		runners: make(map[int64]context.CancelFunc),
	}
}

// Run starts the worker pool and schedules checks for the given targets.
func (m *Manager) Run(ctx context.Context, targets []model.Target) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	for i := 0; i < m.cfg.Workers; i++ {
		m.workWg.Add(1)
		go m.worker()
	}
	for _, t := range targets {
		m.Start(t)
	}
}

// Start schedules checks for a target. It is a no-op if the target is
// already running or the manager is shutting down.
func (m *Manager) Start(t model.Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return
	}
	if cancel, ok := m.runners[t.ID]; ok {
		// The target already exists (AddTarget is a no-op restart on the
		// same URL), so replace the runner to apply a new interval.
		cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.runners[t.ID] = cancel
	m.runWg.Add(1)
	go m.runTarget(ctx, t)
}

// Stop cancels checks for a target.
func (m *Manager) Stop(id int64) {
	m.mu.Lock()
	cancel, ok := m.runners[id]
	delete(m.runners, id)
	m.mu.Unlock()
	if ok {
		cancel()
	}
	m.alerter.Forget(id)
}

// Shutdown stops all producers, then closes the queue and waits for the
// workers to drain it. Safe to call multiple times.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.started = false
	for id, cancel := range m.runners {
		cancel()
		delete(m.runners, id)
	}
	m.mu.Unlock()

	m.runWg.Wait() // all producers stopped: safe to close the channel
	close(m.jobs)
	m.workWg.Wait() // workers drained the queue and exited
}

func (m *Manager) runTarget(ctx context.Context, t model.Target) {
	defer m.runWg.Done()

	m.enqueue(ctx, t) // first check right away

	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.enqueue(ctx, t)
		}
	}
}

func (m *Manager) enqueue(ctx context.Context, t model.Target) {
	select {
	case m.jobs <- t:
	case <-ctx.Done():
	default:
		m.metrics.IncJobsDropped()
		m.log.Warn("check dropped: queue is full", "target", t.URL)
	}
}

func (m *Manager) worker() {
	defer m.workWg.Done()
	for t := range m.jobs {
		m.handle(m.ctx, t)
	}
}

func (m *Manager) handle(ctx context.Context, t model.Target) {
	check := m.prober.Probe(ctx, t)
	if ctx.Err() != nil {
		// The service is shutting down: an aborted probe must not be
		// recorded as a target failure.
		return
	}
	m.metrics.ObserveCheck(t.URL, check.OK, time.Duration(check.LatencyMS)*time.Millisecond)

	// Persist with an independent context so a completed check is not
	// lost when the manager context is cancelled during shutdown.
	dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.store.InsertCheck(dbCtx, check); err != nil {
		m.log.Error("store check", "target", t.URL, "error", err)
	}

	m.alerter.Observe(t, check)
}
