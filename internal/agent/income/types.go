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

// IncomeOpportunity represents a potential income-generating opportunity tracked
// by the agent. Opportunities are the entry point into the income pipeline.
type IncomeOpportunity struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`           // "user" | "system" | "scheduler"
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
	ActionType     string    `json:"action_type"`     // maps to an agent action type string
	Title          string    `json:"title"`
	Reason         string    `json:"reason"`
	ExpectedValue  float64   `json:"expected_value"`
	RiskLevel      string    `json:"risk_level"`      // low|medium|high
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
	Notes          string    `json:"notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// IncomeSignal is a lightweight snapshot used by the graph adapter.
type IncomeSignal struct {
	BestOpenScore     float64 `json:"best_open_score"`
	OpenOpportunities int     `json:"open_opportunities"`
}
