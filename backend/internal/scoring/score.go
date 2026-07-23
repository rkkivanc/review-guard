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
// Hot path avoids closures and heap maps for the fixed 4 dimensions.
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

	type dimSpec struct {
		name string
		w    float64
	}
	dims := [4]dimSpec{
		{DimAuthenticity, weights.Authenticity},
		{DimExperience, weights.Experience},
		{DimConsistency, weights.Consistency},
		{DimUsefulness, weights.Usefulness},
	}

	breakdown := make([]DimensionScore, 4)
	var weightedSum, weightTotal float64
	hardDisagreement := false
	var finals [4]string

	for i, d := range dims {
		ds := scoreDimension(d.name, runs, i)
		breakdown[i] = ds
		finals[i] = ds.FinalLabel
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

func pickDim(run Run, dimIdx int) LabelResult {
	switch dimIdx {
	case 0:
		return run.Authenticity
	case 1:
		return run.Experience
	case 2:
		return run.Consistency
	default:
		return run.Usefulness
	}
}

func scoreDimension(name string, runs []Run, dimIdx int) DimensionScore {
	n := float64(len(runs))
	// Fixed small label cardinality — stack arrays beat maps on this hot path.
	type bucket struct {
		label   string
		count   int
		confSum float64
		maxConf float64
	}
	var buckets [8]bucket
	nBuckets := 0

	findOrAdd := func(label string) *bucket {
		for i := 0; i < nBuckets; i++ {
			if buckets[i].label == label {
				return &buckets[i]
			}
		}
		if nBuckets >= len(buckets) {
			// Extremely unlikely with enum labels; collapse into last slot.
			b := &buckets[len(buckets)-1]
			b.label = label
			return b
		}
		buckets[nBuckets].label = label
		nBuckets++
		return &buckets[nBuckets-1]
	}

	for _, run := range runs {
		lr := pickDim(run, dimIdx)
		label := normalizeLabel(lr.Label)
		if label == "" {
			label = "unknown"
		}
		conf := clamp01(lr.Confidence)
		b := findOrAdd(label)
		b.count++
		b.confSum += conf
		if conf > b.maxConf {
			b.maxConf = conf
		}
	}

	bestIdx := -1
	bestCount := -1
	bestTieConf := -1.0
	for i := 0; i < nBuckets; i++ {
		b := &buckets[i]
		if b.count > bestCount || (b.count == bestCount && b.maxConf > bestTieConf) {
			bestCount = b.count
			bestIdx = i
			bestTieConf = b.maxConf
		}
	}

	bestLabel := ""
	avgConf := 0.0
	if bestIdx >= 0 {
		bestLabel = buckets[bestIdx].label
		if bestCount > 0 {
			avgConf = buckets[bestIdx].confSum / float64(bestCount)
		}
	}
	agreement := float64(bestCount) / n
	dimScore := 0.5*agreement + 0.5*avgConf

	return DimensionScore{
		Dimension:     name,
		FinalLabel:    bestLabel,
		Agreement:     agreement,
		AvgConfidence: avgConf,
		DimScore:      dimScore,
	}
}

func coherencePenalty(finals [4]string) float64 {
	// indexes: 0=auth, 1=exp, 2=cons, 3=use
	penalty := 0.0
	if finals[1] == "experience_based" && finals[0] == "bot" {
		penalty += 0.15
	}
	if finals[0] == "genuine" && finals[2] == "mismatched" && finals[3] == "empty" {
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
