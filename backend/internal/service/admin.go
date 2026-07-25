package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/masterfabric/review-guard/mf-backend/internal/adapters"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/llm"
	"github.com/masterfabric/review-guard/mf-backend/internal/llqlog"
	"github.com/masterfabric/review-guard/mf-backend/internal/llmruntime"
)

// feedbackReviewLister lists reviews for finetune export (ISP).
type feedbackReviewLister interface {
	ListReviews(ctx context.Context, userID string, filter domain.ReviewListFilter) ([]domain.Review, int, error)
}

// AdminService is the LLM control-plane (adapters, prompts, limits, logs).
type AdminService struct {
	runtime  *llmruntime.Store
	adapters *adapters.Registry
	logs     *llqlog.Store
	llm      *llm.Client
	reviews  feedbackReviewLister
}

// NewAdminService wires admin use cases.
func NewAdminService(runtime *llmruntime.Store, reg *adapters.Registry, logs *llqlog.Store, llmClient *llm.Client, reviews feedbackReviewLister) *AdminService {
	return &AdminService{runtime: runtime, adapters: reg, logs: logs, llm: llmClient, reviews: reviews}
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

// ExportFinetuneDataset builds supervised chat examples from human label feedback.
func (s *AdminService) ExportFinetuneDataset(ctx context.Context, userID string) (domain.FinetuneExport, error) {
	if s.reviews == nil {
		return domain.FinetuneExport{}, fmt.Errorf("%w: review store unavailable", domain.ErrValidation)
	}
	items, _, err := s.reviews.ListReviews(ctx, userID, domain.ReviewListFilter{
		Limit:  MaxPageSize,
		Offset: 0,
	})
	if err != nil {
		return domain.FinetuneExport{}, err
	}
	examples := make([]domain.FinetuneExample, 0, len(items))
	for _, r := range items {
		if r.Feedback == nil {
			continue
		}
		ex, ok := buildFinetuneExample(r)
		if !ok {
			continue
		}
		examples = append(examples, ex)
	}
	return domain.FinetuneExport{Count: len(examples), Examples: examples}, nil
}

func buildFinetuneExample(r domain.Review) (domain.FinetuneExample, bool) {
	fb := r.Feedback
	if fb == nil {
		return domain.FinetuneExample{}, false
	}
	target, ok := targetJSONFromFeedback(*fb)
	if !ok {
		return domain.FinetuneExample{}, false
	}
	userPrompt := llm.BuildClassifyPrompt(r.GameName, r.Stars, r.ReviewText, nil)
	sys := "You are a strict game-review analyst. Respond with ONLY a JSON object for the four dimensions."
	return domain.FinetuneExample{
		ReviewID: r.ID,
		GameName: r.GameName,
		Stars:    r.Stars,
		Messages: []domain.FinetuneChatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: userPrompt},
			{Role: "assistant", Content: target},
		},
	}, true
}

func targetJSONFromFeedback(fb domain.ClassificationFeedback) (string, bool) {
	type dim struct {
		Label      string  `json:"label"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	pick := func(j domain.DimJudgment, name string) (dim, bool) {
		label := strings.TrimSpace(j.ModelLabel)
		if !j.Correct {
			label = strings.TrimSpace(j.CorrectLabel)
		}
		if label == "" {
			return dim{}, false
		}
		reason := "human-confirmed label"
		if !j.Correct {
			reason = "human-corrected label"
		}
		if note := strings.TrimSpace(fb.Note); note != "" && name == "consistency" {
			reason = note
		}
		conf := 0.92
		if !j.Correct {
			conf = 0.95
		}
		return dim{Label: label, Confidence: conf, Reason: reason}, true
	}
	c, ok1 := pick(fb.Consistency, "consistency")
	a, ok2 := pick(fb.Authenticity, "authenticity")
	e, ok3 := pick(fb.Experience, "experience")
	u, ok4 := pick(fb.Usefulness, "usefulness")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return "", false
	}
	payload := map[string]dim{
		"consistency":  c,
		"authenticity": a,
		"experience":   e,
		"usefulness":   u,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(raw), true
}
