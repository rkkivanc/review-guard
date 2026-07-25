package service

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/llm"
	"github.com/masterfabric/review-guard/mf-backend/internal/metrics"
	"github.com/masterfabric/review-guard/mf-backend/internal/scoring"
)

// Create classifies via MLC LLM, scores server-side, then stores the review.
func (s *ReviewService) Create(ctx context.Context, userID string, in CreateReviewInput) (domain.ReviewDetail, error) {
	if err := validateCreateInput(in); err != nil {
		return domain.ReviewDetail{}, err
	}

	var hints []domain.FeedbackHint
	if hintRows, err := s.store.ListGradeFeedback(ctx, userID, 8); err == nil {
		hints = hintRows
	}

	rt := domain.LLMRuntimeConfig{}
	if s.runtime != nil {
		rt = s.runtime.Get()
	}
	started := time.Now()
	classified, err := s.llm.ClassifyThreeTimes(ctx, llm.ClassifyInput{
		GameName:     strings.TrimSpace(in.GameName),
		Stars:        in.Stars,
		ReviewText:   strings.TrimSpace(in.ReviewText),
		Hints:        hints,
		AdapterID:    rt.ActiveAdapter,
		Temperature:  rt.Temperature,
		SystemPrompt: rt.SystemPrompt,
	})
	metrics.ClassifyDuration.Observe(time.Since(started).Seconds())
	if err != nil {
		metrics.ClassifyErrors.Inc()
		return domain.ReviewDetail{}, err
	}

	result := scoring.Score(classified.Runs, s.weights(), s.cfg.TrustThreshold)
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
		LatencyMS:   classified.LatencyMS,
		CreatedAt:   now,
	}

	runs := make([]domain.Classification, len(classified.Runs))
	for i, payload := range classified.Runs {
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
func (s *ReviewService) List(ctx context.Context, userID string, filter domain.ReviewListFilter) (ReviewListResponse, error) {
	if filter.Limit <= 0 || filter.Limit > MaxPageSize {
		if filter.Limit <= 0 {
			filter.Limit = 50
		} else {
			filter.Limit = MaxPageSize
		}
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if filter.Offset > MaxOffset {
		filter.Offset = MaxOffset
	}
	items, total, err := s.store.ListReviews(ctx, userID, filter)
	if err != nil {
		return ReviewListResponse{}, err
	}
	return ReviewListResponse{
		Items:  items,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}, nil
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
func (s *ReviewService) ScoreOnly(ctx context.Context, userID, reviewID string) (ScoreResponse, error) {
	detail, err := s.Get(ctx, userID, reviewID)
	if err != nil {
		return ScoreResponse{}, err
	}
	return ScoreResponse{
		ReviewID:    detail.Review.ID,
		TrustScore:  detail.Review.TrustScore,
		Grade:       detail.Review.Grade,
		NeedsReview: detail.Review.NeedsReview,
		Composite:   detail.Composite,
		Penalty:     detail.Penalty,
		Breakdown:   detail.Breakdown,
	}, nil
}

// Analytics builds dashboard aggregates; threshold previews would-flag count.
func (s *ReviewService) Analytics(ctx context.Context, userID string, threshold float64) (domain.Analytics, error) {
	if threshold <= 0 {
		threshold = s.cfg.TrustThreshold
	}
	return s.store.BuildAnalytics(ctx, userID, threshold)
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
	weights := make([]float64, len(reviews))
	for i, r := range reviews {
		stars[i] = r.Stars
		// Prefer human correctness weight; fall back to trust stability.
		if r.CorrectnessScore != nil {
			weights[i] = *r.CorrectnessScore
		} else {
			weights[i] = r.TrustScore
		}
	}
	raw, weighted, inflation := scoring.GameAverages(stars, weights)
	return domain.GameAverage{
		GameName:    reviews[0].GameName,
		ReviewCount: len(reviews),
		RawAvg:      raw,
		WeightedAvg: weighted,
		Inflation:   inflation,
	}, nil
}

// SubmitFeedback stores per-dimension label correctness and updates correctness_score.
// Does not mutate trust_score / grade (those stay stability stats).
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
	if utf8.RuneCountInString(fb.Note) > MaxFeedbackNote {
		return domain.Review{}, domain.ErrValidation
	}
	fb.CreatedAt = s.now().UTC()
	return s.store.UpsertGradeFeedback(ctx, userID, reviewID, fb)
}

// FeedbackHints returns recent human label corrections for improving the in-browser prompt.
func (s *ReviewService) FeedbackHints(ctx context.Context, userID string, limit int) (FeedbackHintsResponse, error) {
	hints, err := s.store.ListGradeFeedback(ctx, userID, limit)
	if err != nil {
		return FeedbackHintsResponse{}, err
	}
	return FeedbackHintsResponse{Hints: hints}, nil
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

func validateCreateInput(in CreateReviewInput) error {
	game := strings.TrimSpace(in.GameName)
	text := strings.TrimSpace(in.ReviewText)
	if game == "" || text == "" {
		return domain.ErrValidation
	}
	if utf8.RuneCountInString(game) > MaxGameNameLen || utf8.RuneCountInString(text) > MaxReviewTextLen {
		return domain.ErrValidation
	}
	if in.Stars < 1 || in.Stars > 10 {
		return domain.ErrValidation
	}
	return nil
}
