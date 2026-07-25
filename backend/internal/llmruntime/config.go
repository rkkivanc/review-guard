package llmruntime

import (
	"sync"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// Store holds hot-swappable LLM settings (no process restart).
type Store struct {
	mu sync.RWMutex
	cfg domain.LLMRuntimeConfig
}

// New creates a runtime store with defaults.
func New(systemPrompt string, temperature float64, maxTokens int, topP float64) *Store {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	if topP <= 0 {
		topP = 1
	}
	if temperature < 0 {
		temperature = 0.7
	}
	return &Store{
		cfg: domain.LLMRuntimeConfig{
			SystemPrompt:  systemPrompt,
			MaxTokens:     maxTokens,
			Temperature:   temperature,
			TopP:          topP,
			ActiveAdapter: "",
		},
	}
}

// Get returns a copy of the current config.
func (s *Store) Get() domain.LLMRuntimeConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update merges non-nil fields from patch into the runtime config.
func (s *Store) Update(patch domain.LLMRuntimeConfig, setAdapter, setPrompt, setMax, setTemp, setTopP bool) domain.LLMRuntimeConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if setPrompt {
		s.cfg.SystemPrompt = patch.SystemPrompt
	}
	if setMax && patch.MaxTokens > 0 {
		s.cfg.MaxTokens = patch.MaxTokens
	}
	if setTemp && patch.Temperature >= 0 {
		s.cfg.Temperature = patch.Temperature
	}
	if setTopP && patch.TopP > 0 {
		s.cfg.TopP = patch.TopP
	}
	if setAdapter {
		s.cfg.ActiveAdapter = patch.ActiveAdapter
	}
	return s.cfg
}

// SetActiveAdapter hot-swaps the PEFT adapter id.
func (s *Store) SetActiveAdapter(id string) domain.LLMRuntimeConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.ActiveAdapter = id
	return s.cfg
}
