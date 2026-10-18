// Package model contains the core domain types shared between layers.
package model

import "time"

// Target is an HTTP endpoint being monitored.
type Target struct {
	ID        int64
	URL       string
	Name      string
	Interval  time.Duration
	CreatedAt time.Time
}

// Check is the result of a single probe of a target.
type Check struct {
	ID         int64
	TargetID   int64
	CheckedAt  time.Time
	OK         bool
	StatusCode int
	LatencyMS  int64
	Error      string
}

// Stats is an aggregated view of checks over a time window.
type Stats struct {
	TargetID     int64
	TotalChecks  int64
	FailedChecks int64
	UptimePct    float64
	AvgLatencyMS float64
	P95LatencyMS float64
}
