package service

import (
	"context"
	"strings"

	"github.com/masterfabric/review-guard/mf-backend/internal/adapters"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/llm"
	"github.com/masterfabric/review-guard/mf-backend/internal/llqlog"
	"github.com/masterfabric/review-guard/mf-backend/internal/llmruntime"
)

// AdminService is the LLM control-plane (adapters, prompts, limits, logs).
type AdminService struct {
	runtime  *llmruntime.Store
	adapters *adapters.Registry
	logs     *llqlog.Store
	llm      *llm.Client
}

// NewAdminService wires admin use cases.
func NewAdminService(runtime *llmruntime.Store, reg *adapters.Registry, logs *llqlog.Store, llmClient *llm.Client) *AdminService {
	return &AdminService{runtime: runtime, adapters: reg, logs: logs, llm: llmClient}
}

// GetLLMConfig returns hot-swap runtime settings.
func (s *AdminService) GetLLMConfig() domain.LLMRuntimeConfig {
	return s.runtime.Get()
}

// UpdateLLMConfigRequest is a partial update for runtime settings.
type UpdateLLMConfigRequest struct {
	SystemPrompt  *string  `json:"system_prompt"`
	MaxTokens     *int     `json:"max_tokens"`
	Temperature   *float64 `json:"temperature"`
	TopP          *float64 `json:"top_p"`
	ActiveAdapter *string  `json:"active_adapter"`
}

// UpdateLLMConfig applies a partial hot-swap update.
func (s *AdminService) UpdateLLMConfig(ctx context.Context, in UpdateLLMConfigRequest) (domain.LLMRuntimeConfig, error) {
	patch := domain.LLMRuntimeConfig{}
	var setPrompt, setMax, setTemp, setTopP, setAdapter bool
	if in.SystemPrompt != nil {
		patch.SystemPrompt = *in.SystemPrompt
		setPrompt = true
	}
	if in.MaxTokens != nil {
		if *in.MaxTokens < 16 || *in.MaxTokens > 8192 {
			return domain.LLMRuntimeConfig{}, domain.ErrValidation
		}
		patch.MaxTokens = *in.MaxTokens
		setMax = true
	}
	if in.Temperature != nil {
		if *in.Temperature < 0 || *in.Temperature > 2 {
			return domain.LLMRuntimeConfig{}, domain.ErrValidation
		}
		patch.Temperature = *in.Temperature
		setTemp = true
	}
	if in.TopP != nil {
		if *in.TopP <= 0 || *in.TopP > 1 {
			return domain.LLMRuntimeConfig{}, domain.ErrValidation
		}
		patch.TopP = *in.TopP
		setTopP = true
	}
	if in.ActiveAdapter != nil {
		aid := strings.TrimSpace(*in.ActiveAdapter)
		if aid != "" {
			if _, err := s.adapters.Get(aid); err != nil {
				return domain.LLMRuntimeConfig{}, err
			}
		}
		patch.ActiveAdapter = aid
		setAdapter = true
		_ = s.llm.ActivateRemoteAdapter(ctx, aid)
	}
	return s.runtime.Update(patch, setAdapter, setPrompt, setMax, setTemp, setTopP), nil
}

// ListAdapters returns registry rows with active flag.
func (s *AdminService) ListAdapters() ([]domain.AdapterMeta, error) {
	rt := s.runtime.Get()
	return s.adapters.List(rt.ActiveAdapter)
}

// UpsertAdapter registers a PEFT adapter for hot-swap.
func (s *AdminService) UpsertAdapter(meta domain.AdapterMeta) (domain.AdapterMeta, error) {
	return s.adapters.Upsert(meta)
}

// RemoveAdapter removes an adapter; clears active if needed.
func (s *AdminService) RemoveAdapter(ctx context.Context, id string) error {
	if err := s.adapters.Remove(id); err != nil {
		return err
	}
	rt := s.runtime.Get()
	if rt.ActiveAdapter == id {
		s.runtime.SetActiveAdapter("")
		_ = s.llm.ActivateRemoteAdapter(ctx, "")
	}
	return nil
}

// ActivateAdapter hot-swaps the active PEFT adapter.
func (s *AdminService) ActivateAdapter(ctx context.Context, id string) (domain.LLMRuntimeConfig, error) {
	id = strings.TrimSpace(id)
	if id != "" {
		if _, err := s.adapters.Get(id); err != nil {
			return domain.LLMRuntimeConfig{}, err
		}
	}
	_ = s.llm.ActivateRemoteAdapter(ctx, id)
	return s.runtime.SetActiveAdapter(id), nil
}

// ListLogs returns recent MCP/LLM query logs.
func (s *AdminService) ListLogs(limit int) []domain.QueryLogEntry {
	return s.logs.List(limit)
}
