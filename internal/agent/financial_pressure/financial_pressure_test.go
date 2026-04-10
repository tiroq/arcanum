package financialpressure

import (
	"context"
	"testing"
)

// --- Pressure calculation tests ---

func TestComputePressure_HighGapLowBuffer(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 1000,
		TargetIncomeMonth:  5000,
		MonthlyExpenses:    4000,
		CashBuffer:         1000,
	}
	p := ComputePressure(state)
	// income_gap = 5000 - 1000 = 4000
	// norm_gap = 4000/5000 = 0.80
	// buffer_ratio = 1000/4000 = 0.25
	// norm_buffer = 1 - 0.25 = 0.75
	// pressure = 0.80*0.50 + 0.75*0.50 = 0.40 + 0.375 = 0.775
	if p.PressureScore < 0.77 || p.PressureScore > 0.78 {
		t.Errorf("expected ~0.775, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyHigh {
		t.Errorf("expected urgency high, got %s", p.UrgencyLevel)
	}
	if p.IncomeGap != 4000 {
		t.Errorf("expected income_gap 4000, got %f", p.IncomeGap)
	}
}

func TestComputePressure_NoGapHighBuffer(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 6000,
		TargetIncomeMonth:  5000,
		MonthlyExpenses:    3000,
		CashBuffer:         12000,
	}
	p := ComputePressure(state)
	// income_gap = 5000 - 6000 = -1000 (negative, norm_gap = 0)
	// buffer_ratio = 12000/3000 = 4.0
	// norm_buffer = clamp(1 - 4.0, 0, 1) = 0
	// pressure = 0 + 0 = 0
	if p.PressureScore != 0 {
		t.Errorf("expected 0, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyLow {
		t.Errorf("expected urgency low, got %s", p.UrgencyLevel)
	}
}

func TestComputePressure_ZeroIncome(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 0,
		TargetIncomeMonth:  5000,
		MonthlyExpenses:    3000,
		CashBuffer:         500,
	}
	p := ComputePressure(state)
	// income_gap = 5000
	// norm_gap = 5000/5000 = 1.0
	// buffer_ratio = 500/3000 ≈ 0.1667
	// norm_buffer = 1 - 0.1667 ≈ 0.8333
	// pressure = 1.0*0.50 + 0.8333*0.50 = 0.50 + 0.4167 = 0.9167
	if p.PressureScore < 0.91 || p.PressureScore > 0.92 {
		t.Errorf("expected ~0.917, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyCritical {
		t.Errorf("expected urgency critical, got %s", p.UrgencyLevel)
	}
}

func TestComputePressure_ZeroTarget(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 1000,
		TargetIncomeMonth:  0,
		MonthlyExpenses:    3000,
		CashBuffer:         3000,
	}
	p := ComputePressure(state)
	// target ≤ 0 → norm_gap = 0
	// buffer_ratio = 3000/3000 = 1.0
	// norm_buffer = 1 - 1.0 = 0
	// pressure = 0
	if p.PressureScore != 0 {
		t.Errorf("expected 0, got %f", p.PressureScore)
	}
}

func TestComputePressure_ZeroExpenses(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 1000,
		TargetIncomeMonth:  5000,
		MonthlyExpenses:    0,
		CashBuffer:         10000,
	}
	p := ComputePressure(state)
	// expenses = 0 → norm_buffer = 0
	// income_gap = 4000, norm_gap = 4000/5000 = 0.80
	// pressure = 0.80*0.50 + 0 = 0.40
	if p.PressureScore < 0.39 || p.PressureScore > 0.41 {
		t.Errorf("expected ~0.40, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyMedium {
		t.Errorf("expected urgency medium, got %s", p.UrgencyLevel)
	}
}

