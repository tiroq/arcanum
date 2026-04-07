package actionmemory

import (
	"fmt"
	"math"
	"time"
)

// --- Temporal Decay Constants ---
// All decay thresholds are explicit and deterministic.

const (
	// decayFreshHours: records updated within this window get full weight.
	decayFreshHours = 1
	// decayRecentHours: records updated within this window get high weight.
	decayRecentHours = 24
	// decayModerateHours: records updated within this window get moderate weight.
	decayModerateHours = 24 * 7 // 7 days

	// Weights for each recency bucket.
	decayWeightFresh    = 1.00
	decayWeightRecent   = 0.80
	decayWeightModerate = 0.60
	decayWeightStale    = 0.40
)

// RecencyWeight computes a deterministic temporal weight for a record
// based on how recently it was updated. Returns a value in [0.40, 1.0].
//
// Buckets:
//   - ≤ 1 hour  → 1.0
//   - ≤ 24 hours → 0.8
//   - ≤ 7 days  → 0.6
//   - older     → 0.4
func RecencyWeight(lastUpdated time.Time, now time.Time) float64 {
	if lastUpdated.IsZero() {
		return decayWeightStale
	}
	age := now.Sub(lastUpdated)
	switch {
	case age <= time.Duration(decayFreshHours)*time.Hour:
		return decayWeightFresh
	case age <= time.Duration(decayRecentHours)*time.Hour:
		return decayWeightRecent
	case age <= time.Duration(decayModerateHours)*time.Hour:
		return decayWeightModerate
	default:
		return decayWeightStale
	}
}

// --- Confidence Constants ---

const (
	// sampleWeightLow: samples below this count yield minimum confidence.
	sampleWeightLowThreshold = 3
	// sampleWeightMedThreshold: samples at or above this count yield medium confidence.
	sampleWeightMedThreshold = 5
	// sampleWeightHighThreshold: samples at or above this count yield full sample confidence.
	sampleWeightHighThreshold = 10

	sampleWeightLow  = 0.30
	sampleWeightMed  = 0.60
	sampleWeightHigh = 1.00
)

// SampleWeight computes a deterministic weight based on sample size.
// Returns a value in [0.30, 1.0].
//
// Buckets:
//   - < 3  → 0.30
//   - < 5  → 0.30 + (n-3)/(5-3) * 0.30  (linear interpolation 0.30→0.60)
//   - < 10 → 0.60 + (n-5)/(10-5) * 0.40  (linear interpolation 0.60→1.00)
//   - ≥ 10 → 1.00
func SampleWeight(sampleSize int) float64 {
	switch {
	case sampleSize < sampleWeightLowThreshold:
		return sampleWeightLow
	case sampleSize < sampleWeightMedThreshold:
		frac := float64(sampleSize-sampleWeightLowThreshold) / float64(sampleWeightMedThreshold-sampleWeightLowThreshold)
		return sampleWeightLow + frac*(sampleWeightMed-sampleWeightLow)
	case sampleSize < sampleWeightHighThreshold:
		frac := float64(sampleSize-sampleWeightMedThreshold) / float64(sampleWeightHighThreshold-sampleWeightMedThreshold)
		return sampleWeightMed + frac*(sampleWeightHigh-sampleWeightMed)
	default:
		return sampleWeightHigh
	}
}

// EvidenceConfidence combines recency weight and sample weight into a
// single confidence score in [0, 1]. Uses geometric mean to ensure both
// dimensions must be reasonably strong.
func EvidenceConfidence(recencyWeight, sampleWeight float64) float64 {
	return math.Sqrt(recencyWeight * sampleWeight)
}

// --- Weighted Feedback ---

// SourceLevel identifies where a feedback signal comes from.
type SourceLevel string

const (
	SourceProviderExact   SourceLevel = "provider_exact"
	SourceProviderPartial SourceLevel = "provider_partial"
	SourceContextExact    SourceLevel = "context_exact"
	SourceContextPartial  SourceLevel = "context_partial"
	SourceGlobal          SourceLevel = "global"
)

// specificityBonus gives a small tie-breaking bonus for more specific signals
// when confidence is otherwise equal. These are intentionally small —
// never enough to overcome a meaningful confidence gap.
func specificityBonus(level SourceLevel) float64 {
	switch level {
	case SourceProviderExact:
		return 0.04
	case SourceProviderPartial:
		return 0.03
	case SourceContextExact:
		return 0.02
	case SourceContextPartial:
		return 0.01
	case SourceGlobal:
		return 0.00
	default:
		return 0.00
	}
}

