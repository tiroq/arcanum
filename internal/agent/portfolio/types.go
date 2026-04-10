package portfolio

import "time"

// --- Constants ---

const (
	// StrategyPriorityBoostMax is the maximum additive boost applied to
	// decision graph paths belonging to high-ROI strategies.
	StrategyPriorityBoostMax = 0.12

	// StrategyPenaltyMax is the maximum penalty applied to decision graph
	// paths belonging to underperforming strategies.
	StrategyPenaltyMax = 0.10

	// MinAllocationFraction prevents any single strategy from receiving
	// less than 10% of total hours (diversification floor).
	MinAllocationFraction = 0.10

	// MaxAllocationFraction prevents any single strategy from receiving
	// more than 50% of total hours (concentration cap).
	MaxAllocationFraction = 0.50

	// HighROIThreshold marks a strategy as "high ROI" for boosting.
	HighROIThreshold = 50.0

	// LowROIThreshold marks a strategy as "low ROI" for penalising.
	LowROIThreshold = 10.0

	// HighVolatilityThreshold marks a strategy as "volatile".
	HighVolatilityThreshold = 0.70

	// MinSamplesForPerformance gates cold-start; no boost/penalty until
	// at least N hours recorded.
	MinSamplesForPerformance = 5.0

	// ROIWeight is the fraction of allocation driven by expected ROI.
	ROIWeight = 0.35

	// StabilityWeight is the fraction of allocation driven by stability.
	StabilityWeight = 0.25

	// SpeedWeight is the fraction of allocation driven by time-to-first-value.
	SpeedWeight = 0.20

	// PressureWeight is the fraction of allocation driven by financial pressure alignment.
	PressureWeight = 0.20

	// MaxTimeToFirstValue is the normalisation ceiling for time_to_first_value (hours).
	MaxTimeToFirstValue = 200.0

	// DefaultConfidence is the initial confidence for a new strategy.
	DefaultConfidence = 0.5
)

// --- Strategy statuses ---

const (
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusAbandoned = "abandoned"
)

// --- Strategy types ---

const (
	TypeConsulting         = "consulting"
	TypeAutomation         = "automation"
	TypeAutomationServices = "automation_services"
	TypeProduct            = "product"
	TypeContent            = "content"
	TypeCostEfficiency     = "cost_efficiency"
	TypeService            = "service"
	TypeOther              = "other"
)

// ValidStrategyTypes is the set of acceptable strategy types.
var ValidStrategyTypes = map[string]bool{
	TypeConsulting:         true,
	TypeAutomation:         true,
	TypeAutomationServices: true,
	TypeProduct:            true,
	TypeContent:            true,
	TypeCostEfficiency:     true,
	TypeService:            true,
	TypeOther:              true,
}

// ValidStatuses is the set of acceptable strategy statuses.
var ValidStatuses = map[string]bool{
	StatusActive:    true,
	StatusPaused:    true,
	StatusAbandoned: true,
}

// --- Entities ---

// Strategy represents a repeatable revenue pattern.
type Strategy struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	ExpectedReturnPerHr float64   `json:"expected_return_per_hour"`
	StabilityScore      float64   `json:"stability_score"`
	Confidence          float64   `json:"confidence"`
	TimeToFirstValue    float64   `json:"time_to_first_value"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// StrategyAllocation represents capacity assigned to a strategy.
type StrategyAllocation struct {
	ID               string    `json:"id"`
	StrategyID       string    `json:"strategy_id"`
	AllocatedHours   float64   `json:"allocated_hours_week"`
	ActualHours      float64   `json:"actual_hours_week"`
	AllocationWeight float64   `json:"allocation_weight"`
	CreatedAt        time.Time `json:"created_at"`
}

// StrategyPerformance tracks real performance of a strategy over time.
type StrategyPerformance struct {
	StrategyID           string    `json:"strategy_id"`
	OpportunityCount     int       `json:"opportunity_count"`
	QualifiedCount       int       `json:"qualified_count"`
	WonCount             int       `json:"won_count"`
	LostCount            int       `json:"lost_count"`
	TotalVerifiedRevenue float64   `json:"total_verified_revenue"`
	TotalEstimatedHours  float64   `json:"total_estimated_hours"`
	ROIPerHour           float64   `json:"roi_per_hour"`
	ConversionRate       float64   `json:"conversion_rate"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// PortfolioSummary provides a top-level view of the portfolio state.
