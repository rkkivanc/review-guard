package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

const storeFileName = "store.json"

var persistBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

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
	TokenVersion int64     `json:"token_version"`
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
	m.persistCh = make(chan struct{}, 1)
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})

	path := filepath.Join(dataDir, storeFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read store: %w", err)
		}
	} else {
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
				TokenVersion: u.TokenVersion,
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
		m.rebuildIndexesLocked()
	}

	go m.persistLoop()
	return m, nil
}

func (m *MemoryStore) persistLoop() {
	defer close(m.doneCh)
	for {
		select {
		case <-m.persistCh:
			// Drain coalesced signals so one flush covers a burst of writes.
			drained := true
			for drained {
				select {
				case <-m.persistCh:
				default:
					drained = false
				}
			}
			logPersistErr(m.flushToDisk())
		case <-m.stopCh:
			// Final drain + flush for durability on shutdown.
			select {
			case <-m.persistCh:
			default:
			}
			for {
				select {
				case <-m.persistCh:
				default:
					logPersistErr(m.flushToDisk())
					return
				}
			}
		}
	}
}

// snapshotUnderRLock builds a deep-enough copy for disk. Caller must hold at least RLock.
func (m *MemoryStore) snapshotUnderRLock() persistSnapshot {
	snap := persistSnapshot{
		Users:           make([]persistUser, 0, len(m.usersByID)),
		RefreshTokens:   make([]persistRefresh, 0, len(m.refreshByID)),
		Reviews:         make([]domain.Review, 0, len(m.reviewsByID)),
		Classifications: make(map[string][]domain.Classification, len(m.classifications)),
		Breakdowns:      make(map[string][]domain.ScoreBreakdownRow, len(m.breakdowns)),
	}
	for _, u := range m.usersByID {
		snap.Users = append(snap.Users, persistUser{
			ID:           u.ID,
			Email:        u.Email,
			Name:         u.Name,
			PasswordHash: u.PasswordHash,
			TokenVersion: u.TokenVersion,
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
	for id, runs := range m.classifications {
		cp := make([]domain.Classification, len(runs))
		copy(cp, runs)
		snap.Classifications[id] = cp
	}
	for id, bd := range m.breakdowns {
		cp := make([]domain.ScoreBreakdownRow, len(bd))
		copy(cp, bd)
		snap.Breakdowns[id] = cp
	}
	return snap
}

func (m *MemoryStore) flushToDisk() error {
	if m.dataDir == "" {
		return nil
	}
	m.mu.RLock()
	snap := m.snapshotUnderRLock()
	m.mu.RUnlock()

	buf := persistBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer persistBufPool.Put(buf)

	enc := json.NewEncoder(buf)
	if err := enc.Encode(snap); err != nil {
		return err
	}
	raw := make([]byte, buf.Len())
	copy(raw, buf.Bytes())

	path := filepath.Join(m.dataDir, storeFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