func TestComputePressure_ZeroEverything(t *testing.T) {
	state := FinancialState{}
	p := ComputePressure(state)
	// all zeros → pressure = 0
	if p.PressureScore != 0 {
		t.Errorf("expected 0, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyLow {
		t.Errorf("expected urgency low, got %s", p.UrgencyLevel)
	}
}

func TestComputePressure_FullGapNoBuffer(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 0,
		TargetIncomeMonth:  10000,
		MonthlyExpenses:    5000,
		CashBuffer:         0,
	}
	p := ComputePressure(state)
	// norm_gap = 10000/10000 = 1.0
	// buffer_ratio = 0/5000 = 0
	// norm_buffer = 1 - 0 = 1.0
	// pressure = 1.0*0.50 + 1.0*0.50 = 1.0
	if p.PressureScore != 1.0 {
		t.Errorf("expected 1.0, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyCritical {
		t.Errorf("expected urgency critical, got %s", p.UrgencyLevel)
	}
}

func TestComputePressure_PartialGapPartialBuffer(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 3000,
		TargetIncomeMonth:  6000,
		MonthlyExpenses:    4000,
		CashBuffer:         2000,
	}
	p := ComputePressure(state)
	// income_gap = 3000, norm_gap = 3000/6000 = 0.50
	// buffer_ratio = 2000/4000 = 0.50
	// norm_buffer = 1 - 0.50 = 0.50
	// pressure = 0.50*0.50 + 0.50*0.50 = 0.25 + 0.25 = 0.50
	if p.PressureScore < 0.49 || p.PressureScore > 0.51 {
		t.Errorf("expected ~0.50, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyMedium {
		t.Errorf("expected urgency medium, got %s", p.UrgencyLevel)
	}
}

// --- Urgency level tests ---

func TestUrgencyFromScore_Low(t *testing.T) {
	if urgencyFromScore(0.0) != UrgencyLow {
		t.Error("expected low for 0.0")
	}
	if urgencyFromScore(0.29) != UrgencyLow {
		t.Error("expected low for 0.29")
	}
}

func TestUrgencyFromScore_Medium(t *testing.T) {
	if urgencyFromScore(0.30) != UrgencyMedium {
		t.Error("expected medium for 0.30")
	}
	if urgencyFromScore(0.59) != UrgencyMedium {
		t.Error("expected medium for 0.59")
	}
}

func TestUrgencyFromScore_High(t *testing.T) {
	if urgencyFromScore(0.60) != UrgencyHigh {
		t.Error("expected high for 0.60")
	}
	if urgencyFromScore(0.79) != UrgencyHigh {
		t.Error("expected high for 0.79")
	}
}

func TestUrgencyFromScore_Critical(t *testing.T) {
	if urgencyFromScore(0.80) != UrgencyCritical {
		t.Error("expected critical for 0.80")
	}
	if urgencyFromScore(1.0) != UrgencyCritical {
		t.Error("expected critical for 1.0")
	}
}

// --- Income scoring integration tests ---

func TestApplyPressureToIncomeScore_NoPressure(t *testing.T) {
	result := ApplyPressureToIncomeScore(0.50, 0.0)
	// final = 0.50 * (1 + 0 * 0.50) = 0.50
	if result != 0.50 {
		t.Errorf("expected 0.50, got %f", result)
	}
}

func TestApplyPressureToIncomeScore_MaxPressure(t *testing.T) {
	result := ApplyPressureToIncomeScore(0.60, 1.0)
	// final = 0.60 * (1 + 1.0 * 0.50) = 0.60 * 1.50 = 0.90
	if result < 0.89 || result > 0.91 {
		t.Errorf("expected ~0.90, got %f", result)
	}
}

func TestApplyPressureToIncomeScore_ClampedToOne(t *testing.T) {
	result := ApplyPressureToIncomeScore(0.80, 1.0)
	// final = 0.80 * 1.50 = 1.20 → clamped to 1.0
	if result != 1.0 {
		t.Errorf("expected 1.0, got %f", result)
	}
}

func TestApplyPressureToIncomeScore_ZeroBase(t *testing.T) {
	result := ApplyPressureToIncomeScore(0.0, 1.0)
	// final = 0.0 * 1.50 = 0.0
	if result != 0.0 {
		t.Errorf("expected 0.0, got %f", result)
	}
}

func TestApplyPressureToIncomeScore_ModeratePressure(t *testing.T) {
	result := ApplyPressureToIncomeScore(0.50, 0.50)
	// final = 0.50 * (1 + 0.50 * 0.50) = 0.50 * 1.25 = 0.625
	if result < 0.624 || result > 0.626 {
		t.Errorf("expected ~0.625, got %f", result)
	}
}

// --- Adapter nil-safety tests ---

func TestGraphAdapter_NilSafe(t *testing.T) {
	var adapter *GraphAdapter
	pressure, urgency := adapter.GetPressure(context.Background())
	if pressure != 0 {
		t.Errorf("expected 0 pressure from nil adapter, got %f", pressure)
	}
	if urgency != UrgencyLow {
		t.Errorf("expected low urgency from nil adapter, got %s", urgency)
	}
}

func TestGraphAdapter_NilStore(t *testing.T) {
	adapter := &GraphAdapter{}
	pressure, urgency := adapter.GetPressure(context.Background())
	if pressure != 0 {
		t.Errorf("expected 0 pressure from nil store, got %f", pressure)
	}
	if urgency != UrgencyLow {
		t.Errorf("expected low urgency from nil store, got %s", urgency)
	}
}

// --- IsIncomeRelated tests ---

func TestIsIncomeRelated_Known(t *testing.T) {
	adapter := &GraphAdapter{}
	if !adapter.IsIncomeRelated("propose_income_action") {
		t.Error("expected propose_income_action to be income-related")
	}
	if !adapter.IsIncomeRelated("analyze_opportunity") {
		t.Error("expected analyze_opportunity to be income-related")
	}
	if !adapter.IsIncomeRelated("schedule_work") {
		t.Error("expected schedule_work to be income-related")
	}
}

func TestIsIncomeRelated_Unknown(t *testing.T) {
	adapter := &GraphAdapter{}
	if adapter.IsIncomeRelated("summarize_state") {
		t.Error("expected summarize_state to not be income-related")
	}
	if adapter.IsIncomeRelated("unknown") {
		t.Error("expected unknown to not be income-related")
	}
}

// --- Clamp tests ---

func TestClamp01_InRange(t *testing.T) {
	if clamp01(0.5) != 0.5 {
		t.Error("expected 0.5")
	}
}

func TestClamp01_Below(t *testing.T) {
	if clamp01(-0.5) != 0 {
		t.Error("expected 0 for negative")
	}
}

func TestClamp01_Above(t *testing.T) {
	if clamp01(1.5) != 1 {
		t.Error("expected 1 for above-1")
	}
}

// --- Boundary condition tests ---

func TestComputePressure_BufferExactlyEqualsExpenses(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 3000,
		TargetIncomeMonth:  3000,
		MonthlyExpenses:    4000,
		CashBuffer:         4000,
	}
	p := ComputePressure(state)
	// income met → norm_gap = 0
	// buffer_ratio = 1.0, norm_buffer = 0
	// pressure = 0
	if p.PressureScore != 0 {
		t.Errorf("expected 0, got %f", p.PressureScore)
	}
}

