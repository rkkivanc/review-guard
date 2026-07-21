package service

import (
	"context"

	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/repository"
)

// HealthService answers liveness and readiness probes.
type HealthService struct {
	cfg   config.Config
	store repository.Store
}

// NewHealthService wires config + store into the health use cases.
func NewHealthService(cfg config.Config, store repository.Store) *HealthService {
	return &HealthService{cfg: cfg, store: store}
}

// Live returns process liveness plus a non-blocking store ping.
func (s *HealthService) Live(ctx context.Context) domain.HealthStatus {
	status := domain.HealthStatus{
		Status:  "ok",
		Store:   s.store.Name(),
		Version: s.cfg.Version,
	}
	if err := s.store.Ping(ctx); err != nil {
		status.Status = "degraded"
		status.DBPingOK = false
		return status
	}
	status.DBPingOK = true
	return status
}

// Ready reports whether the process can accept traffic.
func (s *HealthService) Ready(ctx context.Context) (domain.HealthStatus, bool) {
	h := s.Live(ctx)
	ok := h.Status == "ok" && h.DBPingOK
	if !ok {
		h.Status = "not_ready"
	}
	return h, ok
}
