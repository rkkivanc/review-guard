package scoring

import (
	"math"
	"strings"
)

// Dimension names used across the pipeline.
const (
	DimConsistency  = "consistency"
	DimAuthenticity = "authenticity"
	DimExperience   = "experience"
	DimUsefulness   = "usefulness"
)

// LabelResult is one dimension's output from a single model run.
type LabelResult struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// Run is one full 4-dimension classification from the in-browser model.
type Run struct {
	Consistency  LabelResult `json:"consistency"`
	Authenticity LabelResult `json:"authenticity"`
	Experience   LabelResult `json:"experience"`
	Usefulness   LabelResult `json:"usefulness"`
}

// Weights for the composite trust score (must sum conceptually to 1).
type Weights struct {
	Authenticity float64
	Experience   float64
	Consistency  float64
	Usefulness   float64
}

// DefaultWeights matches the product brief.
func DefaultWeights() Weights {
	return Weights{
		Authenticity: 0.30,
		Experience:   0.30,
		Consistency:  0.25,
		Usefulness:   0.15,
	}
}

// DimensionScore is the per-dimension trust breakdown.
type DimensionScore struct {
	Dimension     string  `json:"dimension"`
	FinalLabel    string  `json:"final_label"`
	Agreement     float64 `json:"agreement"`
	AvgConfidence float64 `json:"avg_confidence"`
	DimScore      float64 `json:"dim_score"`
}

// Result is the full server-side trust evaluation.
type Result struct {
	TrustScore  float64          `json:"trust_score"` // 0–100
	Grade       string           `json:"grade"`
	NeedsReview bool             `json:"needs_review"`
	Composite   float64          `json:"composite"` // 0–1 before scale
	Penalty     float64          `json:"penalty"`
	Breakdown   []DimensionScore `json:"breakdown"`
}

// Score recomputes trust from N classification runs. Client-supplied trust is ignored by design.
func Score(runs []Run, weights Weights, trustThreshold float64) Result {
	if len(runs) == 0 {
		return Result{
			TrustScore:  0,
			Grade:       "F",
			NeedsReview: true,
			Breakdown:   []DimensionScore{},
		}
	}
	if weights == (Weights{}) {
		weights = DefaultWeights()
	}
	if trustThreshold <= 0 {
		trustThreshold = 70
	}

	dims := []struct {
		name string
		pick func(Run) LabelResult
		w    float64
	}{
		{DimAuthenticity, func(r Run) LabelResult { return r.Authenticity }, weights.Authenticity},
		{DimExperience, func(r Run) LabelResult { return r.Experience }, weights.Experience},
		{DimConsistency, func(r Run) LabelResult { return r.Consistency }, weights.Consistency},
		{DimUsefulness, func(r Run) LabelResult { return r.Usefulness }, weights.Usefulness},
	}

	breakdown := make([]DimensionScore, 0, len(dims))
	var weightedSum, weightTotal float64
	hardDisagreement := false

	finals := map[string]string{}

	for _, d := range dims {
		samples := make([]LabelResult, 0, len(runs))
		for _, run := range runs {
			lr := d.pick(run)
			lr.Label = normalizeLabel(lr.Label)
			lr.Confidence = clamp01(lr.Confidence)
			samples = append(samples, lr)
		}
		ds := scoreDimension(d.name, samples)
		breakdown = append(breakdown, ds)
		finals[d.name] = ds.FinalLabel
		if ds.Agreement < 0.5 {
			hardDisagreement = true
		}
		weightedSum += d.w * ds.DimScore
		weightTotal += d.w
	}

	composite := 0.0
	if weightTotal > 0 {
		composite = weightedSum / weightTotal
	}

	penalty := coherencePenalty(finals)
	trust01 := clamp01(composite - penalty)
	trust100 := math.Round(trust01*1000) / 10 // one decimal place on 0–100 scale
	if trust100 > 100 {
		trust100 = 100
	}

	needs := trust100 < trustThreshold || hardDisagreement

	return Result{
		TrustScore:  trust100,
		Grade:       gradeFor(trust100),
		NeedsReview: needs,
		Composite:   composite,
		Penalty:     penalty,
		Breakdown:   breakdown,
	}
}

func scoreDimension(name string, samples []LabelResult) DimensionScore {
	n := float64(len(samples))
	counts := map[string]int{}
	confSum := map[string]float64{}
	maxConf := map[string]float64{}

	for _, s := range samples {
		label := s.Label
		if label == "" {
			label = "unknown"
		}
		counts[label]++
		confSum[label] += s.Confidence
		if s.Confidence > maxConf[label] {
			maxConf[label] = s.Confidence
		}
	}

	// Majority label; ties → highest single-run confidence among tied labels.
	bestLabel := ""
	bestCount := -1
	bestTieConf := -1.0
	for label, c := range counts {
		if c > bestCount || (c == bestCount && maxConf[label] > bestTieConf) {
			bestCount = c
			bestLabel = label
			bestTieConf = maxConf[label]
		}
	}

	agreement := float64(bestCount) / n
	avgConf := 0.0
	if bestCount > 0 {
		avgConf = confSum[bestLabel] / float64(bestCount)
	}
	dimScore := 0.5*agreement + 0.5*avgConf

	return DimensionScore{
		Dimension:     name,
		FinalLabel:    bestLabel,
		Agreement:     agreement,
		AvgConfidence: avgConf,
		DimScore:      dimScore,
	}
}

func coherencePenalty(finals map[string]string) float64 {
	penalty := 0.0
	if finals[DimExperience] == "experience_based" && finals[DimAuthenticity] == "bot" {
		penalty += 0.15
	}
	if finals[DimAuthenticity] == "genuine" &&
		finals[DimConsistency] == "mismatched" &&
		finals[DimUsefulness] == "empty" {
		penalty += 0.10
	}
	return penalty
}

func gradeFor(trust float64) string {
	switch {
	case trust >= 90:
		return "A"
	case trust >= 80:
		return "B"
	case trust >= 70:
		return "C"
	case trust >= 60:
		return "D"
	default:
		return "F"
	}
}

func normalizeLabel(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		// tolerate percentage-style confidences from the model
		if v <= 100 {
			return v / 100
		}
		return 1
	}
	return v
}

// GameAverages computes raw vs trust-weighted star averages.
func GameAverages(stars []int, trusts []float64) (rawAvg, weightedAvg, inflation float64) {
	if len(stars) == 0 || len(stars) != len(trusts) {
		return 0, 0, 0
	}
	var sumStars float64
	var wSum, tSum float64
	for i, s := range stars {
		sumStars += float64(s)
		wSum += float64(s) * trusts[i]
		tSum += trusts[i]
	}
	rawAvg = sumStars / float64(len(stars))
	if tSum > 0 {
		weightedAvg = wSum / tSum
	}
	inflation = rawAvg - weightedAvg
	return rawAvg, weightedAvg, inflation
}