func TestComputePressure_VerySmallGap(t *testing.T) {
	state := FinancialState{
		CurrentIncomeMonth: 4999,
		TargetIncomeMonth:  5000,
		MonthlyExpenses:    3000,
		CashBuffer:         9000,
	}
	p := ComputePressure(state)
	// income_gap = 1, norm_gap = 1/5000 = 0.0002
	// buffer_ratio = 9000/3000 = 3.0, norm_buffer = 0
	// pressure ≈ 0.0001
	if p.PressureScore < 0 || p.PressureScore > 0.01 {
		t.Errorf("expected ~0, got %f", p.PressureScore)
	}
	if p.UrgencyLevel != UrgencyLow {
		t.Errorf("expected urgency low, got %s", p.UrgencyLevel)
	}
}

func TestComputePressure_NegativeGapIgnored(t *testing.T) {
	// Over-earning scenario
	state := FinancialState{
		CurrentIncomeMonth: 10000,
		TargetIncomeMonth:  5000,
		MonthlyExpenses:    3000,
		CashBuffer:         6000,
	}
	p := ComputePressure(state)
	// income_gap = -5000 → norm_gap = 0
	// buffer_ratio = 2.0, norm_buffer = 0
	// pressure = 0
	if p.PressureScore != 0 {
		t.Errorf("expected 0 for over-earning, got %f", p.PressureScore)
	}
	if p.IncomeGap != -5000 {
		t.Errorf("expected income_gap -5000, got %f", p.IncomeGap)
	}
}

// --- Path boost safety tests ---

func TestPathBoost_BoundedAtMax(t *testing.T) {
	// Maximum pressure of 1.0 × PressurePathBoostMax = 0.20
	maxBoost := 1.0 * PressurePathBoostMax
	if maxBoost > 0.20 || maxBoost < 0.20 {
		t.Errorf("expected max path boost 0.20, got %f", maxBoost)
	}
}

func TestPathBoost_CannotExceedOne(t *testing.T) {
	// Even with max boost, a score of 0.95 should clamp to 1.0
	score := 0.95
	boost := 1.0 * PressurePathBoostMax // 0.20
	result := clamp01(score + boost)    // 1.15 → 1.0
	if result != 1.0 {
		t.Errorf("expected 1.0, got %f", result)
	}
}

// --- Constants validation ---

func TestConstants_Ranges(t *testing.T) {
	if WeightIncomeGap+WeightBufferRatio != 1.0 {
		t.Errorf("weights must sum to 1.0, got %f", WeightIncomeGap+WeightBufferRatio)
	}
	if PressureBoostMax <= 0 || PressureBoostMax > 1 {
		t.Errorf("PressureBoostMax must be in (0,1], got %f", PressureBoostMax)
	}
	if PressurePathBoostMax <= 0 || PressurePathBoostMax > 1 {
		t.Errorf("PressurePathBoostMax must be in (0,1], got %f", PressurePathBoostMax)
	}
}
