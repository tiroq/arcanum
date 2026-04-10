package financialtruth

import "time"

// --- Constants ---

const (
	// VerifiedConfidence is the confidence assigned to verified financial facts.
	VerifiedConfidence = 1.0

	// ManualSourceConfidence is the confidence for manually entered events.
	ManualSourceConfidence = 0.90

	// SystemSourceConfidence is the confidence for system-generated events.
	SystemSourceConfidence = 0.80

	// ExternalSourceConfidence is the confidence for external/imported events.
	ExternalSourceConfidence = 0.70

	// MinLinkConfidence is the minimum confidence for heuristic linking.
	// Below this, facts remain unlinked.
	MinLinkConfidence = 0.60

	// ExactMatchConfidence is the confidence for exact identifier-based linking.
	ExactMatchConfidence = 0.95

	// HeuristicMatchConfidence is the confidence for heuristic amount/date matching.
	HeuristicMatchConfidence = 0.70

	// ManualMatchConfidence is the confidence for manually confirmed links.
	ManualMatchConfidence = 1.0
)

// Event direction values.
const (
	DirectionInflow  = "inflow"
	DirectionOutflow = "outflow"
)

// Event type values.
const (
	EventPaymentReceived    = "payment_received"
	EventExpenseRecorded    = "expense_recorded"
	EventInvoicePaid        = "invoice_paid"
	EventSubscriptionCharge = "subscription_charge"
	EventTransferIn         = "transfer_in"
	EventTransferOut        = "transfer_out"
)

// Fact type values.
const (
	FactTypeIncome   = "income"
	FactTypeExpense  = "expense"
	FactTypeRefund   = "refund"
	FactTypeTransfer = "transfer"
)

// Match type values.
const (
	MatchTypeExact     = "exact"
	MatchTypeHeuristic = "heuristic"
	MatchTypeManual    = "manual"
)

// Valid event types.
var validEventTypes = map[string]bool{
	EventPaymentReceived:    true,
	EventExpenseRecorded:    true,
	EventInvoicePaid:        true,
	EventSubscriptionCharge: true,
	EventTransferIn:         true,
	EventTransferOut:        true,
}

// Valid directions.
var validDirections = map[string]bool{
	DirectionInflow:  true,
	DirectionOutflow: true,
}

// Valid fact types.
var validFactTypes = map[string]bool{
	FactTypeIncome:   true,
	FactTypeExpense:  true,
	FactTypeRefund:   true,
	FactTypeTransfer: true,
}

// Valid match types.
var validMatchTypes = map[string]bool{
	MatchTypeExact:     true,
	MatchTypeHeuristic: true,
	MatchTypeManual:    true,
}

// transferEventTypes are events that should not count as income by default.
var transferEventTypes = map[string]bool{
	EventTransferIn:  true,
	EventTransferOut: true,
}

// --- Entities ---

// FinancialEvent represents a raw money-related event from any source.
type FinancialEvent struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`     // "bank" | "manual" | "invoice" | "system"
	EventType   string    `json:"event_type"` // payment_received, expense_recorded, etc.
	Direction   string    `json:"direction"`  // inflow | outflow
	Amount      float64   `json:"amount"`     // always positive
	Currency    string    `json:"currency"`   // ISO 4217, default "USD"
	Description string    `json:"description"`
	ExternalRef string    `json:"external_ref"` // external reference (invoice ID, txn ID, etc.)
	OccurredAt  time.Time `json:"occurred_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// FinancialFact is a normalized verified financial truth record.
type FinancialFact struct {
	ID                  string    `json:"id"`
	FactType            string    `json:"fact_type"`                       // income | expense | refund | transfer
	Amount              float64   `json:"amount"`                          // always positive
	Currency            string    `json:"currency"`                        // ISO 4217
	Verified            bool      `json:"verified"`                        // true = ground truth
	Confidence          float64   `json:"confidence"`                      // [0,1]
	Source              string    `json:"source"`                          // where did this come from
	EventID             string    `json:"event_id"`                        // link to originating event
	LinkedOpportunityID string    `json:"linked_opportunity_id,omitempty"` // nullable
	LinkedOutcomeID     string    `json:"linked_outcome_id,omitempty"`     // nullable
	LinkedProposalID    string    `json:"linked_proposal_id,omitempty"`    // nullable
	FinanciallyVerified bool      `json:"financially_verified"`            // true when linked + verified
	OccurredAt          time.Time `json:"occurred_at"`
	CreatedAt           time.Time `json:"created_at"`
}

// FinancialSummary is a current monthly truth summary.
type FinancialSummary struct {
	Month                        string    `json:"month"` // "2026-04"
	CurrentMonthIncomeVerified   float64   `json:"current_month_income_verified"`
	CurrentMonthExpensesVerified float64   `json:"current_month_expenses_verified"`
	CurrentMonthNetVerified      float64   `json:"current_month_net_verified"`
	PendingUnverifiedInflow      float64   `json:"pending_unverified_inflow"`
	PendingUnverifiedOutflow     float64   `json:"pending_unverified_outflow"`
	TotalFacts                   int       `json:"total_facts"`
	VerifiedFacts                int       `json:"verified_facts"`
	UpdatedAt                    time.Time `json:"updated_at"`
}

// AttributionMatch represents a match between a financial fact and an opportunity/outcome.
type AttributionMatch struct {
	ID              string    `json:"id"`
	FactID          string    `json:"fact_id"`
	OutcomeID       string    `json:"outcome_id,omitempty"`
	OpportunityID   string    `json:"opportunity_id,omitempty"`
	MatchType       string    `json:"match_type"`       // exact | heuristic | manual
	MatchConfidence float64   `json:"match_confidence"` // [0,1]
	CreatedAt       time.Time `json:"created_at"`
}

// LinkRequest is the API input for manually linking a fact to an opportunity/outcome.
type LinkRequest struct {
	FactID        string `json:"fact_id"`
	OutcomeID     string `json:"outcome_id,omitempty"`
	OpportunityID string `json:"opportunity_id,omitempty"`
}

// FinancialTruthSignal carries verified financial truth for upstream consumers.
type FinancialTruthSignal struct {
	VerifiedMonthlyIncome   float64 `json:"verified_monthly_income"`
	VerifiedMonthlyExpenses float64 `json:"verified_monthly_expenses"`
	VerifiedNetIncome       float64 `json:"verified_net_income"`
	HasVerifiedData         bool    `json:"has_verified_data"`
}
