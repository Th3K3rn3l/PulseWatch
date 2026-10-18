package config

import (
	"testing"
	"time"
)

var allKeys = []string{
	"PULSEWATCH_GRPC_ADDR",
	"PULSEWATCH_METRICS_ADDR",
	"PULSEWATCH_POSTGRES_DSN",
	"PULSEWATCH_WORKERS",
	"PULSEWATCH_QUEUE_SIZE",
	"PULSEWATCH_ALERT_THRESHOLD",
	"PULSEWATCH_CHECK_TIMEOUT",
	"PULSEWATCH_SHUTDOWN_TIMEOUT",
	"PULSEWATCH_LOG_LEVEL",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range allKeys {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("PULSEWATCH_POSTGRES_DSN", "postgres://user@localhost/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GRPCAddr != ":50051" || cfg.MetricsAddr != ":2112" {
		t.Fatalf("unexpected addresses: %q, %q", cfg.GRPCAddr, cfg.MetricsAddr)
	}
	if cfg.Workers != 8 || cfg.QueueSize != 128 || cfg.AlertThreshold != 3 {
		t.Fatalf("unexpected pool defaults: %+v", cfg)
	}
	if cfg.CheckTimeout != 5*time.Second || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected timeouts: %+v", cfg)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want info", cfg.LogLevel)
	}
}

func TestLoadRequiresDSN(t *testing.T) {
	clearEnv(t)

	if _, err := Load(); err == nil {
		t.Fatal("Load must fail without PULSEWATCH_POSTGRES_DSN")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
	}{
		{"zero workers", "PULSEWATCH_WORKERS", "0"},
		{"zero queue", "PULSEWATCH_QUEUE_SIZE", "0"},
		{"zero alert threshold", "PULSEWATCH_ALERT_THRESHOLD", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("PULSEWATCH_POSTGRES_DSN", "postgres://user@localhost/db")
			t.Setenv(tt.key, tt.val)

			if _, err := Load(); err == nil {
				t.Fatalf("Load must fail when %s=%s", tt.key, tt.val)
			}
		})
	}
}
