// Package server implements the gRPC API of PulseWatch.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pulsewatchv1 "github.com/Th3K3rn3l/PulseWatch/gen/pulsewatch/v1"
	"github.com/Th3K3rn3l/PulseWatch/internal/checker"
	"github.com/Th3K3rn3l/PulseWatch/internal/model"
	"github.com/Th3K3rn3l/PulseWatch/internal/storage"
)

const (
	defaultIntervalSeconds = 30
	minIntervalSeconds     = 5
	maxIntervalSeconds     = 3600

	defaultChecksLimit = 100
	maxChecksLimit     = 1000

	defaultWindowHours = 24
	maxWindowHours     = 24 * 30
)

// API implements pulsewatchv1.PulseWatchServer.
type API struct {
	pulsewatchv1.UnimplementedPulseWatchServer

	store   *storage.Store
	manager *checker.Manager
	log     *slog.Logger
}

// New creates the gRPC API implementation.
func New(store *storage.Store, manager *checker.Manager, log *slog.Logger) *API {
	return &API{store: store, manager: manager, log: log}
}

// AddTarget validates and stores a target, then schedules its checks.
func (a *API) AddTarget(ctx context.Context, req *pulsewatchv1.AddTargetRequest) (*pulsewatchv1.AddTargetResponse, error) {
	rawURL := strings.TrimSpace(req.GetUrl())
	if err := validateURL(rawURL); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid url: %v", err)
	}

	seconds := req.GetIntervalSeconds()
	if seconds == 0 {
		seconds = defaultIntervalSeconds
	}
	seconds = clamp(seconds, minIntervalSeconds, maxIntervalSeconds)

	target, err := a.store.UpsertTarget(ctx, rawURL, strings.TrimSpace(req.GetName()),
		time.Duration(seconds)*time.Second)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "store target: %v", err)
	}
	a.manager.Start(target)

	a.log.Info("target added", "id", target.ID, "url", target.URL, "interval", target.Interval.String())
	return &pulsewatchv1.AddTargetResponse{Target: targetToProto(target)}, nil
}

// RemoveTarget deletes a target and stops its checks.
func (a *API) RemoveTarget(ctx context.Context, req *pulsewatchv1.RemoveTargetRequest) (*pulsewatchv1.RemoveTargetResponse, error) {
	if req.GetId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "id must be positive")
	}
	err := a.store.RemoveTarget(ctx, req.GetId())
	switch {
	case err == nil:
	case errors.Is(err, storage.ErrNotFound):
		return nil, status.Error(codes.NotFound, "target not found")
	default:
		return nil, status.Errorf(codes.Internal, "remove target: %v", err)
	}
	a.manager.Stop(req.GetId())

	a.log.Info("target removed", "id", req.GetId())
	return &pulsewatchv1.RemoveTargetResponse{}, nil
}

// ListTargets returns all targets.
func (a *API) ListTargets(ctx context.Context, _ *pulsewatchv1.ListTargetsRequest) (*pulsewatchv1.ListTargetsResponse, error) {
	targets, err := a.store.ListTargets(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list targets: %v", err)
	}
	resp := &pulsewatchv1.ListTargetsResponse{Targets: make([]*pulsewatchv1.Target, 0, len(targets))}
	for _, t := range targets {
		resp.Targets = append(resp.Targets, targetToProto(t))
	}
	return resp, nil
}

// ListChecks returns the latest checks of a target.
func (a *API) ListChecks(ctx context.Context, req *pulsewatchv1.ListChecksRequest) (*pulsewatchv1.ListChecksResponse, error) {
	if req.GetTargetId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "target_id must be positive")
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = defaultChecksLimit
	}
	if limit > maxChecksLimit {
		limit = maxChecksLimit
	}

	checks, err := a.store.ListChecks(ctx, req.GetTargetId(), limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list checks: %v", err)
	}
	resp := &pulsewatchv1.ListChecksResponse{Checks: make([]*pulsewatchv1.Check, 0, len(checks))}
	for _, c := range checks {
		resp.Checks = append(resp.Checks, checkToProto(c))
	}
	return resp, nil
}

// GetStats returns aggregated stats of a target over a time window.
func (a *API) GetStats(ctx context.Context, req *pulsewatchv1.GetStatsRequest) (*pulsewatchv1.GetStatsResponse, error) {
	if req.GetTargetId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "target_id must be positive")
	}
	if _, err := a.store.GetTarget(ctx, req.GetTargetId()); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "target not found")
		}
		return nil, status.Errorf(codes.Internal, "get target: %v", err)
	}

	hours := req.GetWindowHours()
	if hours == 0 {
		hours = defaultWindowHours
	}
	if hours > maxWindowHours {
		hours = maxWindowHours
	}

	stats, err := a.store.GetStats(ctx, req.GetTargetId(),
		time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get stats: %v", err)
	}
	return &pulsewatchv1.GetStatsResponse{Stats: statsToProto(stats)}, nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("malformed url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("scheme must be http or https")
	}
	if u.Host == "" {
		return errors.New("host is empty")
	}
	return nil
}

func clamp(v, lo, hi uint32) uint32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func targetToProto(t model.Target) *pulsewatchv1.Target {
	return &pulsewatchv1.Target{
		Id:              t.ID,
		Url:             t.URL,
		Name:            t.Name,
		IntervalSeconds: uint32(t.Interval.Seconds()),
		CreatedAt:       timestamppb.New(t.CreatedAt),
	}
}

func checkToProto(c model.Check) *pulsewatchv1.Check {
	return &pulsewatchv1.Check{
		Id:         c.ID,
		TargetId:   c.TargetID,
		CheckedAt:  timestamppb.New(c.CheckedAt),
		Ok:         c.OK,
		StatusCode: int32(c.StatusCode),
		LatencyMs:  c.LatencyMS,
		Error:      c.Error,
	}
}

func statsToProto(s model.Stats) *pulsewatchv1.Stats {
	return &pulsewatchv1.Stats{
		TargetId:      s.TargetID,
		TotalChecks:   uint64(s.TotalChecks),
		FailedChecks:  uint64(s.FailedChecks),
		UptimePercent: s.UptimePct,
		AvgLatencyMs:  s.AvgLatencyMS,
		P95LatencyMs:  s.P95LatencyMS,
	}
}
