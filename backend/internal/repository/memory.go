package repository

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// MemoryStore is the local-dev fallback when DATABASE_URL is empty.
type MemoryStore struct {
	mu            sync.RWMutex
	usersByID     map[string]domain.User
	usersByEmail  map[string]string // email → id
	refreshByID   map[string]domain.RefreshToken
	refreshByHash map[string]string // hash → id
}

// NewMemoryStore constructs an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usersByID:     make(map[string]domain.User),
		usersByEmail:  make(map[string]string),
		refreshByID:   make(map[string]domain.RefreshToken),
		refreshByHash: make(map[string]string),
	}
}

// Name identifies the store implementation for health responses.
func (m *MemoryStore) Name() string {
	return "memory"
}

// Ping always succeeds for the in-memory store.
func (m *MemoryStore) Ping(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (m *MemoryStore) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	email := strings.ToLower(strings.TrimSpace(user.Email))
	if _, exists := m.usersByEmail[email]; exists {
		return domain.User{}, domain.ErrConflict
	}
	user.Email = email
	m.usersByID[user.ID] = user
	m.usersByEmail[email] = user.ID
	return user, nil
}

func (m *MemoryStore) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.usersByEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return m.usersByID[id], nil
}

func (m *MemoryStore) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	u, ok := m.usersByID[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (m *MemoryStore) UpdateUser(ctx context.Context, user domain.User) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.usersByID[user.ID]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}

	newEmail := strings.ToLower(strings.TrimSpace(user.Email))
	if newEmail != existing.Email {
		if otherID, taken := m.usersByEmail[newEmail]; taken && otherID != user.ID {
			return domain.User{}, domain.ErrConflict
		}
		delete(m.usersByEmail, existing.Email)
		m.usersByEmail[newEmail] = user.ID
	}

	existing.Email = newEmail
	existing.Name = user.Name
	m.usersByID[user.ID] = existing
	return existing, nil
}

func (m *MemoryStore) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.usersByID[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.PasswordHash = passwordHash
	m.usersByID[userID] = u
	return nil
}

func (m *MemoryStore) CreateRefreshToken(ctx context.Context, token domain.RefreshToken) (domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return domain.RefreshToken{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.refreshByID[token.ID] = token
	m.refreshByHash[token.TokenHash] = token.ID
	return token, nil
}

func (m *MemoryStore) GetRefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return domain.RefreshToken{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.refreshByHash[hash]
	if !ok {
		return domain.RefreshToken{}, domain.ErrNotFound
	}
	return m.refreshByID[id], nil
}

func (m *MemoryStore) RevokeRefreshToken(ctx context.Context, id string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.refreshByID[id]
	if !ok {
		return domain.ErrNotFound
	}
	if t.RevokedAt == nil {
		t.RevokedAt = &at
		m.refreshByID[id] = t
	}
	return nil
}

func (m *MemoryStore) RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time, exceptID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, t := range m.refreshByID {
		if t.UserID != userID || id == exceptID {
			continue
		}
		if t.RevokedAt == nil {
			revoked := at
			t.RevokedAt = &revoked
			m.refreshByID[id] = t
		}
	}
	return nil
}

func (m *MemoryStore) ListUserRefreshTokens(ctx context.Context, userID string) ([]domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.RefreshToken, 0)
	for _, t := range m.refreshByID {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *MemoryStore) ListUserGames(ctx context.Context, userID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []string{}, nil
}
