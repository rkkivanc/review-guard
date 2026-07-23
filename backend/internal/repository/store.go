package repository

import (
	"context"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// Store is the composite persistence boundary (memory + Postgres).
// Prefer injecting the narrower consumer interfaces defined in service packages.
type Store interface {
	Ping(ctx context.Context) error
	Name() string
	Close() error

	CreateUser(ctx context.Context, user domain.User) (domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	GetUserByID(ctx context.Context, id string) (domain.User, error)
	UpdateUser(ctx context.Context, user domain.User) (domain.User, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) error

	CreateRefreshToken(ctx context.Context, token domain.RefreshToken) (domain.RefreshToken, error)
	GetRefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error)
	// ConsumeRefreshToken atomically revokes a still-valid refresh token.
	// Returns ErrTokenReuse if the token exists but was already revoked/expired,
	// ErrNotFound if unknown, or the consumed token on success.
	ConsumeRefreshToken(ctx context.Context, hash string, at time.Time) (domain.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id string, at time.Time) error
	RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time, exceptID string) error
	ListUserRefreshTokens(ctx context.Context, userID string) ([]domain.RefreshToken, error)
	BumpTokenVersion(ctx context.Context, userID string) (int64, error)

	ListUserGames(ctx context.Context, userID string) ([]string, error)

	CreateReview(ctx context.Context, review domain.Review, runs []domain.Classification, breakdown []domain.ScoreBreakdownRow) (domain.Review, error)
	GetReviewForUser(ctx context.Context, userID, reviewID string) (domain.Review, error)
	ListReviews(ctx context.Context, userID string, filter domain.ReviewListFilter) ([]domain.Review, int, error)
	DeleteReview(ctx context.Context, userID, reviewID string) error
	GetClassifications(ctx context.Context, reviewID string) ([]domain.Classification, error)
	GetScoreBreakdown(ctx context.Context, reviewID string) ([]domain.ScoreBreakdownRow, error)
	ReplaceScore(ctx context.Context, userID string, review domain.Review, breakdown []domain.ScoreBreakdownRow) error
	ListAllReviewsForUser(ctx context.Context, userID string) ([]domain.Review, error)
	ListReviewsForGame(ctx context.Context, userID, gameName string) ([]domain.Review, error)
	UpsertGradeFeedback(ctx context.Context, userID, reviewID string, fb domain.ClassificationFeedback) (domain.Review, error)
	ListGradeFeedback(ctx context.Context, userID string, limit int) ([]domain.FeedbackHint, error)

	// BuildAnalytics aggregates dashboard metrics without N+1 round-trips.
	BuildAnalytics(ctx context.Context, userID string, threshold float64) (domain.Analytics, error)
}
