package repository

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

const storeFileName = "store.json"

type persistSnapshot struct {
	Users           []persistUser                         `json:"users"`
	RefreshTokens   []persistRefresh                      `json:"refresh_tokens"`
	Reviews         []domain.Review                       `json:"reviews"`
	Classifications map[string][]domain.Classification    `json:"classifications"`
	Breakdowns      map[string][]domain.ScoreBreakdownRow `json:"breakdowns"`
}

type persistUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type persistRefresh struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"token_hash"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// OpenMemoryStore loads a file-backed memory store from dataDir (creates dir if needed).
func OpenMemoryStore(dataDir string) (*MemoryStore, error) {
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	m := NewMemoryStore()
	m.dataDir = dataDir
	path := filepath.Join(dataDir, storeFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, fmt.Errorf("read store: %w", err)
	}
	var snap persistSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("decode store: %w", err)
	}
	for _, u := range snap.Users {
		user := domain.User{
			ID:           u.ID,
			Email:        u.Email,
			Name:         u.Name,
			PasswordHash: u.PasswordHash,
			CreatedAt:    u.CreatedAt,
		}
		m.usersByID[user.ID] = user
		m.usersByEmail[user.Email] = user.ID
	}
	for _, t := range snap.RefreshTokens {
		tok := domain.RefreshToken{
			ID:        t.ID,
			UserID:    t.UserID,
			TokenHash: t.TokenHash,
			ExpiresAt: t.ExpiresAt,
			RevokedAt: t.RevokedAt,
			CreatedAt: t.CreatedAt,
		}
		m.refreshByID[tok.ID] = tok
		m.refreshByHash[tok.TokenHash] = tok.ID
	}
	for _, r := range snap.Reviews {
		m.reviewsByID[r.ID] = r
	}
	if snap.Classifications != nil {
		m.classifications = snap.Classifications
	}
	if snap.Breakdowns != nil {
		m.breakdowns = snap.Breakdowns
	}
	return m, nil
}

// persistLocked writes the store to disk. Caller must hold m.mu.
func (m *MemoryStore) persistLocked() error {
	if m.dataDir == "" {
		return nil
	}
	snap := persistSnapshot{
		Users:           make([]persistUser, 0, len(m.usersByID)),
		RefreshTokens:   make([]persistRefresh, 0, len(m.refreshByID)),
		Reviews:         make([]domain.Review, 0, len(m.reviewsByID)),
		Classifications: m.classifications,
		Breakdowns:      m.breakdowns,
	}
	for _, u := range m.usersByID {
		snap.Users = append(snap.Users, persistUser{
			ID:           u.ID,
			Email:        u.Email,
			Name:         u.Name,
			PasswordHash: u.PasswordHash,
			CreatedAt:    u.CreatedAt,
		})
	}
	for _, t := range m.refreshByID {
		snap.RefreshTokens = append(snap.RefreshTokens, persistRefresh{
			ID:        t.ID,
			UserID:    t.UserID,
			TokenHash: t.TokenHash,
			ExpiresAt: t.ExpiresAt,
			RevokedAt: t.RevokedAt,
			CreatedAt: t.CreatedAt,
		})
	}
	for _, r := range m.reviewsByID {
		snap.Reviews = append(snap.Reviews, r)
	}

	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.dataDir, storeFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
