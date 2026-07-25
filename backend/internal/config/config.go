package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds process-wide settings loaded from the environment.
type Config struct {
	Port            string
	DatabaseURL     string
	DataDir         string
	JWTSecret       string
	CORSOrigins     []string
	TrustedProxies  []string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Version         string

	// Scoring / classification knobs (exposed via GET /config).
	ClassificationRuns        int
	ClassificationTemperature float64
	TrustThreshold            float64
	Weights                   DimensionWeights

	// MLC LLM (OpenAI-compatible) service.
	MLCLLMURL   string
	MLCModelID  string
	LLMTimeout  time.Duration

	// FINAL BOSS control plane.
	PEFTAdaptersDir string
	AdminEmails     []string
	DefaultSystemPrompt string
}

// DimensionWeights are the composite trust-score weights from PROJECT_CONTEXT §4.
type DimensionWeights struct {
	Authenticity float64 `json:"authenticity"`
	Experience   float64 `json:"experience"`
	Consistency  float64 `json:"consistency"`
	Usefulness   float64 `json:"usefulness"`
}

// Load reads configuration from environment variables with safe local defaults.
func Load() Config {
	return Config{
		Port:            envOr("PORT", "8080"),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DataDir:         envOr("DATA_DIR", "data"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		CORSOrigins:     splitCSV(envOr("CORS_ORIGINS", "http://localhost:3000")),
		TrustedProxies:  splitCSV(os.Getenv("TRUSTED_PROXIES")),
		AccessTokenTTL:  envDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: envDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		Version:         envOr("APP_VERSION", "0.1.0-dev"),

		ClassificationRuns:        envInt("CLASSIFICATION_RUNS", 3),
		ClassificationTemperature: envFloat("CLASSIFICATION_TEMPERATURE", 0.7),
		TrustThreshold:            envFloat("TRUST_THRESHOLD", 70),
		Weights: DimensionWeights{
			Authenticity: 0.30,
			Experience:   0.30,
			Consistency:  0.25,
			Usefulness:   0.15,
		},

		// Local `go run` defaults to the host stub; Docker Compose overrides to http://llm:8000.
		MLCLLMURL:  envOr("MLC_LLM_URL", "http://localhost:8000"),
		MLCModelID: envOr("MLC_MODEL_ID", "gemma-2-2b-it-q4f16_1-MLC"),
		LLMTimeout: envDuration("LLM_TIMEOUT", 60*time.Second),

		PEFTAdaptersDir: envOr("PEFT_ADAPTERS_DIR", defaultAdaptersDir()),
		AdminEmails:     splitCSV(envOr("ADMIN_EMAILS", "admin@reviewguard.local")),
		DefaultSystemPrompt: envOr("LLM_SYSTEM_PROMPT", defaultSystemPrompt),
	}
}

const defaultSystemPrompt = `You are ReviewGuard's local analyst. Be precise, structured, and prefer Markdown with tables when summarizing knowledge. Never invent credentials or external URLs.`

// IsAdminEmail reports whether email should receive the admin role on register.
func (c Config) IsAdminEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, a := range c.AdminEmails {
		if strings.ToLower(strings.TrimSpace(a)) == email {
			return true
		}
	}
	return false
}

// UsingMemoryStore reports whether the process should use the in-memory repository.
func (c Config) UsingMemoryStore() bool {
	return c.DatabaseURL == ""
}

// EnsureJWTSecret requires JWT_SECRET when Postgres is configured.
// For file-backed memory mode it loads or creates dataDir/jwt.secret so restarts keep sessions.
func (c *Config) EnsureJWTSecret() (fromFile bool, err error) {
	if strings.TrimSpace(c.JWTSecret) != "" {
		return false, nil
	}
	if !c.UsingMemoryStore() {
		return false, fmt.Errorf("JWT_SECRET is required when DATABASE_URL is set")
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return false, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(c.DataDir, "jwt.secret")
	if raw, err := os.ReadFile(path); err == nil {
		sec := strings.TrimSpace(string(raw))
		if sec != "" {
			c.JWTSecret = sec
			return true, nil
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("read jwt secret: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return false, fmt.Errorf("generate jwt secret: %w", err)
	}
	c.JWTSecret = hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(c.JWTSecret+"\n"), 0o600); err != nil {
		return false, fmt.Errorf("write jwt secret: %w", err)
	}
	return true, nil
}

func defaultAdaptersDir() string {
	// Prefer repo-root peft-adapters when the process cwd is backend/.
	if st, err := os.Stat("../peft-adapters"); err == nil && st.IsDir() {
		return "../peft-adapters"
	}
	return "peft-adapters"
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
