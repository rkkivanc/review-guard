package service

import (
	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
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
			"llm_in_browser": true,
			"server_scores":  true,
		},
		// Model stays in-browser; backend only advertises the expected id.
		ModelID: "gemma-2-2b-it-q4f16_1-MLC",
	}
}

// Version returns build/version metadata.
func (s *ConfigService) Version() domain.VersionInfo {
	return domain.VersionInfo{
		Version: s.cfg.Version,
		Name:    "reviewguard",
	}
}
