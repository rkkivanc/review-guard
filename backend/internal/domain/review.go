package domain

import (
	"fmt"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/scoring"
)

// Review is a persisted game review with human correctness + trust stability fields.
type Review struct {
	ID               string                  `json:"id"`
	UserID           string                  `json:"user_id"`
	GameName         string                  `json:"game_name"`
	Stars            int                     `json:"stars"`
	ReviewText       string                  `json:"review_text"`
	TrustScore       float64                 `json:"trust_score"`
	Grade            string                  `json:"grade"`
	NeedsReview      bool                    `json:"needs_review"`
	LatencyMS        int                     `json:"latency_ms"`
	CreatedAt        time.Time               `json:"created_at"`
	Feedback         *ClassificationFeedback `json:"feedback,omitempty"`
	CorrectnessScore *float64                `json:"correctness_score,omitempty"` // 0–100 from human label scores
	CorrectnessLabel string                  `json:"correctness_label,omitempty"` // e.g. "3/4"
}

// DimJudgment is the user's correctness mark for one classification dimension.
type DimJudgment struct {
	Correct      bool   `json:"correct"`
	ModelLabel   string `json:"model_label"`
	CorrectLabel string `json:"correct_label,omitempty"` // required when Correct is false
}

// ClassificationFeedback scores whether each dimension label was right.
// Primary product score. Does not mutate trust statistics; also guides later prompts.
type ClassificationFeedback struct {
	Consistency  DimJudgment `json:"consistency"`
	Authenticity DimJudgment `json:"authenticity"`
	Experience   DimJudgment `json:"experience"`
	Usefulness   DimJudgment `json:"usefulness"`
	Note         string      `json:"note,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

// CorrectnessFromFeedback returns 0–100 (% of four dimensions marked correct) and "n/4".
func CorrectnessFromFeedback(fb ClassificationFeedback) (score float64, label string) {
	ok := 0
	if fb.Consistency.Correct {
		ok++
	}
	if fb.Authenticity.Correct {
		ok++
	}
	if fb.Experience.Correct {
		ok++
	}
	if fb.Usefulness.Correct {
		ok++
	}
	return float64(ok) / 4.0 * 100, fmt.Sprintf("%d/4", ok)
}

// FeedbackHint is a compact row used to improve later Gemma prompts.
type FeedbackHint struct {
	GameName    string          `json:"game_name"`
	Stars       int             `json:"stars"`
	Corrections []DimCorrection `json:"corrections"`
	Note        string          `json:"note,omitempty"`
}

// DimCorrection is one human correction (or confirmation) for a dimension label.
type DimCorrection struct {
	Dimension    string `json:"dimension"`
	ModelLabel   string `json:"model_label"`
	Correct      bool   `json:"correct"`
	CorrectLabel string `json:"correct_label,omitempty"`
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
	FeedbackScoredCount  int                       `json:"feedback_scored_count"`
	AvgCorrectnessScore  float64                   `json:"avg_correctness_score"`
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
