package domain

import "time"

// AdapterMeta describes a PEFT (LoRA) adapter available for hot-swap.
type AdapterMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	Active      bool   `json:"active"`
}

// LLMRuntimeConfig is the hot-swappable LLM control plane (no restart).
type LLMRuntimeConfig struct {
	SystemPrompt string  `json:"system_prompt"`
	MaxTokens    int     `json:"max_tokens"`
	Temperature  float64 `json:"temperature"`
	TopP         float64 `json:"top_p"`
	ActiveAdapter string `json:"active_adapter"`
}

// QueryLogEntry records an MCP/LLM call for the admin monitor.
type QueryLogEntry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Tool      string    `json:"tool"`
	Query     string    `json:"query"`
	AdapterID string    `json:"adapter_id,omitempty"`
	ModelID   string    `json:"model_id,omitempty"`
	LatencyMS int       `json:"latency_ms"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// RichResult is the enriched MCP tool response for the frontend.
type RichResult struct {
	Content   string         `json:"content"`
	Format    string         `json:"format"` // markdown | json
	Metadata  map[string]any `json:"metadata"`
	LatencyMS int            `json:"latency_ms"`
}
