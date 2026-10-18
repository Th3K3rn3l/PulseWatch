package server

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Th3K3rn3l/PulseWatch/internal/metrics"
)

// Interceptors returns the unary interceptor chain of the gRPC server:
// panic recovery, structured logging and Prometheus metrics.
func Interceptors(log *slog.Logger, m *metrics.Metrics) grpc.ServerOption {
	return grpc.ChainUnaryInterceptor(
		recoveryInterceptor(log),
		loggingInterceptor(log),
		metricsInterceptor(m),
	)
}

// recoveryInterceptor turns panics in handlers into Internal errors
// instead of crashing the whole server.
func recoveryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// loggingInterceptor logs every request with duration and status code.
func loggingInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		log.Info("grpc request",
			"method", info.FullMethod,
			"duration_ms", time.Since(start).Milliseconds(),
			"code", status.Code(err).String(),
		)
		return resp, err
	}
}

// metricsInterceptor records request count and duration per method.
func metricsInterceptor(m *metrics.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		m.ObserveGRPC(info.FullMethod, status.Code(err).String(), time.Since(start))
		return resp, err
	}
}
