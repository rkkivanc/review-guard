package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds process-wide settings loaded from the environment.
type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       string
	CORSOrigins     []string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Version         string

	// Scoring / classification knobs (exposed via GET /config).
	ClassificationRuns        int
	ClassificationTemperature float64
	TrustThreshold            float64
	Weights                   DimensionWeights
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
		JWTSecret:       os.Getenv("JWT_SECRET"),
		CORSOrigins:     splitCSV(envOr("CORS_ORIGINS", "http://localhost:3000")),
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
	}
}

// UsingMemoryStore reports whether the process should use the in-memory repository.
func (c Config) UsingMemoryStore() bool {
	return c.DatabaseURL == ""
}

// EnsureJWTSecret requires JWT_SECRET when Postgres is configured; for memory-store
// local dev it generates an ephemeral secret (sessions die on restart).
func (c *Config) EnsureJWTSecret() (ephemeral bool, err error) {
	if strings.TrimSpace(c.JWTSecret) != "" {
		return false, nil
	}
	if !c.UsingMemoryStore() {
		return false, fmt.Errorf("JWT_SECRET is required when DATABASE_URL is set")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return false, fmt.Errorf("generate jwt secret: %w", err)
	}
	c.JWTSecret = hex.EncodeToString(buf)
	return true, nil
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
