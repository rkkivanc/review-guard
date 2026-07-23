package service

import (
	"context"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/scoring"
)

// ReviewRepository is the review-facing persistence surface (ISP).
type ReviewRepository interface {
	CreateReview(ctx context.Context, review domain.Review, runs []domain.Classification, breakdown []domain.ScoreBreakdownRow) (domain.Review, error)
	GetReviewForUser(ctx context.Context, userID, reviewID string) (domain.Review, error)
	ListReviews(ctx context.Context, userID string, filter domain.ReviewListFilter) ([]domain.Review, int, error)
	DeleteReview(ctx context.Context, userID, reviewID string) error
	GetClassifications(ctx context.Context, reviewID string) ([]domain.Classification, error)
	GetScoreBreakdown(ctx context.Context, reviewID string) ([]domain.ScoreBreakdownRow, error)
	ReplaceScore(ctx context.Context, userID string, review domain.Review, breakdown []domain.ScoreBreakdownRow) error
	ListReviewsForGame(ctx context.Context, userID, gameName string) ([]domain.Review, error)
	UpsertGradeFeedback(ctx context.Context, userID, reviewID string, fb domain.ClassificationFeedback) (domain.Review, error)
	ListGradeFeedback(ctx context.Context, userID string, limit int) ([]domain.FeedbackHint, error)
	BuildAnalytics(ctx context.Context, userID string, threshold float64) (domain.Analytics, error)
}

// ReviewService persists reviews and always recomputes trust server-side.
type ReviewService struct {
	store ReviewRepository
	cfg   config.Config
	now   func() time.Time
}

// NewReviewService constructs a ReviewService.
func NewReviewService(store ReviewRepository, cfg config.Config) *ReviewService {
	return &ReviewService{store: store, cfg: cfg, now: time.Now}
}

// CreateReviewInput is the client payload for POST /reviews (no trust fields).
type CreateReviewInput struct {
	GameName   string
	Stars      int
	ReviewText string
	LatencyMS  int
	Runs       []scoring.Run
}

// ScoreResponse is the typed GET /reviews/{id}/score payload.
type ScoreResponse struct {
	ReviewID    string                      `json:"review_id"`
	TrustScore  float64                     `json:"trust_score"`
	Grade       string                      `json:"grade"`
	NeedsReview bool                        `json:"needs_review"`
	Composite   float64                     `json:"composite"`
	Penalty     float64                     `json:"penalty"`
	Breakdown   []domain.ScoreBreakdownRow  `json:"breakdown"`
}

// ReviewListResponse is the typed GET /reviews payload.
type ReviewListResponse struct {
	Items  []domain.Review `json:"items"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// FeedbackHintsResponse wraps hint rows.
type FeedbackHintsResponse struct {
	Hints []domain.FeedbackHint `json:"hints"`
}
