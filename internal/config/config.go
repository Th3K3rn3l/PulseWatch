// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all tunable service parameters.
type Config struct {
	GRPCAddr        string
	MetricsAddr     string
	PostgresDSN     string
	Workers         int
	QueueSize       int
	AlertThreshold  int
	CheckTimeout    time.Duration
	ShutdownTimeout time.Duration
	LogLevel        string
}

// Load reads configuration from the environment, applying defaults.
// Only PULSEWATCH_POSTGRES_DSN is required.
func Load() (Config, error) {
	cfg := Config{
		GRPCAddr:        getEnv("PULSEWATCH_GRPC_ADDR", ":50051"),
		MetricsAddr:     getEnv("PULSEWATCH_METRICS_ADDR", ":2112"),
		PostgresDSN:     os.Getenv("PULSEWATCH_POSTGRES_DSN"),
		Workers:         getEnvInt("PULSEWATCH_WORKERS", 8),
		QueueSize:       getEnvInt("PULSEWATCH_QUEUE_SIZE", 128),
		AlertThreshold:  getEnvInt("PULSEWATCH_ALERT_THRESHOLD", 3),
		CheckTimeout:    getEnvDuration("PULSEWATCH_CHECK_TIMEOUT", 5*time.Second),
		ShutdownTimeout: getEnvDuration("PULSEWATCH_SHUTDOWN_TIMEOUT", 10*time.Second),
		LogLevel:        getEnv("PULSEWATCH_LOG_LEVEL", "info"),
	}

	if cfg.PostgresDSN == "" {
		return Config{}, fmt.Errorf("PULSEWATCH_POSTGRES_DSN is required")
	}
	if cfg.Workers < 1 {
		return Config{}, fmt.Errorf("PULSEWATCH_WORKERS must be >= 1, got %d", cfg.Workers)
	}
	if cfg.QueueSize < 1 {
		return Config{}, fmt.Errorf("PULSEWATCH_QUEUE_SIZE must be >= 1, got %d", cfg.QueueSize)
	}
	if cfg.AlertThreshold < 1 {
		return Config{}, fmt.Errorf("PULSEWATCH_ALERT_THRESHOLD must be >= 1, got %d", cfg.AlertThreshold)
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
