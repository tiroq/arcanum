package income

import "time"

// Opportunity status values.
const (
	StatusOpen      = "open"
	StatusEvaluated = "evaluated"
	StatusProposed  = "proposed"
	StatusRejected  = "rejected"
	StatusClosed    = "closed"
)

// Proposal status values.
const (
	ProposalStatusPending  = "pending"
	ProposalStatusApproved = "approved"
	ProposalStatusRejected = "rejected"
	ProposalStatusExecuted = "executed"
)

// Risk levels.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Outcome status values.
const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
	OutcomePartial   = "partial"
)

// Outcome source values (Iteration 39).
const (
	OutcomeSourceManual   = "manual"
	OutcomeSourceSystem   = "system"
	OutcomeSourceExternal = "external"
)

// validOutcomeSources is the set of accepted outcome_source values.
var validOutcomeSources = map[string]bool{
	OutcomeSourceManual:   true,
	OutcomeSourceSystem:   true,
	OutcomeSourceExternal: true,
}

// Valid opportunity types.
var validOpportunityTypes = map[string]bool{
	"consulting": true,
	"automation": true,
	"service":    true,
	"content":    true,
	"other":      true,
}

// Scoring constants.
const (
	// WeightValue is the fraction of the score driven by estimated value.
	WeightValue = 0.40
	// WeightConf is the fraction of the score driven by confidence.
	WeightConf = 0.30
	// WeightEffort penalises high-effort opportunities.
	WeightEffort = 0.20
	// MaxOpValue normalises estimated_value to [0,1].
	MaxOpValue = 10_000.0
	// ScoreThreshold is the minimum score required to generate proposals.
	ScoreThreshold = 0.20
	// BigTicketThreshold marks high-value proposals that always require review.
	BigTicketThreshold = 5_000.0
	// IncomeSignalMaxBoost caps the income signal contribution to path scoring.
	IncomeSignalMaxBoost = 0.15
)

// Learning constants (Iteration 39).
const (
	// LearningWeight controls how much historical accuracy influences the adjustment.
	LearningWeight = 0.30
	// LearningMaxConfAdj bounds the confidence adjustment from learning in either direction.
	LearningMaxConfAdj = 0.10
	// MinLearningOutcomes is the minimum number of outcomes required before learning signals are produced.
	MinLearningOutcomes = 3
	// OutcomeFeedbackMaxBoost caps the positive outcome boost applied to income-related paths.
	OutcomeFeedbackMaxBoost = 0.10
	// OutcomeFeedbackMaxPenalty caps the negative outcome penalty applied to income-related paths.
	OutcomeFeedbackMaxPenalty = 0.10
)

// IncomeOpportunity represents a potential income-generating opportunity tracked
// by the agent. Opportunities are the entry point into the income pipeline.
type IncomeOpportunity struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"` // "user" | "system" | "scheduler"
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	OpportunityType string    `json:"opportunity_type"` // consulting|automation|service|content|other
	EstimatedValue  float64   `json:"estimated_value"`  // USD
	EstimatedEffort float64   `json:"estimated_effort"` // [0,1] normalised
	Confidence      float64   `json:"confidence"`       // [0,1]
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// IncomeActionProposal is a concrete action proposal generated from an opportunity.
// It links an income opportunity to an executable agent action type.
type IncomeActionProposal struct {
	ID             string    `json:"id"`
	OpportunityID  string    `json:"opportunity_id"`
	ActionType     string    `json:"action_type"` // maps to an agent action type string
	Title          string    `json:"title"`
	Reason         string    `json:"reason"`
	ExpectedValue  float64   `json:"expected_value"`
	RiskLevel      string    `json:"risk_level"` // low|medium|high
	RequiresReview bool      `json:"requires_review"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// IncomeOutcome records the actual result after an income opportunity was acted upon.
type IncomeOutcome struct {
	ID             string    `json:"id"`
	OpportunityID  string    `json:"opportunity_id"`
	ProposalID     string    `json:"proposal_id,omitempty"` // optional link to proposal
	OutcomeStatus  string    `json:"outcome_status"`        // succeeded|failed|partial
	ActualValue    float64   `json:"actual_value"`          // USD realised
	OwnerTimeSaved float64   `json:"owner_time_saved"`      // hours saved for the owner
	OutcomeSource  string    `json:"outcome_source"`        // manual|system|external (Iteration 39)
	Verified       bool      `json:"verified"`              // ground-truth verified (Iteration 39)
	Notes          string    `json:"notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// LearningRecord holds per-opportunity-type learning stats derived from real outcomes (Iteration 39).
type LearningRecord struct {
	OpportunityType      string    `json:"opportunity_type"`
	TotalOutcomes        int       `json:"total_outcomes"`
	SuccessCount         int       `json:"success_count"`
	TotalAccuracy        float64   `json:"total_accuracy"`        // sum of per-outcome accuracy values
	AvgAccuracy          float64   `json:"avg_accuracy"`          // total_accuracy / total_outcomes
	SuccessRate          float64   `json:"success_rate"`          // success_count / total_outcomes
	ConfidenceAdjustment float64   `json:"confidence_adjustment"` // bounded in [-LearningMaxConfAdj, +LearningMaxConfAdj]
	UpdatedAt            time.Time `json:"updated_at"`
}

// AttributionRecord links an outcome back to its opportunity for accuracy computation (Iteration 39).
type AttributionRecord struct {
	OutcomeID       string  `json:"outcome_id"`
	OpportunityID   string  `json:"opportunity_id"`
	ProposalID      string  `json:"proposal_id,omitempty"`
	OpportunityType string  `json:"opportunity_type"`
	EstimatedValue  float64 `json:"estimated_value"`
	ActualValue     float64 `json:"actual_value"`
	Accuracy        float64 `json:"accuracy"` // actual_value / estimated_value (capped at 2.0)
	OutcomeStatus   string  `json:"outcome_status"`
}

// PerformanceStats is a summary of income attribution performance (Iteration 39).
type PerformanceStats struct {
	TotalOutcomes      int              `json:"total_outcomes"`
	VerifiedOutcomes   int              `json:"verified_outcomes"`
	OverallAccuracy    float64          `json:"overall_accuracy"`
	OverallSuccessRate float64          `json:"overall_success_rate"`
	ByType             []LearningRecord `json:"by_type"`
}

// IncomeSignal is a lightweight snapshot used by the graph adapter.
type IncomeSignal struct {
	BestOpenScore     float64 `json:"best_open_score"`
	OpenOpportunities int     `json:"open_opportunities"`
}
