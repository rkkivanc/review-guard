package service

import (
	"context"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// UserRepository is the auth-facing user persistence surface (ISP).
type UserRepository interface {
	CreateUser(ctx context.Context, user domain.User) (domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	GetUserByID(ctx context.Context, id string) (domain.User, error)
	UpdateUser(ctx context.Context, user domain.User) (domain.User, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
}

// SessionRepository is the auth-facing session persistence surface.
type SessionRepository interface {
	CreateRefreshToken(ctx context.Context, token domain.RefreshToken) (domain.RefreshToken, error)
	GetRefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error)
	ConsumeRefreshToken(ctx context.Context, hash string, at time.Time) (domain.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id string, at time.Time) error
	RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time, exceptID string) error
	ListUserRefreshTokens(ctx context.Context, userID string) ([]domain.RefreshToken, error)
	ListUserGames(ctx context.Context, userID string) ([]string, error)
	BumpTokenVersion(ctx context.Context, userID string) (int64, error)
}

// AuthStore combines user + session persistence for AuthService.
type AuthStore interface {
	UserRepository
	SessionRepository
}

// AuthService implements registration, login, session rotation, and profile updates.
type AuthService struct {
	store        AuthStore
	tokens       *auth.TokenManager
	isAdminEmail func(string) bool
	now          func() time.Time
}

// NewAuthService wires the auth use cases.
func NewAuthService(store AuthStore, tokens *auth.TokenManager, isAdminEmail func(string) bool) *AuthService {
	if isAdminEmail == nil {
		isAdminEmail = func(string) bool { return false }
	}
	return &AuthService{
		store:        store,
		tokens:       tokens,
		isAdminEmail: isAdminEmail,
		now:          time.Now,
	}
}
