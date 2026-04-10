package financialpressure

import "time"

// Pressure scoring constants.
const (
	// WeightIncomeGap is the fraction of pressure driven by income gap.
	WeightIncomeGap = 0.50
	// WeightBufferRatio is the fraction of pressure driven by buffer depletion.
	WeightBufferRatio = 0.50

	// PressureBoostMax caps the income scoring boost from financial pressure.
	PressureBoostMax = 0.50

	// PressurePathBoostMax caps the decision-graph path boost from pressure.
	PressurePathBoostMax = 0.20

	// UrgencyLowThreshold is the upper bound for "low" urgency.
	UrgencyLowThreshold = 0.30
	// UrgencyMediumThreshold is the upper bound for "medium" urgency.
	UrgencyMediumThreshold = 0.60
	// UrgencyHighThreshold is the upper bound for "high" urgency.
	UrgencyHighThreshold = 0.80
)

// Urgency levels.
const (
	UrgencyLow      = "low"
	UrgencyMedium   = "medium"
	UrgencyHigh     = "high"
	UrgencyCritical = "critical"
)

// FinancialState represents the current financial snapshot used to compute
// pressure. Updated by the user or an upstream system.
type FinancialState struct {
	ID                 string    `json:"id"`
	CurrentIncomeMonth float64   `json:"current_income_month"` // USD earned this month
	TargetIncomeMonth  float64   `json:"target_income_month"`  // USD target for this month
	MonthlyExpenses    float64   `json:"monthly_expenses"`     // USD monthly expense baseline
	CashBuffer         float64   `json:"cash_buffer"`          // USD available cash buffer
	UpdatedAt          time.Time `json:"updated_at"`
}

// FinancialPressure is the computed pressure signal derived from FinancialState.
type FinancialPressure struct {
	PressureScore float64   `json:"pressure_score"` // [0,1]
	UrgencyLevel  string    `json:"urgency_level"`  // low|medium|high|critical
	IncomeGap     float64   `json:"income_gap"`     // target - current (may be <= 0)
	BufferRatio   float64   `json:"buffer_ratio"`   // cash_buffer / monthly_expenses
	ComputedAt    time.Time `json:"computed_at"`
}
