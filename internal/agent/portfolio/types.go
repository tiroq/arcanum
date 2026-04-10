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
	ROIWeight = 0.40

	// StabilityWeight is the fraction of allocation driven by stability.
	StabilityWeight = 0.25

	// PressureWeight is the fraction of allocation driven by financial pressure.
	PressureWeight = 0.20

	// FamilyWeight is the fraction of allocation driven by family-safe constraints.
	FamilyWeight = 0.15
)

// --- Strategy statuses ---

const (
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusAbandoned = "abandoned"
)

// --- Strategy types ---

const (
	TypeConsulting = "consulting"
	TypeAutomation = "automation"
	TypeProduct    = "product"
	TypeContent    = "content"
	TypeService    = "service"
	TypeOther      = "other"
)

// ValidStrategyTypes is the set of acceptable strategy types.
var ValidStrategyTypes = map[string]bool{
	TypeConsulting: true,
	TypeAutomation: true,
	TypeProduct:    true,
	TypeContent:    true,
	TypeService:    true,
	TypeOther:      true,
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
	Volatility          float64   `json:"volatility"`
	TimeToFirstValue    float64   `json:"time_to_first_value"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// StrategyAllocation represents capacity assigned to a strategy.
type StrategyAllocation struct {
	ID             string    `json:"id"`
	StrategyID     string    `json:"strategy_id"`
	AllocatedHours float64   `json:"allocated_hours"`
	ActualHours    float64   `json:"actual_hours"`
	CreatedAt      time.Time `json:"created_at"`
}

// StrategyPerformance tracks real performance of a strategy over time.
type StrategyPerformance struct {
	StrategyID     string    `json:"strategy_id"`
	TotalRevenue   float64   `json:"total_revenue"`
	TotalTimeSpent float64   `json:"total_time_spent"`
	ROI            float64   `json:"roi"`
	ConversionRate float64   `json:"conversion_rate"`
	UpdatedAt      time.Time `json:"updated_at"`
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
	"consulting": TypeConsulting,
	"automation": TypeAutomation,
	"service":    TypeService,
	"content":    TypeContent,
	"product":    TypeProduct,
	"other":      TypeOther,
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
	TypeConsulting: {"propose_income_action", "schedule_work"},
	TypeAutomation: {"propose_income_action", "analyze_opportunity"},
	TypeProduct:    {"propose_income_action", "analyze_opportunity", "schedule_work"},
	TypeContent:    {"propose_income_action"},
	TypeService:    {"propose_income_action", "schedule_work"},
	TypeOther:      {"propose_income_action"},
}
