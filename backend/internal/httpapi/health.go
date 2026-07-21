package httpapi

import (
	"net/http"

	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

// HealthHandler serves GET /health and GET /ready.
type HealthHandler struct {
	svc *service.HealthService
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(svc *service.HealthService) *HealthHandler {
	return &HealthHandler{svc: svc}
}

// Live handles GET /health.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	WriteOK(w, http.StatusOK, h.svc.Live(r.Context()))
}

// Ready handles GET /ready.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	status, ok := h.svc.Ready(r.Context())
	if !ok {
		WriteOK(w, http.StatusServiceUnavailable, status)
		return
	}
	WriteOK(w, http.StatusOK, status)
}
