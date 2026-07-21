package scoring

import (
	"math"
	"testing"
)

func TestScore_PerfectAgreement(t *testing.T) {
	runs := []Run{
		fullRun("aligned", "genuine", "experience_based", "useful", 0.9),
		fullRun("aligned", "genuine", "experience_based", "useful", 0.8),
		fullRun("aligned", "genuine", "experience_based", "useful", 0.85),
	}
	got := Score(runs, DefaultWeights(), 70)

	if got.NeedsReview {
		t.Fatalf("expected needs_review=false, got true (trust=%.1f grade=%s)", got.TrustScore, got.Grade)
	}
	if got.TrustScore < 80 {
		t.Fatalf("expected high trust, got %.1f", got.TrustScore)
	}
	if got.Grade != "A" && got.Grade != "B" {
		t.Fatalf("expected grade A/B, got %s", got.Grade)
	}
	if got.Penalty != 0 {
		t.Fatalf("expected no penalty, got %.2f", got.Penalty)
	}
	for _, d := range got.Breakdown {
		if d.Agreement != 1 {
			t.Fatalf("%s agreement want 1 got %v", d.Dimension, d.Agreement)
		}
		if d.FinalLabel == "" {
			t.Fatalf("%s missing final label", d.Dimension)
		}
	}
}

func TestScore_HardDisagreementFlagsReview(t *testing.T) {
	// authenticity: three different labels → agreement 1/3 < 0.5
	runs := []Run{
		{Authenticity: lr("genuine", 0.9), Consistency: lr("aligned", 0.9), Experience: lr("experience_based", 0.9), Usefulness: lr("useful", 0.9)},
		{Authenticity: lr("suspicious", 0.9), Consistency: lr("aligned", 0.9), Experience: lr("experience_based", 0.9), Usefulness: lr("useful", 0.9)},
		{Authenticity: lr("bot", 0.9), Consistency: lr("aligned", 0.9), Experience: lr("experience_based", 0.9), Usefulness: lr("useful", 0.9)},
	}
	got := Score(runs, DefaultWeights(), 70)
	if !got.NeedsReview {
		t.Fatal("expected needs_review due to hard disagreement")
	}
	var auth DimensionScore
	for _, d := range got.Breakdown {
		if d.Dimension == DimAuthenticity {
			auth = d
		}
	}
	if auth.Agreement != 1.0/3.0 {
		t.Fatalf("authenticity agreement want 1/3 got %v", auth.Agreement)
	}
}

func TestScore_DissentingConfidenceDoesNotInflate(t *testing.T) {
	// majority genuine (2/3); dissenting bot has confidence 1.0 — must not raise avgConfidence
	runs := []Run{
		{Authenticity: lr("genuine", 0.5), Consistency: lr("aligned", 1), Experience: lr("experience_based", 1), Usefulness: lr("useful", 1)},
		{Authenticity: lr("genuine", 0.5), Consistency: lr("aligned", 1), Experience: lr("experience_based", 1), Usefulness: lr("useful", 1)},
		{Authenticity: lr("bot", 1.0), Consistency: lr("aligned", 1), Experience: lr("experience_based", 1), Usefulness: lr("useful", 1)},
	}
	got := Score(runs, DefaultWeights(), 70)
	var auth DimensionScore
	for _, d := range got.Breakdown {
		if d.Dimension == DimAuthenticity {
			auth = d
		}
	}
	if auth.FinalLabel != "genuine" {
		t.Fatalf("final label want genuine got %s", auth.FinalLabel)
	}
	if math.Abs(auth.AvgConfidence-0.5) > 1e-9 {
		t.Fatalf("avgConfidence want 0.5 (agreeing only), got %v", auth.AvgConfidence)
	}
}

func TestScore_CoherencePenaltyBotExperience(t *testing.T) {
	runs := []Run{
		fullRun("aligned", "bot", "experience_based", "useful", 0.95),
		fullRun("aligned", "bot", "experience_based", "useful", 0.95),
		fullRun("aligned", "bot", "experience_based", "useful", 0.95),
	}
	got := Score(runs, DefaultWeights(), 70)
	if math.Abs(got.Penalty-0.15) > 1e-9 {
		t.Fatalf("penalty want 0.15 got %v", got.Penalty)
	}
}

func TestScore_PercentageConfidenceNormalized(t *testing.T) {
	runs := []Run{
		fullRun("aligned", "genuine", "experience_based", "useful", 90), // 90%
		fullRun("aligned", "genuine", "experience_based", "useful", 90),
		fullRun("aligned", "genuine", "experience_based", "useful", 90),
	}
	got := Score(runs, DefaultWeights(), 70)
	for _, d := range got.Breakdown {
		if d.AvgConfidence > 1.0+1e-9 {
			t.Fatalf("%s avgConfidence should be 0–1, got %v", d.Dimension, d.AvgConfidence)
		}
	}
	if got.TrustScore < 70 {
		t.Fatalf("expected solid trust after normalizing percents, got %.1f", got.TrustScore)
	}
}

func TestGameAverages_Inflation(t *testing.T) {
	stars := []int{10, 10, 2}
	trusts := []float64{20, 20, 90} // low-trust 10★ inflate raw avg
	raw, weighted, inflation := GameAverages(stars, trusts)
	if raw <= weighted {
		t.Fatalf("expected raw > weighted when fake high scores present; raw=%v weighted=%v", raw, weighted)
	}
	if inflation <= 0 {
		t.Fatalf("expected positive inflation, got %v", inflation)
	}
}

func TestGradeBoundaries(t *testing.T) {
	cases := []struct {
		trust float64
		want  string
	}{
		{90, "A"}, {89.9, "B"}, {80, "B"}, {70, "C"}, {60, "D"}, {59.9, "F"},
	}
	for _, tc := range cases {
		if g := gradeFor(tc.trust); g != tc.want {
			t.Fatalf("gradeFor(%.1f)=%s want %s", tc.trust, g, tc.want)
		}
	}
}

func fullRun(consistency, authenticity, experience, usefulness string, conf float64) Run {
	return Run{
		Consistency:  lr(consistency, conf),
		Authenticity: lr(authenticity, conf),
		Experience:   lr(experience, conf),
		Usefulness:   lr(usefulness, conf),
	}
}

func lr(label string, conf float64) LabelResult {
	return LabelResult{Label: label, Confidence: conf, Reason: "test"}
}
