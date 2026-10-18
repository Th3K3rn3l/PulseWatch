package alerter

import (
	"io"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Th3K3rn3l/PulseWatch/internal/metrics"
	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

func TestAlerterFiresOncePerOutage(t *testing.T) {
	m := metrics.New(prometheus.NewRegistry())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(2, m, log)

	target := model.Target{ID: 1, URL: "https://example.com"}
	alerts := func() float64 {
		return testutil.ToFloat64(m.AlertsTotal.WithLabelValues("https://example.com"))
	}

	a.Observe(target, model.Check{OK: false})
	if alerts() != 0 {
		t.Fatalf("alerts after 1 failure = %v, want 0", alerts())
	}

	a.Observe(target, model.Check{OK: false})
	if alerts() != 1 {
		t.Fatalf("alerts after the threshold = %v, want 1", alerts())
	}

	// Further failures must not spam: one alert per ongoing outage.
	a.Observe(target, model.Check{OK: false})
	a.Observe(target, model.Check{OK: false})
	if alerts() != 1 {
		t.Fatalf("alerts during the outage = %v, want 1", alerts())
	}

	// Recovery resets the state...
	a.Observe(target, model.Check{OK: true})
	a.Observe(target, model.Check{OK: false})
	if alerts() != 1 {
		t.Fatalf("alerts after recovery = %v, want 1", alerts())
	}

	// ...so the next outage alerts again.
	a.Observe(target, model.Check{OK: false})
	if alerts() != 2 {
		t.Fatalf("alerts after the second outage = %v, want 2", alerts())
	}
}

func TestAlerterTracksTargetsIndependently(t *testing.T) {
	m := metrics.New(prometheus.NewRegistry())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(2, m, log)

	first := model.Target{ID: 1, URL: "https://one.example.com"}
	second := model.Target{ID: 2, URL: "https://two.example.com"}

	a.Observe(first, model.Check{OK: false})
	a.Observe(first, model.Check{OK: false})
	a.Observe(second, model.Check{OK: false})

	if got := testutil.ToFloat64(m.AlertsTotal.WithLabelValues("https://one.example.com")); got != 1 {
		t.Fatalf("first target alerts = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.AlertsTotal.WithLabelValues("https://two.example.com")); got != 0 {
		t.Fatalf("second target alerts = %v, want 0", got)
	}
}
