package objective

import "time"

// --- Utility model weights ---

const (
	// WeightIncome is the fraction of utility driven by verified income progress.
	WeightIncome = 0.30

	// WeightFamily is the fraction of utility driven by family stability / protected time.
	WeightFamily = 0.25

	// WeightOwner is the fraction of utility driven by owner relief and capacity fit.
	WeightOwner = 0.20

	// WeightExecution is the fraction of utility driven by execution readiness.
	WeightExecution = 0.15

	// WeightStrategic is the fraction of utility driven by strategic allocation quality.
	WeightStrategic = 0.10
)

// --- Risk model weights ---

const (
	// WeightFinancialRisk is the fraction of risk driven by financial instability.
	WeightFinancialRisk = 0.30

	// WeightOverloadRisk is the fraction of risk driven by owner overload.
	WeightOverloadRisk = 0.25

	// WeightExecutionRisk is the fraction of risk driven by execution failures.
	WeightExecutionRisk = 0.20

	// WeightConcentrationRisk is the fraction of risk driven by strategy concentration.
	WeightConcentrationRisk = 0.15

	// WeightPricingRisk is the fraction of risk driven by pricing confidence weakness.
	WeightPricingRisk = 0.10
)

// --- Net utility ---

const (
	// RiskPenaltyWeight controls how much risk reduces utility.
	// net_utility = clamp01(utility - risk * RiskPenaltyWeight)
	RiskPenaltyWeight = 0.60

	// ObjectiveBoostMax is the maximum additive boost the objective signal
	// can apply to decision graph path scores.
	ObjectiveBoostMax = 0.08

	// ObjectivePenaltyMax is the maximum penalty the objective signal
	// can apply to decision graph path scores.
	ObjectivePenaltyMax = 0.06

	// NeutralNetUtility is the net utility value that produces zero adjustment.
	// Above this → boost; below this → penalty.
	NeutralNetUtility = 0.50
)

// --- Entities ---

// ObjectiveState holds the positive-side utility input components.
type ObjectiveState struct {
	VerifiedIncomeScore     float64   `json:"verified_income_score"`
	IncomeGrowthScore       float64   `json:"income_growth_score"`
	OwnerReliefScore        float64   `json:"owner_relief_score"`
	FamilyStabilityScore    float64   `json:"family_stability_score"`
	StrategyQualityScore    float64   `json:"strategy_quality_score"`
	ExecutionReadinessScore float64   `json:"execution_readiness_score"`
	ComputedAt              time.Time `json:"computed_at"`
}

// RiskState holds the downside risk input components.
type RiskState struct {
	FinancialInstabilityRisk  float64   `json:"financial_instability_risk"`
	OverloadRisk              float64   `json:"overload_risk"`
	ExecutionRisk             float64   `json:"execution_risk"`
	StrategyConcentrationRisk float64   `json:"strategy_concentration_risk"`
	PricingConfidenceRisk     float64   `json:"pricing_confidence_risk"`
	ComputedAt                time.Time `json:"computed_at"`
}

// ObjectiveSummary is the final combined objective output.
type ObjectiveSummary struct {
	UtilityScore           float64   `json:"utility_score"`
	RiskScore              float64   `json:"risk_score"`
	NetUtility             float64   `json:"net_utility"`
	DominantPositiveFactor string    `json:"dominant_positive_factor"`
	DominantRiskFactor     string    `json:"dominant_risk_factor"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// ObjectiveSignal is the planner-facing signal produced by the objective function.
type ObjectiveSignal struct {
	SignalType  string   `json:"signal_type"`
	Strength    float64  `json:"strength"`
	Explanation string   `json:"explanation"`
	ContextTags []string `json:"context_tags"`
}

// ObjectiveInputs aggregates all raw inputs gathered from subsystems.
type ObjectiveInputs struct {
	// Income
	VerifiedMonthlyIncome float64 `json:"verified_monthly_income"`
	TargetMonthlyIncome   float64 `json:"target_monthly_income"`
	BestOpenOppScore      float64 `json:"best_open_opp_score"`
	OpenOpportunityCount  int     `json:"open_opportunity_count"`

	// Financial pressure
	PressureScore float64 `json:"pressure_score"`
	UrgencyLevel  string  `json:"urgency_level"`

	// Capacity
	OwnerLoadScore      float64 `json:"owner_load_score"`
	AvailableHoursToday float64 `json:"available_hours_today"`
	AvailableHoursWeek  float64 `json:"available_hours_week"`
	MaxDailyWorkHours   float64 `json:"max_daily_work_hours"`

	// Family
	BlockedHoursToday  float64 `json:"blocked_hours_today"`
	MinFamilyTimeHours float64 `json:"min_family_time_hours"`

	// Portfolio
	DiversificationIndex float64 `json:"diversification_index"`
	DominantAllocation   float64 `json:"dominant_allocation"`
	ActiveStrategies     int     `json:"active_strategies"`
	PortfolioROI         float64 `json:"portfolio_roi"`

	// Pricing
	PricingConfidence float64 `json:"pricing_confidence"`
	WinRate           float64 `json:"win_rate"`

	// External actions
	FailedActionCount  int `json:"failed_action_count"`
	PendingActionCount int `json:"pending_action_count"`
	TotalActionCount   int `json:"total_action_count"`
}
