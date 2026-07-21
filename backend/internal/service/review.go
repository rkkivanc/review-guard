package service

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/repository"
	"github.com/masterfabric/review-guard/mf-backend/internal/scoring"
)

// ReviewService persists reviews and always recomputes trust server-side.
type ReviewService struct {
	store repository.Store
	cfg   config.Config
	now   func() time.Time
}

// NewReviewService constructs a ReviewService.
func NewReviewService(store repository.Store, cfg config.Config) *ReviewService {
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

// Create stores the review after scoring the provided runs.
func (s *ReviewService) Create(ctx context.Context, userID string, in CreateReviewInput) (domain.ReviewDetail, error) {
	if err := validateCreate(in, s.cfg.ClassificationRuns); err != nil {
		return domain.ReviewDetail{}, err
	}

	result := scoring.Score(in.Runs, s.weights(), s.cfg.TrustThreshold)
	now := s.now().UTC()
	reviewID := uuid.NewString()

	review := domain.Review{
		ID:          reviewID,
		UserID:      userID,
		GameName:    strings.TrimSpace(in.GameName),
		Stars:       in.Stars,
		ReviewText:  strings.TrimSpace(in.ReviewText),
		TrustScore:  result.TrustScore,
		Grade:       result.Grade,
		NeedsReview: result.NeedsReview,
		LatencyMS:   in.LatencyMS,
		CreatedAt:   now,
	}

	runs := make([]domain.Classification, len(in.Runs))
	for i, payload := range in.Runs {
		runs[i] = domain.Classification{
			ID:       uuid.NewString(),
			ReviewID: reviewID,
			RunIndex: i,
			Payload:  payload,
		}
	}
	breakdown := breakdownRows(reviewID, result)

	saved, err := s.store.CreateReview(ctx, review, runs, breakdown)
	if err != nil {
		return domain.ReviewDetail{}, err
	}
	return domain.ReviewDetail{
		Review:    saved,
		Runs:      runs,
		Breakdown: breakdown,
		Composite: result.Composite,
		Penalty:   result.Penalty,
	}, nil
}

// Get returns one review owned by the user (else not found).
func (s *ReviewService) Get(ctx context.Context, userID, reviewID string) (domain.ReviewDetail, error) {
	review, err := s.store.GetReviewForUser(ctx, userID, reviewID)
	if err != nil {
		return domain.ReviewDetail{}, err
	}
	runs, err := s.store.GetClassifications(ctx, reviewID)
	if err != nil {
		return domain.ReviewDetail{}, err
	}
	bd, err := s.store.GetScoreBreakdown(ctx, reviewID)
	if err != nil {
		return domain.ReviewDetail{}, err
	}
	// Recompute composite/penalty for response consistency without mutating.
	payloads := make([]scoring.Run, len(runs))
	for i, r := range runs {
		payloads[i] = r.Payload
	}
	result := scoring.Score(payloads, s.weights(), s.cfg.TrustThreshold)
	return domain.ReviewDetail{
		Review:    review,
		Runs:      runs,
		Breakdown: bd,
		Composite: result.Composite,
		Penalty:   result.Penalty,
	}, nil
}

// List returns a filtered page of reviews.
func (s *ReviewService) List(ctx context.Context, userID string, filter domain.ReviewListFilter) ([]domain.Review, int, error) {
	return s.store.ListReviews(ctx, userID, filter)
}

// Delete removes a review owned by the user.
func (s *ReviewService) Delete(ctx context.Context, userID, reviewID string) error {
	return s.store.DeleteReview(ctx, userID, reviewID)
}

// Rescore recomputes trust from stored runs (config/audit).
func (s *ReviewService) Rescore(ctx context.Context, userID, reviewID string) (domain.ReviewDetail, error) {
	review, err := s.store.GetReviewForUser(ctx, userID, reviewID)
	if err != nil {
		return domain.ReviewDetail{}, err
	}
	runs, err := s.store.GetClassifications(ctx, reviewID)
	if err != nil {
		return domain.ReviewDetail{}, err
	}
	payloads := make([]scoring.Run, len(runs))
	for i, r := range runs {
		payloads[i] = r.Payload
	}
	result := scoring.Score(payloads, s.weights(), s.cfg.TrustThreshold)
	review.TrustScore = result.TrustScore
	review.Grade = result.Grade
	review.NeedsReview = result.NeedsReview
	breakdown := breakdownRows(reviewID, result)
	if err := s.store.ReplaceScore(ctx, userID, review, breakdown); err != nil {
		return domain.ReviewDetail{}, err
	}
	return domain.ReviewDetail{
		Review:    review,
		Runs:      runs,
		Breakdown: breakdown,
		Composite: result.Composite,
		Penalty:   result.Penalty,
	}, nil
}

// ScoreOnly returns the breakdown for one review.
func (s *ReviewService) ScoreOnly(ctx context.Context, userID, reviewID string) (map[string]any, error) {
	detail, err := s.Get(ctx, userID, reviewID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"review_id":    detail.Review.ID,
		"trust_score":  detail.Review.TrustScore,
		"grade":        detail.Review.Grade,
		"needs_review": detail.Review.NeedsReview,
		"composite":    detail.Composite,
		"penalty":      detail.Penalty,
		"breakdown":    detail.Breakdown,
	}, nil
}

// Analytics builds dashboard aggregates; threshold previews would-flag count.
func (s *ReviewService) Analytics(ctx context.Context, userID string, threshold float64) (domain.Analytics, error) {
	if threshold <= 0 {
		threshold = s.cfg.TrustThreshold
	}
	reviews, err := s.store.ListAllReviewsForUser(ctx, userID)
	if err != nil {
		return domain.Analytics{}, err
	}

	out := domain.Analytics{
		TotalReviews:  len(reviews),
		GradeCounts:   map[string]int{"A": 0, "B": 0, "C": 0, "D": 0, "F": 0},
		DimensionDist: map[string]map[string]int{},
		Threshold:     threshold,
	}

	var trustSum float64
	for _, r := range reviews {
		trustSum += r.TrustScore
		if r.NeedsReview {
			out.FlaggedCount++
		}
		out.GradeCounts[r.Grade]++
		if r.TrustScore < threshold {
			out.WouldFlagAtThreshold++
		}

		bd, err := s.store.GetScoreBreakdown(ctx, r.ID)
		if err != nil {
			continue
		}
		for _, row := range bd {
			if out.DimensionDist[row.Dimension] == nil {
				out.DimensionDist[row.Dimension] = map[string]int{}
			}
			out.DimensionDist[row.Dimension][row.FinalLabel]++
		}
	}
	if len(reviews) > 0 {
		out.AvgTrustScore = trustSum / float64(len(reviews))
	}
	return out, nil
}

// GameAverage returns raw vs trust-weighted averages for one game.
func (s *ReviewService) GameAverage(ctx context.Context, userID, gameName string) (domain.GameAverage, error) {
	gameName, err := url.PathUnescape(gameName)
	if err != nil {
		gameName = strings.TrimSpace(gameName)
	}
	reviews, err := s.store.ListReviewsForGame(ctx, userID, gameName)
	if err != nil {
		return domain.GameAverage{}, err
	}
	if len(reviews) == 0 {
		return domain.GameAverage{}, domain.ErrNotFound
	}
	stars := make([]int, len(reviews))
	trusts := make([]float64, len(reviews))
	for i, r := range reviews {
		stars[i] = r.Stars
		trusts[i] = r.TrustScore
	}
	raw, weighted, inflation := scoring.GameAverages(stars, trusts)
	return domain.GameAverage{
		GameName:    reviews[0].GameName,
		ReviewCount: len(reviews),
		RawAvg:      raw,
		WeightedAvg: weighted,
		Inflation:   inflation,
	}, nil
}

// SubmitFeedback stores per-dimension label correctness (does not mutate trust_score).
func (s *ReviewService) SubmitFeedback(ctx context.Context, userID, reviewID string, fb domain.ClassificationFeedback) (domain.Review, error) {
	var err error
	if fb.Consistency, err = normalizeJudgment(fb.Consistency, []string{"aligned", "mismatched"}); err != nil {
		return domain.Review{}, err
	}
	if fb.Authenticity, err = normalizeJudgment(fb.Authenticity, []string{"genuine", "suspicious", "bot"}); err != nil {
		return domain.Review{}, err
	}
	if fb.Experience, err = normalizeJudgment(fb.Experience, []string{"experience_based", "speculative"}); err != nil {
		return domain.Review{}, err
	}
	if fb.Usefulness, err = normalizeJudgment(fb.Usefulness, []string{"useful", "neutral", "empty"}); err != nil {
		return domain.Review{}, err
	}
	fb.Note = strings.TrimSpace(fb.Note)
	fb.CreatedAt = s.now().UTC()
	return s.store.UpsertGradeFeedback(ctx, userID, reviewID, fb)
}

// FeedbackHints returns recent human label corrections for improving the in-browser prompt.
func (s *ReviewService) FeedbackHints(ctx context.Context, userID string, limit int) ([]domain.FeedbackHint, error) {
	return s.store.ListGradeFeedback(ctx, userID, limit)
}

func normalizeJudgment(j domain.DimJudgment, allowed []string) (domain.DimJudgment, error) {
	j.ModelLabel = strings.ToLower(strings.TrimSpace(j.ModelLabel))
	if j.ModelLabel == "" || !containsStr(allowed, j.ModelLabel) {
		return domain.DimJudgment{}, domain.ErrValidation
	}
	if j.Correct {
		j.CorrectLabel = ""
		return j, nil
	}
	j.CorrectLabel = strings.ToLower(strings.TrimSpace(j.CorrectLabel))
	if j.CorrectLabel == "" || !containsStr(allowed, j.CorrectLabel) {
		return domain.DimJudgment{}, domain.ErrValidation
	}
	return j, nil
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *ReviewService) weights() scoring.Weights {
	return scoring.Weights{
		Authenticity: s.cfg.Weights.Authenticity,
		Experience:   s.cfg.Weights.Experience,
		Consistency:  s.cfg.Weights.Consistency,
		Usefulness:   s.cfg.Weights.Usefulness,
	}
}

func breakdownRows(reviewID string, result scoring.Result) []domain.ScoreBreakdownRow {
	out := make([]domain.ScoreBreakdownRow, len(result.Breakdown))
	for i, d := range result.Breakdown {
		out[i] = domain.ScoreBreakdownRow{
			ID:            uuid.NewString(),
			ReviewID:      reviewID,
			Dimension:     d.Dimension,
			FinalLabel:    d.FinalLabel,
			Agreement:     d.Agreement,
			AvgConfidence: d.AvgConfidence,
			DimScore:      d.DimScore,
		}
	}
	return out
}

func validateCreate(in CreateReviewInput, expectedRuns int) error {
	if strings.TrimSpace(in.GameName) == "" || strings.TrimSpace(in.ReviewText) == "" {
		return domain.ErrValidation
	}
	if in.Stars < 1 || in.Stars > 10 {
		return domain.ErrValidation
	}
	if expectedRuns <= 0 {
		expectedRuns = 3
	}
	if len(in.Runs) != expectedRuns {
		return domain.ErrValidation
	}
	if in.LatencyMS < 0 {
		return domain.ErrValidation
	}
	return nil
}
