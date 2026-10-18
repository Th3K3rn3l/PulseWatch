package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Th3K3rn3l/PulseWatch/internal/model"
)

func TestHTTPProberStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		wantOK bool
	}{
		{"200 OK", http.StatusOK, true},
		{"204 No Content", http.StatusNoContent, true},
		{"399 boundary", 399, true},
		{"400 boundary", 400, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"500 Internal Server Error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			p := NewHTTPProber(2 * time.Second)
			check := p.Probe(context.Background(), model.Target{ID: 1, URL: srv.URL})

			if check.OK != tt.wantOK {
				t.Fatalf("OK = %v, want %v (status %d, error %q)",
					check.OK, tt.wantOK, check.StatusCode, check.Error)
			}
			if check.StatusCode != tt.status {
				t.Fatalf("StatusCode = %d, want %d", check.StatusCode, tt.status)
			}
			if !tt.wantOK && check.Error == "" {
				t.Fatal("expected a non-empty error for a failed check")
			}
			if check.LatencyMS < 0 {
				t.Fatalf("LatencyMS = %d, want >= 0", check.LatencyMS)
			}
		})
	}
}

func TestHTTPProberTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewHTTPProber(50 * time.Millisecond)
	check := p.Probe(context.Background(), model.Target{ID: 1, URL: srv.URL})

	if check.OK {
		t.Fatal("expected the timeout to fail the check")
	}
	if !strings.Contains(check.Error, "deadline") && !strings.Contains(check.Error, "Timeout") {
		t.Fatalf("unexpected error: %q", check.Error)
	}
}

func TestHTTPProberRecordsLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewHTTPProber(2 * time.Second)
	check := p.Probe(context.Background(), model.Target{ID: 1, URL: srv.URL})

	if check.LatencyMS < 25 {
		t.Fatalf("LatencyMS = %d, want >= 25", check.LatencyMS)
	}
}
