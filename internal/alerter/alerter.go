// Package alerter tracks consecutive failures per target and fires alerts.
package alerter

import (
	"log/slog"
	"sync"

	"github.com/Th3K3rn3l/PulseWatch/internal/metrics"
	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

// Alerter fires exactly one alert per ongoing outage: after Threshold
// consecutive failed checks it logs an alert and increments the
// pulsewatch_alerts_total counter; when the target recovers, it logs a
// recovery message and resets the state, so the next outage alerts again.
type Alerter struct {
	threshold int
	metrics   *metrics.Metrics
	log       *slog.Logger

	mu     sync.Mutex
	states map[int64]*state
}

type state struct {
	fails  int
	firing bool
}

// New creates an Alerter with the given failure threshold (>= 1).
func New(threshold int, m *metrics.Metrics, log *slog.Logger) *Alerter {
	if threshold < 1 {
		threshold = 1
	}
	return &Alerter{
		threshold: threshold,
		metrics:   m,
		log:       log,
		states:    make(map[int64]*state),
	}
}

// Observe ingests a check result and updates the alert state of the target.
func (a *Alerter) Observe(target model.Target, check model.Check) {
	a.mu.Lock()
	st := a.states[target.ID]
	if st == nil {
		st = &state{}
		a.states[target.ID] = st
	}

	var fire, recovered bool
	failures := st.fails

	if check.OK {
		if st.firing {
			recovered = true
		}
		st.fails = 0
		st.firing = false
	} else {
		st.fails++
		failures = st.fails
		if st.fails >= a.threshold && !st.firing {
			st.firing = true
			fire = true
		}
	}
	a.mu.Unlock()

	switch {
	case fire:
		a.metrics.IncAlert(target.URL)
		a.log.Warn("ALERT: target is down",
			"target", target.URL,
			"consecutive_failures", failures,
		)
	case recovered:
		a.log.Info("target recovered", "target", target.URL)
	}
}