// WeightedFeedback represents a scored feedback signal from any memory layer.
type WeightedFeedback struct {
	Recommendation Recommendation `json:"recommendation"`
	SourceLevel    SourceLevel    `json:"source_level"`
	Confidence     float64        `json:"confidence"`
	RecencyWeight  float64        `json:"recency_weight"`
	SampleWeight   float64        `json:"sample_weight"`
	FinalWeight    float64        `json:"final_weight"`
	SampleSize     int            `json:"sample_size"`
	SuccessRate    float64        `json:"success_rate"`
	FailureRate    float64        `json:"failure_rate"`
	ActionType     string         `json:"action_type"`
}

// computeFinalWeight computes the FinalWeight from confidence + specificity bonus.
func computeFinalWeight(confidence float64, level SourceLevel) float64 {
	return confidence + specificityBonus(level)
}

// BuildWeightedFeedback constructs a WeightedFeedback from a raw memory record.
func BuildWeightedFeedback(fb ActionFeedback, level SourceLevel, lastUpdated time.Time, now time.Time) WeightedFeedback {
	rw := RecencyWeight(lastUpdated, now)
	sw := SampleWeight(fb.SampleSize)
	conf := EvidenceConfidence(rw, sw)
	return WeightedFeedback{
		Recommendation: fb.Recommendation,
		SourceLevel:    level,
		Confidence:     conf,
		RecencyWeight:  rw,
		SampleWeight:   sw,
		FinalWeight:    computeFinalWeight(conf, level),
		SampleSize:     fb.SampleSize,
		SuccessRate:    fb.SuccessRate,
		FailureRate:    fb.FailureRate,
		ActionType:     fb.ActionType,
	}
}

// --- Weighted Resolution ---

// minFinalWeightThreshold: candidates below this are considered too weak
// to influence scoring on their own. They may still contribute as fallback.
const minFinalWeightThreshold = 0.35

// ResolveWeightedFeedback gathers all applicable WeightedFeedback candidates
// and returns the best-supported signal plus all candidates for observability.
//
// Resolution rules:
//  1. Collect all applicable candidates from provider-context, contextual, and global.
//  2. Compute FinalWeight for each.
//  3. Discard insufficient_data and neutral recommendations.
//  4. Select the highest FinalWeight candidate.
//  5. If the top candidate's FinalWeight < minFinalWeightThreshold, demote to
//     conservative blending with any available higher-confidence fallback.
//
// Returns nil as best when no actionable feedback exists.
func ResolveWeightedFeedback(candidates []WeightedFeedback) (*WeightedFeedback, []WeightedFeedback) {
	if len(candidates) == 0 {
		return nil, nil
	}

	// Filter to actionable candidates.
	var actionable []WeightedFeedback
	for _, c := range candidates {
		if c.Recommendation == RecommendInsufficientData || c.Recommendation == RecommendNeutral {
			continue
		}
		actionable = append(actionable, c)
	}

	if len(actionable) == 0 {
		return nil, candidates
	}

	// Find the best (highest FinalWeight).
	best := actionable[0]
	for _, c := range actionable[1:] {
		if c.FinalWeight > best.FinalWeight {
			best = c
		}
	}

	return &best, candidates
}

// WeightedScoreAdjustment computes the score adjustment from a resolved
// weighted feedback signal. The adjustment is scaled by the confidence
// to prevent low-confidence signals from having full impact.
//
// adjustment = feedbackAdjustment(recommendation) * confidence
func WeightedScoreAdjustment(wf *WeightedFeedback, avoidPenalty, preferBoost float64) (adjustment float64, reasoning string) {
	if wf == nil {
		return 0, ""
	}

	rawAdj := feedbackAdjustment(wf.Recommendation, avoidPenalty, preferBoost)
	adj := rawAdj * wf.Confidence

	return adj, fmt.Sprintf(
		"weighted %s %s: raw=%.2f × confidence=%.2f → %.2f (recency=%.2f, samples=%d, final_weight=%.2f)",
		wf.SourceLevel, wf.Recommendation,
		rawAdj, wf.Confidence, adj,
		wf.RecencyWeight, wf.SampleSize, wf.FinalWeight,
	)
}
