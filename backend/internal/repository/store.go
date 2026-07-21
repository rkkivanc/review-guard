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

	// Games
	ListUserGames(ctx context.Context, userID string) ([]string, error)

	// Reviews
	CreateReview(ctx context.Context, review domain.Review, runs []domain.Classification, breakdown []domain.ScoreBreakdownRow) (domain.Review, error)
	GetReviewForUser(ctx context.Context, userID, reviewID string) (domain.Review, error)
	ListReviews(ctx context.Context, userID string, filter domain.ReviewListFilter) ([]domain.Review, int, error)
	DeleteReview(ctx context.Context, userID, reviewID string) error
	GetClassifications(ctx context.Context, reviewID string) ([]domain.Classification, error)
	GetScoreBreakdown(ctx context.Context, reviewID string) ([]domain.ScoreBreakdownRow, error)
	ReplaceScore(ctx context.Context, userID string, review domain.Review, breakdown []domain.ScoreBreakdownRow) error
	ListAllReviewsForUser(ctx context.Context, userID string) ([]domain.Review, error)
	ListReviewsForGame(ctx context.Context, userID, gameName string) ([]domain.Review, error)
}
