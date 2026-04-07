package policy

import (
	"time"

	"github.com/google/uuid"
)

// PolicyParam identifies a tunable parameter.
type PolicyParam string

const (
	ParamFeedbackAvoidPenalty     PolicyParam = "feedbackAvoidPenalty"
	ParamFeedbackPreferBoost      PolicyParam = "feedbackPreferBoost"
	ParamHighBacklogResyncPenalty PolicyParam = "highBacklogResyncPenalty"
	ParamHighRetryBoost           PolicyParam = "highRetryBoost"
	ParamSafetyPreferenceBoost    PolicyParam = "safetyPreferenceBoost"
	ParamNoopBasePenalty          PolicyParam = "noopBasePenalty"
)

// DefaultValues for every tunable parameter.
var DefaultValues = map[PolicyParam]float64{
	ParamFeedbackAvoidPenalty:     0.40,
	ParamFeedbackPreferBoost:      0.25,
	ParamHighBacklogResyncPenalty: 0.30,
	ParamHighRetryBoost:           0.15,
	ParamSafetyPreferenceBoost:    0.05,
	ParamNoopBasePenalty:          0.20,
}

// SafetyBounds constrains every parameter.
type SafetyBounds struct {
	Min      float64
	Max      float64
	MaxDelta float64
}

// Bounds per category.
var (
	PenaltyBounds   = SafetyBounds{Min: 0.0, Max: 1.0, MaxDelta: 0.05}
	BoostBounds     = SafetyBounds{Min: 0.0, Max: 1.0, MaxDelta: 0.05}
	ThresholdBounds = SafetyBounds{Min: 0.1, Max: 0.9, MaxDelta: 0.05}
)

// ParamBounds returns the safety bounds for a given parameter.
func ParamBounds(p PolicyParam) SafetyBounds {
	switch p {
	case ParamFeedbackAvoidPenalty, ParamHighBacklogResyncPenalty, ParamNoopBasePenalty:
		return PenaltyBounds
	case ParamFeedbackPreferBoost, ParamHighRetryBoost, ParamSafetyPreferenceBoost:
		return BoostBounds
	default:
		return ThresholdBounds
	}
}

// MaxChangesPerCycle is the maximum number of policy changes applied in one run.
const MaxChangesPerCycle = 2

// MinConfidence is the minimum confidence for auto-apply.
const MinConfidence = 0.70

// EvaluationCycles is how many scheduler cycles to wait before evaluating a change.
const EvaluationCycles = 5

// PolicyChange is a proposed adjustment to a single parameter.
type PolicyChange struct {
	Parameter  PolicyParam    `json:"parameter"`
	OldValue   float64        `json:"old_value"`
	NewValue   float64        `json:"new_value"`
	Delta      float64        `json:"delta"`
	Reason     string         `json:"reason"`
	Evidence   map[string]any `json:"evidence"`
	Confidence float64        `json:"confidence"`
}

// ChangeRecord is a persisted policy change (from agent_policy_changes).
type ChangeRecord struct {
	ID                  uuid.UUID      `json:"id"`
	Parameter           string         `json:"parameter"`
	OldValue            float64        `json:"old_value"`
	NewValue            float64        `json:"new_value"`
	Reason              string         `json:"reason"`
	Evidence            map[string]any `json:"evidence"`
	Applied             bool           `json:"applied"`
	CreatedAt           time.Time      `json:"created_at"`
	EvaluatedAt         *time.Time     `json:"evaluated_at,omitempty"`
	ImprovementDetected *bool          `json:"improvement_detected,omitempty"`
}

// PolicyState is a snapshot of all active parameter values.
type PolicyState struct {
	Values    map[PolicyParam]float64 `json:"values"`
	UpdatedAt time.Time               `json:"updated_at"`
}
