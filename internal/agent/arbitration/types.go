package arbitration

// SignalType enumerates the categories of learning signals that feed into decision scoring.
type SignalType int

const (
	SignalStability          SignalType = iota // Priority 1 (highest)
	SignalCalibration                          // Priority 2
	SignalCausal                               // Priority 3
	SignalComparative                          // Priority 4
	SignalPathLearning                         // Priority 5
	SignalTransitionLearning                   // Priority 6
	SignalExploration                          // Priority 7 (lowest)
)

// String returns the human-readable name for a SignalType.
func (s SignalType) String() string {
	switch s {
	case SignalStability:
		return "stability"
	case SignalCalibration:
		return "calibration"
	case SignalCausal:
		return "causal"
	case SignalComparative:
		return "comparative"
	case SignalPathLearning:
		return "path_learning"
	case SignalTransitionLearning:
		return "transition_learning"
	case SignalExploration:
		return "exploration"
	default:
		return "unknown"
	}
}

// Recommendation is the directional intent of a signal.
type Recommendation int

const (
	RecommendNeutral Recommendation = iota
	RecommendPrefer
	RecommendAvoid
)

// String returns the human-readable name for a Recommendation.
func (r Recommendation) String() string {
	switch r {
	case RecommendNeutral:
		return "neutral"
	case RecommendPrefer:
		return "prefer"
	case RecommendAvoid:
		return "avoid"
	default:
		return "unknown"
	}
}

// Signal represents a single learning signal contributing to path scoring.
type Signal struct {
	Type           SignalType     `json:"type"`
	Recommendation Recommendation `json:"recommendation"`
	Adjustment     float64        `json:"adjustment"`
	Confidence     float64        `json:"confidence"`
	Source         string         `json:"source"`
}

// ArbitrationResult holds the final output of signal arbitration for a single path.
type ArbitrationResult struct {
	FinalAdjustment float64          `json:"final_adjustment"`
	Trace           ArbitrationTrace `json:"trace"`
}

// Thresholds for arbitration rules.
const (
	// ConfidenceSuppressionThreshold: below this, learning signals are suppressed (Rule 2).
	ConfidenceSuppressionThreshold = 0.4

	// NeutralBaseline: score target when conflicts cannot be resolved (Rule 4).
	NeutralBaseline = 0.5

	// NeutralizationStrength: how strongly conflicting signals pull toward baseline.
	NeutralizationStrength = 0.3
)
