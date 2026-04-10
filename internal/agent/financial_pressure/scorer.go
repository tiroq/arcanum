package financialpressure

import "time"

// ComputePressure derives a deterministic FinancialPressure from the given state.
//
// Formula:
//
//	income_gap    = target_income_month - current_income_month
//	norm_gap      = clamp(income_gap / target_income_month, 0, 1)       [0 if target ≤ 0]
//	buffer_ratio  = cash_buffer / monthly_expenses                       [∞ if expenses = 0]
//	norm_buffer   = clamp(1 - buffer_ratio, 0, 1)                       [0 if expenses = 0]
//	pressure      = clamp(norm_gap * 0.50 + norm_buffer * 0.50, 0, 1)
//
// Edge cases:
//   - target ≤ 0 → income gap component is 0 (no target, no gap pressure)
//   - expenses = 0 → buffer component is 0 (no expenses, no buffer pressure)
//   - current ≥ target → income gap component is 0 (target already met)
//   - cash_buffer ≥ expenses → buffer component is 0 (fully buffered)
func ComputePressure(state FinancialState) FinancialPressure {
	incomeGap := state.TargetIncomeMonth - state.CurrentIncomeMonth

	// Normalise income gap.
	var normGap float64
	if state.TargetIncomeMonth > 0 && incomeGap > 0 {
		normGap = incomeGap / state.TargetIncomeMonth
		normGap = clamp01(normGap)
	}

	// Normalise buffer depletion.
	var bufferRatio float64
	var normBuffer float64
	if state.MonthlyExpenses > 0 {
		bufferRatio = state.CashBuffer / state.MonthlyExpenses
		normBuffer = clamp01(1.0 - bufferRatio)
	}

	pressure := clamp01(normGap*WeightIncomeGap + normBuffer*WeightBufferRatio)

	return FinancialPressure{
		PressureScore: pressure,
		UrgencyLevel:  urgencyFromScore(pressure),
		IncomeGap:     incomeGap,
		BufferRatio:   bufferRatio,
		ComputedAt:    time.Now().UTC(),
	}
}

// ApplyPressureToIncomeScore adjusts a base income score upward based on
// financial pressure.
//
//	final = base * (1 + pressure * PressureBoostMax)
//
// Clamped to [0,1]. PressureBoostMax = 0.50 so max multiplier is 1.5×.
func ApplyPressureToIncomeScore(baseScore, pressure float64) float64 {
	boost := pressure * PressureBoostMax
	return clamp01(baseScore * (1 + boost))
}

// urgencyFromScore maps a pressure score to an urgency level.
func urgencyFromScore(score float64) string {
	switch {
	case score < UrgencyLowThreshold:
		return UrgencyLow
	case score < UrgencyMediumThreshold:
		return UrgencyMedium
	case score < UrgencyHighThreshold:
		return UrgencyHigh
	default:
		return UrgencyCritical
	}
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
