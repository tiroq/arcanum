package vector

import "time"

// SystemVector represents the runtime configuration vector that the owner can
// change live to alter system behavior. It is a single-row table (id='current').
type SystemVector struct {
	IncomePriority        float64   `json:"income_priority"`
	FamilySafetyPriority  float64   `json:"family_safety_priority"`
	InfraPriority         float64   `json:"infra_priority"`
	AutomationPriority    float64   `json:"automation_priority"`
	ExplorationLevel      float64   `json:"exploration_level"`
	RiskTolerance         float64   `json:"risk_tolerance"`
	HumanReviewStrictness float64   `json:"human_review_strictness"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// DefaultVector returns the initial system vector with balanced defaults.
func DefaultVector() SystemVector {
	return SystemVector{
		IncomePriority:        0.70,
		FamilySafetyPriority:  1.00,
		InfraPriority:         0.30,
		AutomationPriority:    0.40,
		ExplorationLevel:      0.30,
		RiskTolerance:         0.30,
		HumanReviewStrictness: 0.80,
	}
}

// Clamp ensures all values are in [0, 1].
func (v *SystemVector) Clamp() {
	v.IncomePriority = clamp01(v.IncomePriority)
	v.FamilySafetyPriority = clamp01(v.FamilySafetyPriority)
	v.InfraPriority = clamp01(v.InfraPriority)
	v.AutomationPriority = clamp01(v.AutomationPriority)
	v.ExplorationLevel = clamp01(v.ExplorationLevel)
	v.RiskTolerance = clamp01(v.RiskTolerance)
	v.HumanReviewStrictness = clamp01(v.HumanReviewStrictness)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// VectorProvider is the interface used by other subsystems to read the current vector.
type VectorProvider interface {
	GetVector() SystemVector
}
