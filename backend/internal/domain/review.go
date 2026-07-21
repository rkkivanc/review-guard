package domain

import (
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/scoring"
)

// Review is a persisted game review with server-computed trust fields.
type Review struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	GameName    string    `json:"game_name"`
	Stars       int       `json:"stars"`
	ReviewText  string    `json:"review_text"`
	TrustScore  float64   `json:"trust_score"`
	Grade       string    `json:"grade"`
	NeedsReview bool      `json:"needs_review"`
	LatencyMS   int       `json:"latency_ms"`
	CreatedAt   time.Time `json:"created_at"`
}

// Classification is one model run payload stored for audit/rescore.
type Classification struct {
	ID       string      `json:"id"`
	ReviewID string      `json:"review_id"`
	RunIndex int         `json:"run_index"`
	Payload  scoring.Run `json:"payload"`
}

// ScoreBreakdownRow is one dimension's persisted score line.
type ScoreBreakdownRow struct {
	ID            string  `json:"id"`
	ReviewID      string  `json:"review_id"`
	Dimension     string  `json:"dimension"`
	FinalLabel    string  `json:"final_label"`
	Agreement     float64 `json:"agreement"`
	AvgConfidence float64 `json:"avg_confidence"`
	DimScore      float64 `json:"dim_score"`
}

// ReviewDetail is GET /reviews/{id} payload.
type ReviewDetail struct {
	Review    Review              `json:"review"`
	Runs      []Classification    `json:"runs"`
	Breakdown []ScoreBreakdownRow `json:"breakdown"`
	Composite float64             `json:"composite"`
	Penalty   float64             `json:"penalty"`
}

// ReviewListFilter controls GET /reviews.
type ReviewListFilter struct {
	Game        string
	NeedsReview *bool
	Grade       string
	Limit       int
	Offset      int
}

// Analytics is GET /reviews/analytics.
type Analytics struct {
	TotalReviews         int                       `json:"total_reviews"`
	FlaggedCount         int                       `json:"flagged_count"`
	AvgTrustScore        float64                   `json:"avg_trust_score"`
	GradeCounts          map[string]int            `json:"grade_counts"`
	DimensionDist        map[string]map[string]int `json:"dimension_distribution"`
	Threshold            float64                   `json:"threshold"`
	WouldFlagAtThreshold int                       `json:"would_flag_at_threshold"`
}

// GameAverage is GET /games/{name}/average.
type GameAverage struct {
	GameName    string  `json:"game_name"`
	ReviewCount int     `json:"review_count"`
	RawAvg      float64 `json:"raw_avg"`
	WeightedAvg float64 `json:"weighted_avg"`
	Inflation   float64 `json:"inflation"`
}
