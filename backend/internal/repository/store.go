package repository

import (
	"context"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// Store is the persistence boundary shared by memory and Postgres implementations.
type Store interface {
	Ping(ctx context.Context) error
	Name() string

	// Users
	CreateUser(ctx context.Context, user domain.User) (domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	GetUserByID(ctx context.Context, id string) (domain.User, error)
	UpdateUser(ctx context.Context, user domain.User) (domain.User, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) error

	// Refresh tokens / sessions
	CreateRefreshToken(ctx context.Context, token domain.RefreshToken) (domain.RefreshToken, error)
	GetRefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id string, at time.Time) error
	RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time, exceptID string) error
	ListUserRefreshTokens(ctx context.Context, userID string) ([]domain.RefreshToken, error)

	// Games (auth-scoped; empty until reviews exist)
	ListUserGames(ctx context.Context, userID string) ([]string, error)
}
