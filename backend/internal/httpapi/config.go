package httpapi

import (
	"net/http"

	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

// ConfigHandler serves GET /config and GET /version.
type ConfigHandler struct {
	svc *service.ConfigService
}

// NewConfigHandler constructs a ConfigHandler.
func NewConfigHandler(svc *service.ConfigService) *ConfigHandler {
	return &ConfigHandler{svc: svc}
}

// Config handles GET /config.
func (h *ConfigHandler) Config(w http.ResponseWriter, r *http.Request) {
	WriteOK(w, http.StatusOK, h.svc.Public())
}

// Version handles GET /version.
func (h *ConfigHandler) Version(w http.ResponseWriter, r *http.Request) {
	WriteOK(w, http.StatusOK, h.svc.Version())
}
