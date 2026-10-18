// Command pulsewatch runs the PulseWatch service: gRPC API, checks engine,
// PostgreSQL storage and Prometheus metrics.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pulsewatchv1 "github.com/Th3K3rn3l/PulseWatch/gen/pulsewatch/v1"
	"github.com/Th3K3rn3l/PulseWatch/internal/alerter"
	"github.com/Th3K3rn3l/PulseWatch/internal/checker"
	"github.com/Th3K3rn3l/PulseWatch/internal/config"
	"github.com/Th3K3rn3l/PulseWatch/internal/metrics"
	"github.com/Th3K3rn3l/PulseWatch/internal/server"
	"github.com/Th3K3rn3l/PulseWatch/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	m := metrics.New(reg)

	prober := checker.NewHTTPProber(cfg.CheckTimeout)
	al := alerter.New(cfg.AlertThreshold, m, log)
	manager := checker.New(store, prober, al, m, log, checker.Config{
		Workers:   cfg.Workers,
		QueueSize: cfg.QueueSize,
	})

	targets, err := store.ListTargets(ctx)
	if err != nil {
		return err
	}
	manager.Run(ctx, targets)
	defer manager.Shutdown()
	log.Info("checker started", "targets", len(targets), "workers", cfg.Workers)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", m.Handler())
	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	metricsSrv := &http.Server{Addr: cfg.MetricsAddr, Handler: metricsMux}

	grpcSrv := grpc.NewServer(server.Interceptors(log, m))
	pulsewatchv1.RegisterPulseWatchServer(grpcSrv, server.New(store, manager, log))

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv)
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Reflection lets grpcurl and grpcui work without local proto files.
	reflection.Register(grpcSrv)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.GRPCAddr, err)
	}

	errCh := make(chan error, 2)
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("metrics server: %w", err)
		}
	}()
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			errCh <- fmt.Errorf("grpc server: %w", err)
		}
	}()

	log.Info("pulsewatch started", "grpc_addr", cfg.GRPCAddr, "metrics_addr", cfg.MetricsAddr)

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	// Graceful shutdown: stop accepting new requests, then wait for
	// in-flight ones. manager.Shutdown runs via defer.
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		grpcSrv.Stop()
		log.Warn("graceful stop timed out, forced stop")
	}

	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("metrics server shutdown", "error", err)
	}

	log.Info("pulsewatch stopped")
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
