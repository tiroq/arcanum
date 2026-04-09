package outcome

import (
	"time"

	"github.com/google/uuid"
)

// OutcomeStatus classifies the real-world impact of an executed action.
type OutcomeStatus string

const (
	OutcomeSuccess OutcomeStatus = "success"
	OutcomeNeutral OutcomeStatus = "neutral"
	OutcomeFailure OutcomeStatus = "failure"
)

// ActionOutcome captures the verified result of a single executed action.
type ActionOutcome struct {
	ID             uuid.UUID      `json:"id"`
	ActionID       uuid.UUID      `json:"action_id"`
	GoalID         string         `json:"goal_id"`
	ActionType     string         `json:"action_type"`
	TargetType     string         `json:"target_type"`
	TargetID       uuid.UUID      `json:"target_id"`
	OutcomeStatus  OutcomeStatus  `json:"outcome_status"`
	BeforeState    map[string]any `json:"before_state"`
	AfterState     map[string]any `json:"after_state"`
	EffectDetected bool           `json:"effect_detected"`
	Improvement    bool           `json:"improvement"`
	EvaluatedAt    time.Time      `json:"evaluated_at"`

	// Utility fields (Iteration 35) — measure real-world value of the action.
	// All default to 0 when not applicable.
	IncomeValue      float64 `json:"income_value"`
	FamilyValue      float64 `json:"family_value"`
	OwnerReliefValue float64 `json:"owner_relief_value"`
	RiskCost         float64 `json:"risk_cost"`

	// UtilityScore is the net value: IncomeValue + FamilyValue + OwnerReliefValue - RiskCost.
	// Stored for learning signal consumption.
	UtilityScore float64 `json:"utility_score"`
}
