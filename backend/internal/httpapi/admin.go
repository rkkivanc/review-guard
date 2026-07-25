package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

// AdminHandler serves /admin/* control-plane routes.
type AdminHandler struct {
	Admin *service.AdminService
}

func NewAdminHandler(admin *service.AdminService) *AdminHandler {
	return &AdminHandler{Admin: admin}
}

func (h *AdminHandler) GetLLMConfig(w http.ResponseWriter, r *http.Request) {
	WriteOK(w, http.StatusOK, h.Admin.GetLLMConfig())
}

func (h *AdminHandler) PatchLLMConfig(w http.ResponseWriter, r *http.Request) {
	var body service.UpdateLLMConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteErr(w, http.StatusBadRequest, "validation_error", "invalid JSON body")
		return
	}
	cfg, err := h.Admin.UpdateLLMConfig(r.Context(), body)
	if err != nil {
		writeAdminErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, cfg)
}

func (h *AdminHandler) ListAdapters(w http.ResponseWriter, r *http.Request) {
	items, err := h.Admin.ListAdapters()
	if err != nil {
		writeAdminErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, map[string]any{"adapters": items})
}

func (h *AdminHandler) UpsertAdapter(w http.ResponseWriter, r *http.Request) {
	var body domain.AdapterMeta
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteErr(w, http.StatusBadRequest, "validation_error", "invalid JSON body")
		return
	}
	item, err := h.Admin.UpsertAdapter(body)
	if err != nil {
		writeAdminErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, item)
}

func (h *AdminHandler) DeleteAdapter(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Admin.RemoveAdapter(r.Context(), id); err != nil {
		writeAdminErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *AdminHandler) ActivateAdapter(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AdapterID string `json:"adapter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteErr(w, http.StatusBadRequest, "validation_error", "invalid JSON body")
		return
	}
	cfg, err := h.Admin.ActivateAdapter(r.Context(), body.AdapterID)
	if err != nil {
		writeAdminErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, cfg)
}

func (h *AdminHandler) ListLogs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	WriteOK(w, http.StatusOK, map[string]any{"logs": h.Admin.ListLogs(limit)})
}

func (h *AdminHandler) ExportFinetune(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	exp, err := h.Admin.ExportFinetuneDataset(r.Context(), userID)
	if err != nil {
		writeAdminErr(w, err)
		return
	}
	if r.URL.Query().Get("format") == "jsonl" {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="feedback-finetune.jsonl"`)
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		for _, ex := range exp.Examples {
			_ = enc.Encode(ex)
		}
		return
	}
	WriteOK(w, http.StatusOK, exp)
}

func writeAdminErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		WriteErr(w, http.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		WriteErr(w, http.StatusNotFound, "not_found", "adapter not found")
	default:
		WriteErr(w, http.StatusInternalServerError, "internal_error", "admin operation failed")
	}
}
