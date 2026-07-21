package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/repository"
)

const (
	minPasswordLen = 8
	bcryptCost     = bcrypt.DefaultCost
)

// AuthService implements registration, login, session rotation, and profile updates.
type AuthService struct {
	store  repository.Store
	tokens *auth.TokenManager
	now    func() time.Time
}

// NewAuthService wires the auth use cases.
func NewAuthService(store repository.Store, tokens *auth.TokenManager) *AuthService {
	return &AuthService{
		store:  store,
		tokens: tokens,
		now:    time.Now,
	}
}

// Register creates a user and returns a fresh session.
func (s *AuthService) Register(ctx context.Context, email, name, password string) (domain.AuthTokens, error) {
	email = normalizeEmail(email)
	name = strings.TrimSpace(name)
	if err := validateCredentials(email, name, password); err != nil {
		return domain.AuthTokens{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return domain.AuthTokens{}, err
	}

	user := domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		Name:         name,
		PasswordHash: string(hash),
		CreatedAt:    s.now().UTC(),
	}
	created, err := s.store.CreateUser(ctx, user)
	if err != nil {
		return domain.AuthTokens{}, err
	}
	return s.issueSession(ctx, created)
}

// Login verifies credentials and returns a fresh session.
func (s *AuthService) Login(ctx context.Context, email, password string) (domain.AuthTokens, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return domain.AuthTokens{}, domain.ErrValidation
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.AuthTokens{}, domain.ErrInvalidCredentials
		}
		return domain.AuthTokens{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return domain.AuthTokens{}, domain.ErrInvalidCredentials
	}
	return s.issueSession(ctx, user)
}

// Refresh rotates the refresh token and issues a new access token.
func (s *AuthService) Refresh(ctx context.Context, refreshRaw string) (domain.AuthTokens, error) {
	refreshRaw = strings.TrimSpace(refreshRaw)
	if refreshRaw == "" {
		return domain.AuthTokens{}, domain.ErrUnauthorized
	}

	stored, err := s.store.GetRefreshTokenByHash(ctx, auth.HashRefreshToken(refreshRaw))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.AuthTokens{}, domain.ErrUnauthorized
		}
		return domain.AuthTokens{}, err
	}

	now := s.now().UTC()
	if stored.RevokedAt != nil || now.After(stored.ExpiresAt) {
		// Reuse of a revoked/expired token — revoke all for this user (theft detection).
		_ = s.store.RevokeUserRefreshTokens(ctx, stored.UserID, now, "")
		return domain.AuthTokens{}, domain.ErrUnauthorized
	}

	if err := s.store.RevokeRefreshToken(ctx, stored.ID, now); err != nil {
		return domain.AuthTokens{}, err
	}

	user, err := s.store.GetUserByID(ctx, stored.UserID)
	if err != nil {
		return domain.AuthTokens{}, err
	}
	return s.issueSession(ctx, user)
}

// Logout revokes the presented refresh token.
func (s *AuthService) Logout(ctx context.Context, refreshRaw string) error {
	refreshRaw = strings.TrimSpace(refreshRaw)
	if refreshRaw == "" {
		return domain.ErrValidation
	}
	stored, err := s.store.GetRefreshTokenByHash(ctx, auth.HashRefreshToken(refreshRaw))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil // idempotent
		}
		return err
	}
	return s.store.RevokeRefreshToken(ctx, stored.ID, s.now().UTC())
}

// Me returns the current user profile.
func (s *AuthService) Me(ctx context.Context, userID string) (domain.User, error) {
	return s.store.GetUserByID(ctx, userID)
}

// UpdateMe updates name and/or email.
func (s *AuthService) UpdateMe(ctx context.Context, userID, email, name string) (domain.User, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if email != "" {
		user.Email = normalizeEmail(email)
		if user.Email == "" || !strings.Contains(user.Email, "@") {
			return domain.User{}, domain.ErrValidation
		}
	}
	if name != "" {
		user.Name = strings.TrimSpace(name)
		if user.Name == "" {
			return domain.User{}, domain.ErrValidation
		}
	}
	return s.store.UpdateUser(ctx, user)
}

// ChangePassword updates the password and revokes other sessions.
func (s *AuthService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword, currentRefreshRaw string) error {
	if len(newPassword) < minPasswordLen {
		return domain.ErrValidation
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return domain.ErrInvalidCredentials
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return err
	}

	exceptID := ""
	if currentRefreshRaw != "" {
		if stored, err := s.store.GetRefreshTokenByHash(ctx, auth.HashRefreshToken(currentRefreshRaw)); err == nil {
			exceptID = stored.ID
		}
	}
	return s.store.RevokeUserRefreshTokens(ctx, userID, s.now().UTC(), exceptID)
}

// ListSessions returns active (non-revoked, non-expired) sessions for the user.
func (s *AuthService) ListSessions(ctx context.Context, userID, currentRefreshRaw string) ([]domain.SessionView, error) {
	tokens, err := s.store.ListUserRefreshTokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	currentHash := ""
	if currentRefreshRaw != "" {
		currentHash = auth.HashRefreshToken(currentRefreshRaw)
	}
	now := s.now().UTC()
	out := make([]domain.SessionView, 0, len(tokens))
	for _, t := range tokens {
		if t.RevokedAt != nil || now.After(t.ExpiresAt) {
			continue
		}
		out = append(out, domain.SessionView{
			ID:        t.ID,
			ExpiresAt: t.ExpiresAt,
			RevokedAt: t.RevokedAt,
			CreatedAt: t.CreatedAt,
			Current:   currentHash != "" && t.TokenHash == currentHash,
		})
	}
	return out, nil
}

// ListGames returns distinct game names the user has reviewed (empty until reviews exist).
func (s *AuthService) ListGames(ctx context.Context, userID string) ([]string, error) {
	return s.store.ListUserGames(ctx, userID)
}

func (s *AuthService) issueSession(ctx context.Context, user domain.User) (domain.AuthTokens, error) {
	now := s.now().UTC()
	access, err := s.tokens.IssueAccessToken(user.ID, user.Email, now)
	if err != nil {
		return domain.AuthTokens{}, err
	}
	raw, hash, err := s.tokens.NewRefreshToken()
	if err != nil {
		return domain.AuthTokens{}, err
	}
	rt := domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: now.Add(s.tokens.RefreshTTL()),
		CreatedAt: now,
	}
	if _, err := s.store.CreateRefreshToken(ctx, rt); err != nil {
		return domain.AuthTokens{}, err
	}

	user.PasswordHash = ""
	return domain.AuthTokens{
		AccessToken:  access,
		RefreshToken: raw,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.tokens.AccessTTL().Seconds()),
		User:         user,
	}, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateCredentials(email, name, password string) error {
	if email == "" || !strings.Contains(email, "@") {
		return domain.ErrValidation
	}
	if name == "" {
		return domain.ErrValidation
	}
	if len(password) < minPasswordLen {
		return domain.ErrValidation
	}
	return nil
}