type PortfolioSummary struct {
	TotalActiveStrategies int       `json:"total_active_strategies"`
	TotalAllocatedHours   float64   `json:"total_allocated_hours"`
	DominantStrategyID    string    `json:"dominant_strategy_id"`
	DiversificationScore  float64   `json:"diversification_score"`
	RebalancedAt          time.Time `json:"rebalanced_at"`
}

// --- Portfolio view ---

// PortfolioEntry combines strategy, allocation, and performance for API.
type PortfolioEntry struct {
	Strategy    Strategy             `json:"strategy"`
	Allocation  *StrategyAllocation  `json:"allocation,omitempty"`
	Performance *StrategyPerformance `json:"performance,omitempty"`
}

// Portfolio is the top-level portfolio view.
type Portfolio struct {
	Entries            []PortfolioEntry `json:"entries"`
	Summary            PortfolioSummary `json:"summary"`
	TotalAllocatedHrs  float64          `json:"total_allocated_hours"`
	TotalActualHrs     float64          `json:"total_actual_hours"`
	TotalRevenue       float64          `json:"total_revenue"`
	PortfolioROI       float64          `json:"portfolio_roi"`
	DiversificationIdx float64          `json:"diversification_index"`
}

// --- Signals ---

// StrategicSignal represents a strategic-level signal for the decision graph.
type StrategicSignal struct {
	StrategyID   string  `json:"strategy_id"`
	StrategyType string  `json:"strategy_type"`
	SignalType   string  `json:"signal_type"`
	Score        float64 `json:"score"`
	Reason       string  `json:"reason"`
}

// RebalanceResult captures the outcome of a portfolio rebalance operation.
type RebalanceResult struct {
	PreviousAllocations []StrategyAllocation `json:"previous_allocations"`
	NewAllocations      []StrategyAllocation `json:"new_allocations"`
	Signals             []StrategicSignal    `json:"signals"`
	Reason              string               `json:"reason"`
}

// --- Opportunity to Strategy mapping ---

// OpportunityStrategyMap maps income opportunity types to strategy types.
var OpportunityStrategyMap = map[string]string{
	// Spec-required mappings.
	"consulting_lead":               TypeConsulting,
	"automation_candidate":          TypeAutomationServices,
	"product_feature_candidate":     TypeProduct,
	"content_opportunity":           TypeContent,
	"cost_saving_candidate":         TypeCostEfficiency,
	"resale_or_repackage_candidate": TypeAutomationServices, // deterministic: automation_services
	// Backward-compatible short-form mappings.
	"consulting":          TypeConsulting,
	"automation":          TypeAutomation,
	"automation_services": TypeAutomationServices,
	"service":             TypeService,
	"content":             TypeContent,
	"product":             TypeProduct,
	"cost_efficiency":     TypeCostEfficiency,
	"other":               TypeOther,
}

// MapOpportunityToStrategy returns the strategy type for an opportunity type.
// Returns TypeOther if no mapping exists.
func MapOpportunityToStrategy(opportunityType string) string {
	if st, ok := OpportunityStrategyMap[opportunityType]; ok {
		return st
	}
	return TypeOther
}

// StrategyActionTypes maps strategy types to agent action types.
var StrategyActionTypes = map[string][]string{
	TypeConsulting:         {"propose_income_action", "schedule_work"},
	TypeAutomation:         {"propose_income_action", "analyze_opportunity"},
	TypeAutomationServices: {"propose_income_action", "analyze_opportunity"},
	TypeProduct:            {"propose_income_action", "analyze_opportunity", "schedule_work"},
	TypeContent:            {"propose_income_action"},
	TypeCostEfficiency:     {"propose_income_action", "analyze_opportunity"},
	TypeService:            {"propose_income_action", "schedule_work"},
	TypeOther:              {"propose_income_action"},
}
