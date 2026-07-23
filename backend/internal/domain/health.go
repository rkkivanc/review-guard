package domain

// HealthStatus is returned by GET /health and GET /ready.
type HealthStatus struct {
	Status   string `json:"status"`
	Store    string `json:"store"`
	DBPingOK bool   `json:"db_ping_ok"`
	Version  string `json:"version"`
}

// PublicConfig is the subset of runtime knobs exposed by GET /config.
type PublicConfig struct {
	ClassificationRuns        int                `json:"classification_runs"`
	ClassificationTemperature float64            `json:"classification_temperature"`
	TrustThreshold            float64            `json:"trust_threshold"`
	Weights                   map[string]float64 `json:"weights"`
	Flags                     map[string]bool    `json:"flags"`
	ModelID                   string             `json:"model_id"`
	LLMURLConfigured          bool               `json:"llm_configured"`
}

// VersionInfo is returned by GET /version.
type VersionInfo struct {
	Version string `json:"version"`
	Name    string `json:"name"`
}
