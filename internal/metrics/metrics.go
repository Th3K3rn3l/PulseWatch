// Package metrics declares all Prometheus instruments used by PulseWatch.
//
// Note: the target label is a URL. For a pet project this is fine, but in a
// large installation we would bound cardinality (e.g. label by target id and
// keep a separate mapping), since Prometheus stores one time series per
// label value.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics is the set of instruments used across the service.
type Metrics struct {
	reg *prometheus.Registry

	ChecksTotal   *prometheus.CounterVec
	CheckDuration *prometheus.HistogramVec
	TargetUp      *prometheus.GaugeVec
	AlertsTotal   *prometheus.CounterVec
	JobsDropped   prometheus.Counter
	GRPCRequests  *prometheus.CounterVec
	GRPCDuration  *prometheus.HistogramVec
}

// New creates and registers all instruments on the given registry.
func New(reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		reg: reg,
		ChecksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulsewatch_checks_total",
			Help: "Total number of checks by target and result (ok|failed).",
		}, []string{"target", "result"}),
		CheckDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pulsewatch_check_duration_seconds",
			Help:    "Check duration in seconds by target.",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"target"}),
		TargetUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pulsewatch_target_up",
			Help: "1 if the last check of the target succeeded, 0 otherwise.",
		}, []string{"target"}),
		AlertsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulsewatch_alerts_total",
			Help: "Total number of fired alerts (target went down).",
		}, []string{"target"}),
		JobsDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pulsewatch_jobs_dropped_total",
			Help: "Checks dropped because the worker queue was full.",
		}),
		GRPCRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulsewatch_grpc_requests_total",
			Help: "Total number of gRPC requests by method and status code.",
		}, []string{"method", "code"}),
		GRPCDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pulsewatch_grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds by method.",
			Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		}, []string{"method"}),
	}

	reg.MustRegister(
		m.ChecksTotal, m.CheckDuration, m.TargetUp,
		m.AlertsTotal, m.JobsDropped, m.GRPCRequests, m.GRPCDuration,
	)
	return m
}

// Handler returns the Prometheus scrape handler.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ObserveCheck records the result of a single check.
func (m *Metrics) ObserveCheck(target string, ok bool, latency time.Duration) {
	result := "ok"
	if !ok {
		result = "failed"
	}
	m.ChecksTotal.WithLabelValues(target, result).Inc()
	m.CheckDuration.WithLabelValues(target).Observe(latency.Seconds())
	if ok {
		m.TargetUp.WithLabelValues(target).Set(1)
	} else {
		m.TargetUp.WithLabelValues(target).Set(0)
	}
}

// IncAlert records a fired alert for a target.
func (m *Metrics) IncAlert(target string) {
	m.AlertsTotal.WithLabelValues(target).Inc()
}

// IncJobsDropped records a check dropped because the queue was full.
func (m *Metrics) IncJobsDropped() { m.JobsDropped.Inc() }

// ObserveGRPC records one handled gRPC request.
func (m *Metrics) ObserveGRPC(method, code string, d time.Duration) {
	m.GRPCRequests.WithLabelValues(method, code).Inc()
	m.GRPCDuration.WithLabelValues(method).Observe(d.Seconds())
}
