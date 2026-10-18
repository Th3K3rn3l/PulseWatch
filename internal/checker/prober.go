// Package checker executes scheduled HTTP probes with a bounded worker pool.
package checker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

// Prober performs a single HTTP probe of a target.
type Prober interface {
	Probe(ctx context.Context, target model.Target) model.Check
}

// HTTPProber is a Prober backed by net/http.
type HTTPProber struct {
	client *http.Client
}

// NewHTTPProber returns a prober whose requests fail after timeout.
func NewHTTPProber(timeout time.Duration) *HTTPProber {
	return &HTTPProber{client: &http.Client{Timeout: timeout}}
}

// Probe issues a GET request and measures latency. A check is OK when the
// response status is 2xx or 3xx.
func (p *HTTPProber) Probe(ctx context.Context, target model.Target) model.Check {
	check := model.Check{TargetID: target.ID, CheckedAt: time.Now()}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		check.Error = err.Error()
		return check
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	check.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		check.Error = err.Error()
		return check
	}
	defer resp.Body.Close()

	// Drain a small tail so the connection returns to the keep-alive pool.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	check.StatusCode = resp.StatusCode
	check.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !check.OK {
		check.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return check
}
