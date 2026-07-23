package service

import (
	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"strings"
)

// ConfigService exposes public runtime configuration to clients.
type ConfigService struct {
	cfg config.Config
}

// NewConfigService constructs a ConfigService.
func NewConfigService(cfg config.Config) *ConfigService {
	return &ConfigService{cfg: cfg}
}

// Public returns the knobs the SPA needs (thresholds, runs, weights, flags).
func (s *ConfigService) Public() domain.PublicConfig {
	llmConfigured := strings.TrimSpace(s.cfg.MLCLLMURL) != ""
	return domain.PublicConfig{
		ClassificationRuns:        s.cfg.ClassificationRuns,
		ClassificationTemperature: s.cfg.ClassificationTemperature,
		TrustThreshold:            s.cfg.TrustThreshold,
		Weights: map[string]float64{
			"authenticity": s.cfg.Weights.Authenticity,
			"experience":   s.cfg.Weights.Experience,
			"consistency":  s.cfg.Weights.Consistency,
			"usefulness":   s.cfg.Weights.Usefulness,
		},
		Flags: map[string]bool{
			"memory_store":   s.cfg.UsingMemoryStore(),
			"llm_in_browser": false,
			"server_scores":  true,
			"llm_backend":    llmConfigured,
		},
		ModelID:          s.cfg.MLCModelID,
		LLMURLConfigured: llmConfigured,
	}
}

// Version returns build/version metadata.
func (s *ConfigService) Version() domain.VersionInfo {
	return domain.VersionInfo{
		Version: s.cfg.Version,
		Name:    "reviewguard",
	}
}
