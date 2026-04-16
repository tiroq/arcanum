package actuation

import (
	"context"
	"time"
)

// --- Actuation types ---

// ActuationType enumerates the corrective actions the system can propose.
type ActuationType string

const (
	ActRebalancePortfolio ActuationType = "rebalance_portfolio"
	ActAdjustPricing      ActuationType = "adjust_pricing"
	ActShiftScheduling    ActuationType = "shift_scheduling"
	ActIncreaseDiscovery  ActuationType = "increase_discovery"
	ActTriggerAutomation  ActuationType = "trigger_automation"
	ActReduceLoad         ActuationType = "reduce_load"
	ActStabilizeIncome    ActuationType = "stabilize_income"
)

// ValidActuationTypes is the set of valid actuation types.
var ValidActuationTypes = map[ActuationType]bool{
	ActRebalancePortfolio: true,
	ActAdjustPricing:      true,
	ActShiftScheduling:    true,
	ActIncreaseDiscovery:  true,
	ActTriggerAutomation:  true,
	ActReduceLoad:         true,
	ActStabilizeIncome:    true,
}

// --- Decision status ---

// DecisionStatus enumerates the lifecycle states of an actuation decision.
type DecisionStatus string

const (
	StatusProposed DecisionStatus = "proposed"
	StatusApproved DecisionStatus = "approved"
	StatusRejected DecisionStatus = "rejected"
	StatusExecuted DecisionStatus = "executed"
)

// ValidTransitions defines the allowed state transitions.
var ValidTransitions = map[DecisionStatus][]DecisionStatus{
	StatusProposed: {StatusApproved, StatusRejected},
	StatusApproved: {StatusExecuted},
}

// --- Routing targets ---

// RoutingTarget maps actuation types to the subsystem they route into.
var RoutingTarget = map[ActuationType]string{
	ActRebalancePortfolio: "portfolio",
	ActAdjustPricing:      "pricing",
	ActShiftScheduling:    "scheduling",
	ActIncreaseDiscovery:  "discovery",
	ActTriggerAutomation:  "self_extension",
	ActReduceLoad:         "scheduling",
	ActStabilizeIncome:    "portfolio+pricing",
}

// --- Entities ---

// ActuationDecision represents a single corrective action proposal.
type ActuationDecision struct {
	ID             string         `json:"id"`
	Type           ActuationType  `json:"type"`
	Reason         string         `json:"reason"`
	SignalSource   string         `json:"signal_source"`
	Confidence     float64        `json:"confidence"`
	Priority       float64        `json:"priority"`
	RequiresReview bool           `json:"requires_review"`
	Status         DecisionStatus `json:"status"`
	Target         string         `json:"target"`
	ProposedAt     time.Time      `json:"proposed_at"`
	ResolvedAt     *time.Time     `json:"resolved_at,omitempty"`
}

// ActuationRunResult captures the output of a single actuation run.
type ActuationRunResult struct {
	RunAt      time.Time           `json:"run_at"`
	Decisions  []ActuationDecision `json:"decisions"`
	InputsUsed ActuationInputs     `json:"inputs_used"`
}

// ActuationInputs aggregates all inputs gathered from providers for a single run.
type ActuationInputs struct {
	// Reflection signals (Iteration 49)
	ReflectionSignals []ReflectionSignalInput `json:"reflection_signals"`

	// Objective summary (Iteration 50)
	NetUtility    float64 `json:"net_utility"`
	UtilityScore  float64 `json:"utility_score"`
	RiskScore     float64 `json:"risk_score"`
	FinancialRisk float64 `json:"financial_risk"`
	OverloadRisk  float64 `json:"overload_risk"`
}

// ReflectionSignalInput is a simplified view of a reflection signal for actuation inputs.
type ReflectionSignalInput struct {
	SignalType string  `json:"signal_type"`
	Strength   float64 `json:"strength"`
}

// --- Constants ---

const (
	// LowUtilityThreshold is the net utility below which all action priorities are escalated.
	LowUtilityThreshold = 0.40

	// HighFinancialRiskThreshold is the financial risk above which income stabilization is prioritized.
	HighFinancialRiskThreshold = 0.70

	// HighOverloadRiskThreshold is the overload risk above which load reduction is prioritized.
	HighOverloadRiskThreshold = 0.70

	// PriorityEscalationBoost is the additive boost applied to priority when utility is below threshold.
	PriorityEscalationBoost = 0.20

	// MaxDecisionsPerRun caps the number of decisions produced per actuation run.
	MaxDecisionsPerRun = 10

	// MinSignalStrength is the minimum reflection signal strength to trigger actuation.
	MinSignalStrength = 0.10

	// ReviewRequiredTypes are actuation types that always require human review.
	// (checked programmatically below)
)

// ReviewRequired returns true if the given actuation type always requires human review.
func ReviewRequired(t ActuationType) bool {
	switch t {
	case ActTriggerAutomation, ActAdjustPricing:
		return true
	default:
		return false
	}
}

// --- Provider interfaces (local, avoids import cycles) ---

// ReflectionProvider reads active reflection signals.
type ReflectionProvider interface {
	GetReflectionSignals(ctx context.Context) ([]ReflectionSignalInput, error)
}

// ObjectiveProvider reads the current objective summary and risk state.
type ObjectiveProvider interface {
	GetNetUtility(ctx context.Context) float64
	GetUtilityScore(ctx context.Context) float64
	GetRiskScore(ctx context.Context) float64
	GetFinancialRisk(ctx context.Context) float64
	GetOverloadRisk(ctx context.Context) float64
}

// VectorProvider reads system vector fields for actuation behavior adjustment.
type VectorProvider interface {
	GetHumanReviewStrictness() float64
	GetRiskTolerance() float64
	GetIncomePriority() float64
}
