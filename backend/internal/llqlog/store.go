package llqlog

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// Store is an in-memory ring buffer of recent LLM/MCP queries.
type Store struct {
	mu      sync.RWMutex
	entries []domain.QueryLogEntry
	max     int
}

// New creates a log store retaining the last max entries.
func New(max int) *Store {
	if max <= 0 {
		max = 200
	}
	return &Store{max: max, entries: make([]domain.QueryLogEntry, 0, max)}
}

// Add appends a query log entry.
func (s *Store) Add(userID, tool, query, adapterID, modelID string, latencyMS int, ok bool, errMsg string) domain.QueryLogEntry {
	e := domain.QueryLogEntry{
		ID:        uuid.NewString(),
		UserID:    userID,
		Tool:      tool,
		Query:     truncate(query, 500),
		AdapterID: adapterID,
		ModelID:   modelID,
		LatencyMS: latencyMS,
		OK:        ok,
		Error:     errMsg,
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	if len(s.entries) > s.max {
		s.entries = s.entries[len(s.entries)-s.max:]
	}
	return e
}

// List returns newest-first entries (up to limit).
func (s *Store) List(limit int) []domain.QueryLogEntry {
	if limit <= 0 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.entries)
	if n == 0 {
		return nil
	}
	if limit > n {
		limit = n
	}
	out := make([]domain.QueryLogEntry, 0, limit)
	for i := n - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.entries[i])
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
