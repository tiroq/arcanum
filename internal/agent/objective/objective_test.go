package objective

import (
	"context"
	"math"
	"testing"
)

// ---- Utility Model Tests ----

func TestIncomeUtilityIncreasesWithVerifiedIncome(t *testing.T) {
	low := ComputeIncomeUtility(1000, 5000, 0.5, 3)
	high := ComputeIncomeUtility(4000, 5000, 0.5, 3)
	if high <= low {
		t.Errorf("higher verified income should increase utility: low=%f high=%f", low, high)
	}
}

func TestFamilyUtilityIncreasesWithStability(t *testing.T) {
	lowStability := ComputeFamilyUtility(0.8, 1.0, 4.0)  // high pressure
	highStability := ComputeFamilyUtility(0.1, 3.0, 4.0) // low pressure
	if highStability <= lowStability {
		t.Errorf("stronger family stability should increase utility: low=%f high=%f", lowStability, highStability)
	}
}

func TestOwnerUtilityIncreasesWithLowerOverload(t *testing.T) {
	overloaded := ComputeOwnerUtility(0.9, 1.0, 8.0)
	relaxed := ComputeOwnerUtility(0.2, 6.0, 8.0)
	if relaxed <= overloaded {
		t.Errorf("lower owner overload should increase utility: overloaded=%f relaxed=%f", overloaded, relaxed)
	}
}

func TestExecutionUtilityIncreasesWithReadiness(t *testing.T) {
	failing := ComputeExecutionUtility(8, 5, 10, 2.0, 8.0)
	ready := ComputeExecutionUtility(0, 0, 10, 7.0, 8.0)
	if ready <= failing {
		t.Errorf("stronger execution readiness should increase utility: failing=%f ready=%f", failing, ready)
	}
}

func TestUtilityAlwaysBounded01(t *testing.T) {
	cases := []struct {
		name   string
		inputs ObjectiveInputs
	}{
		{"all zeros", ObjectiveInputs{}},
		{"max values", ObjectiveInputs{
			VerifiedMonthlyIncome: 100000, TargetMonthlyIncome: 5000,
			BestOpenOppScore: 1.0, OpenOpportunityCount: 100,
			PressureScore: 0, OwnerLoadScore: 0,
			AvailableHoursToday: 20, MaxDailyWorkHours: 8,
			BlockedHoursToday: 10, MinFamilyTimeHours: 4,
			DiversificationIndex: 1.0, PortfolioROI: 200, ActiveStrategies: 10,
			PricingConfidence: 1.0, WinRate: 1.0,
		}},
		{"extreme negative", ObjectiveInputs{
			PressureScore: 1.0, OwnerLoadScore: 1.0,
			FailedActionCount: 100, TotalActionCount: 100,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objState, _, summary := ComputeFromInputs(tc.inputs)
			if summary.UtilityScore < 0 || summary.UtilityScore > 1 {
				t.Errorf("utility out of bounds: %f", summary.UtilityScore)
			}
			if objState.VerifiedIncomeScore < 0 || objState.VerifiedIncomeScore > 1 {
				t.Errorf("income score out of bounds: %f", objState.VerifiedIncomeScore)
			}
			if objState.FamilyStabilityScore < 0 || objState.FamilyStabilityScore > 1 {
				t.Errorf("family score out of bounds: %f", objState.FamilyStabilityScore)
			}
			if objState.OwnerReliefScore < 0 || objState.OwnerReliefScore > 1 {
				t.Errorf("owner score out of bounds: %f", objState.OwnerReliefScore)
			}
			if objState.ExecutionReadinessScore < 0 || objState.ExecutionReadinessScore > 1 {
				t.Errorf("execution score out of bounds: %f", objState.ExecutionReadinessScore)
			}
			if objState.StrategyQualityScore < 0 || objState.StrategyQualityScore > 1 {
				t.Errorf("strategy score out of bounds: %f", objState.StrategyQualityScore)
			}
		})
	}
}

// ---- Risk Model Tests ----

func TestHighPressureIncreasesFinancialRisk(t *testing.T) {
	lowPressure := ComputeFinancialRisk(0.1, 4000, 5000)
	highPressure := ComputeFinancialRisk(0.9, 1000, 5000)
	if highPressure <= lowPressure {
		t.Errorf("high financial pressure should increase risk: low=%f high=%f", lowPressure, highPressure)
	}
}

func TestHighLoadIncreasesOverloadRisk(t *testing.T) {
	lowLoad := ComputeOverloadRisk(0.2, 6.0, 8.0)
	highLoad := ComputeOverloadRisk(0.9, 1.0, 8.0)
	if highLoad <= lowLoad {
		t.Errorf("high owner load should increase overload risk: low=%f high=%f", lowLoad, highLoad)
	}
}

func TestOverConcentrationIncreasesRisk(t *testing.T) {
	diversified := ComputeConcentrationRisk(0.3, 0.8, 4)
	concentrated := ComputeConcentrationRisk(0.9, 0.2, 1)
	if concentrated <= diversified {
		t.Errorf("over-concentration should increase risk: diversified=%f concentrated=%f", diversified, concentrated)
	}
}

func TestLowPricingConfidenceIncreasesRisk(t *testing.T) {
	confident := ComputePricingRisk(0.9, 0.8)
	unconfident := ComputePricingRisk(0.2, 0.3)
	if unconfident <= confident {
		t.Errorf("low pricing confidence should increase risk: confident=%f unconfident=%f", confident, unconfident)
	}
}

func TestRiskAlwaysBounded01(t *testing.T) {
	cases := []struct {
		name   string
		inputs ObjectiveInputs
	}{
		{"all zeros", ObjectiveInputs{}},
		{"max risk", ObjectiveInputs{
			PressureScore: 1.0, OwnerLoadScore: 1.0,
			FailedActionCount: 100, TotalActionCount: 100,
			DominantAllocation: 1.0, DiversificationIndex: 0, ActiveStrategies: 1,
			PricingConfidence: 0, WinRate: 0,
		}},
		{"no risk", ObjectiveInputs{
			VerifiedMonthlyIncome: 10000, TargetMonthlyIncome: 5000,
			PressureScore: 0, OwnerLoadScore: 0,
			AvailableHoursToday: 8, MaxDailyWorkHours: 8,
			DiversificationIndex: 1.0, ActiveStrategies: 5,
			PricingConfidence: 1.0, WinRate: 1.0,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, riskState, summary := ComputeFromInputs(tc.inputs)
			if summary.RiskScore < 0 || summary.RiskScore > 1 {
				t.Errorf("risk out of bounds: %f", summary.RiskScore)
			}
			if riskState.FinancialInstabilityRisk < 0 || riskState.FinancialInstabilityRisk > 1 {
				t.Errorf("financial risk out of bounds: %f", riskState.FinancialInstabilityRisk)
			}
			if riskState.OverloadRisk < 0 || riskState.OverloadRisk > 1 {
				t.Errorf("overload risk out of bounds: %f", riskState.OverloadRisk)
			}
			if riskState.ExecutionRisk < 0 || riskState.ExecutionRisk > 1 {
				t.Errorf("execution risk out of bounds: %f", riskState.ExecutionRisk)
			}
			if riskState.StrategyConcentrationRisk < 0 || riskState.StrategyConcentrationRisk > 1 {
				t.Errorf("concentration risk out of bounds: %f", riskState.StrategyConcentrationRisk)
			}
			if riskState.PricingConfidenceRisk < 0 || riskState.PricingConfidenceRisk > 1 {
				t.Errorf("pricing risk out of bounds: %f", riskState.PricingConfidenceRisk)
			}
		})
	}
}

// ---- Net Objective Tests ----

func TestHigherRiskLowersNetUtility(t *testing.T) {
	lowRisk := ComputeNetUtility(0.7, 0.1)
	highRisk := ComputeNetUtility(0.7, 0.8)
	if highRisk >= lowRisk {
		t.Errorf("higher risk should lower net utility: lowRisk=%f highRisk=%f", lowRisk, highRisk)
	}
}

func TestPositiveUtilityLowRiskImprovesNetScore(t *testing.T) {
	net := ComputeNetUtility(0.8, 0.1)
	if net <= 0.5 {
		t.Errorf("positive utility with low risk should be above neutral: net=%f", net)
	}
}

func TestIdenticalInputGivesIdenticalOutput(t *testing.T) {
	inputs := ObjectiveInputs{
		VerifiedMonthlyIncome: 3000, TargetMonthlyIncome: 5000,
		BestOpenOppScore: 0.6, OpenOpportunityCount: 5,
		PressureScore: 0.4, OwnerLoadScore: 0.3,
		AvailableHoursToday: 5.0, MaxDailyWorkHours: 8.0,
		BlockedHoursToday: 2.0, MinFamilyTimeHours: 4.0,
		DiversificationIndex: 0.7, PortfolioROI: 30.0, ActiveStrategies: 3,
		PricingConfidence: 0.6, WinRate: 0.5,
		FailedActionCount: 2, PendingActionCount: 3, TotalActionCount: 20,
	}
	_, _, s1 := ComputeFromInputs(inputs)
	_, _, s2 := ComputeFromInputs(inputs)
	if s1.UtilityScore != s2.UtilityScore || s1.RiskScore != s2.RiskScore || s1.NetUtility != s2.NetUtility {
		t.Errorf("identical inputs must give identical output: s1=%+v s2=%+v", s1, s2)
	}
}

func TestFailOpenWhenNoData(t *testing.T) {
	inputs := ObjectiveInputs{} // all zeros
	objState, riskState, summary := ComputeFromInputs(inputs)

	// Must not panic, must produce bounded values.
	if summary.UtilityScore < 0 || summary.UtilityScore > 1 {
		t.Errorf("utility out of bounds with no data: %f", summary.UtilityScore)
	}
	if summary.RiskScore < 0 || summary.RiskScore > 1 {
		t.Errorf("risk out of bounds with no data: %f", summary.RiskScore)
	}
	if summary.NetUtility < 0 || summary.NetUtility > 1 {
		t.Errorf("net utility out of bounds with no data: %f", summary.NetUtility)
	}
	if summary.DominantPositiveFactor == "" {
		t.Error("dominant positive factor should not be empty")
	}
	if summary.DominantRiskFactor == "" {
		t.Error("dominant risk factor should not be empty")
	}
	_ = objState
	_ = riskState
}

// ---- Objective Signal Tests ----

func TestObjectiveSignalProducesBoundedBoost(t *testing.T) {
	sig := ComputeObjectiveSignal(0.8, "income", "financial")
	if sig.SignalType != "objective_boost" {
		t.Errorf("expected boost signal, got %s", sig.SignalType)
	}
	if sig.Strength < 0 || sig.Strength > ObjectiveBoostMax {
		t.Errorf("boost out of bounds: %f (max %f)", sig.Strength, ObjectiveBoostMax)
	}
}

func TestObjectiveSignalProducesBoundedPenalty(t *testing.T) {
	sig := ComputeObjectiveSignal(0.2, "income", "financial")
	if sig.SignalType != "objective_penalty" {
		t.Errorf("expected penalty signal, got %s", sig.SignalType)
	}
	if sig.Strength > 0 || sig.Strength < -ObjectivePenaltyMax {
		t.Errorf("penalty out of bounds: %f (max -%f)", sig.Strength, ObjectivePenaltyMax)
	}
}

func TestObjectiveSignalZeroAtNeutral(t *testing.T) {
	sig := ComputeObjectiveSignal(NeutralNetUtility, "income", "financial")
	if sig.Strength != 0 {
		t.Errorf("at neutral, strength should be 0, got %f", sig.Strength)
	}
}

func TestNoProviderNoEffect(t *testing.T) {
	var adapter *GraphAdapter
	sig := adapter.GetObjectiveSignal(context.Background())
	if sig.Strength != 0 || sig.SignalType != "" {
		t.Errorf("nil adapter should return zero signal: %+v", sig)
	}
}

func TestEngineFailOpenNoProviders(t *testing.T) {
	// Engine with no providers should produce valid but zero-ish results.
	engine := &Engine{
		objStore:     nil,
		riskStore:    nil,
		summaryStore: nil,
	}
	inputs := engine.GatherInputs(context.Background())
	if inputs.VerifiedMonthlyIncome != 0 {
		t.Errorf("expected zero income with no providers: %f", inputs.VerifiedMonthlyIncome)
	}
	if inputs.PressureScore != 0 {
		t.Errorf("expected zero pressure with no providers: %f", inputs.PressureScore)
	}
}

// ---- Dominant Factor Tests ----

func TestDominantPositiveFactorSelection(t *testing.T) {
	// Income at 0.9, others low.
	factor := DominantPositiveFactor(0.9, 0.1, 0.1, 0.1, 0.1)
	if factor != "income" {
		t.Errorf("expected income as dominant, got %s", factor)
	}

	// Family at 0.9, others low.
	factor = DominantPositiveFactor(0.1, 0.9, 0.1, 0.1, 0.1)
	if factor != "family" {
		t.Errorf("expected family as dominant, got %s", factor)
	}
}

func TestDominantRiskFactorSelection(t *testing.T) {
	// Financial at 0.9, others low.
	factor := DominantRiskFactor(0.9, 0.1, 0.1, 0.1, 0.1)
	if factor != "financial" {
		t.Errorf("expected financial as dominant, got %s", factor)
	}

	// Overload at 0.9, others low.
	factor = DominantRiskFactor(0.1, 0.9, 0.1, 0.1, 0.1)
	if factor != "overload" {
		t.Errorf("expected overload as dominant, got %s", factor)
	}
}

// ---- Full Pipeline Integration Tests ----

func TestHighIncomeHighStabilityScenario(t *testing.T) {
	inputs := ObjectiveInputs{
		VerifiedMonthlyIncome: 8000, TargetMonthlyIncome: 5000,
		BestOpenOppScore: 0.8, OpenOpportunityCount: 5,
		PressureScore: 0.1, UrgencyLevel: "low",
		OwnerLoadScore: 0.2, AvailableHoursToday: 6, AvailableHoursWeek: 30, MaxDailyWorkHours: 8,
		BlockedHoursToday: 3, MinFamilyTimeHours: 4,
		DiversificationIndex: 0.8, PortfolioROI: 60, ActiveStrategies: 4,
		PricingConfidence: 0.8, WinRate: 0.7,
		DominantAllocation: 0.3,
	}
	_, _, summary := ComputeFromInputs(inputs)
	if summary.UtilityScore < 0.6 {
		t.Errorf("high income / low risk should have high utility: %f", summary.UtilityScore)
	}
	if summary.RiskScore > 0.3 {
		t.Errorf("low risk scenario should have low risk: %f", summary.RiskScore)
	}
	if summary.NetUtility < 0.5 {
		t.Errorf("high income / low risk should have high net utility: %f", summary.NetUtility)
	}
}

func TestHighPressureHighRiskScenario(t *testing.T) {
	inputs := ObjectiveInputs{
		VerifiedMonthlyIncome: 500, TargetMonthlyIncome: 5000,
		BestOpenOppScore: 0.2, OpenOpportunityCount: 1,
		PressureScore: 0.9, UrgencyLevel: "critical",
		OwnerLoadScore: 0.8, AvailableHoursToday: 1, MaxDailyWorkHours: 8,
		BlockedHoursToday: 1, MinFamilyTimeHours: 4,
		DiversificationIndex: 0.2, PortfolioROI: 5, ActiveStrategies: 1,
		PricingConfidence: 0.2, WinRate: 0.1,
		DominantAllocation: 0.9,
		FailedActionCount:  5, TotalActionCount: 10,
	}
	_, _, summary := ComputeFromInputs(inputs)
	if summary.RiskScore < 0.5 {
		t.Errorf("high pressure scenario should have high risk: %f", summary.RiskScore)
	}
	if summary.NetUtility > 0.4 {
		t.Errorf("high risk should suppress net utility: %f", summary.NetUtility)
	}
}

func TestOverloadedOwnerScenario(t *testing.T) {
	inputs := ObjectiveInputs{
		VerifiedMonthlyIncome: 3000, TargetMonthlyIncome: 5000,
		OwnerLoadScore: 0.95, AvailableHoursToday: 0.5, MaxDailyWorkHours: 8,
		PressureScore: 0.5,
	}
	_, riskState, _ := ComputeFromInputs(inputs)
	if riskState.OverloadRisk < 0.7 {
		t.Errorf("overloaded owner should have high overload risk: %f", riskState.OverloadRisk)
	}
}

func TestConcentrationRiskScenario(t *testing.T) {
	inputs := ObjectiveInputs{
		DominantAllocation:   0.95,
		DiversificationIndex: 0.1,
		ActiveStrategies:     1,
	}
	_, riskState, _ := ComputeFromInputs(inputs)
	if riskState.StrategyConcentrationRisk < 0.6 {
		t.Errorf("concentrated portfolio should have high concentration risk: %f", riskState.StrategyConcentrationRisk)
	}
}

// ---- Weight Consistency Tests ----

func TestUtilityWeightsSumTo1(t *testing.T) {
	sum := WeightIncome + WeightFamily + WeightOwner + WeightExecution + WeightStrategic
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("utility weights must sum to 1.0, got %f", sum)
	}
}

func TestRiskWeightsSumTo1(t *testing.T) {
	sum := WeightFinancialRisk + WeightOverloadRisk + WeightExecutionRisk + WeightConcentrationRisk + WeightPricingRisk
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("risk weights must sum to 1.0, got %f", sum)
	}
}

// ---- Clamp Tests ----

func TestClamp01(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{-0.5, 0}, {0, 0}, {0.5, 0.5}, {1.0, 1.0}, {1.5, 1.0},
	}
	for _, tc := range cases {
		got := clamp01(tc.in)
		if got != tc.want {
			t.Errorf("clamp01(%f) = %f, want %f", tc.in, got, tc.want)
		}
	}
}

// ---- Edge Cases ----

func TestZeroDivisionSafety(t *testing.T) {
	// All division-susceptible cases.
	_ = ComputeIncomeUtility(100, 0, 0, 0)     // targetIncome=0
	_ = ComputeFamilyUtility(0, 0, 0)          // minFamilyHours=0
	_ = ComputeOwnerUtility(0, 0, 0)           // maxDailyHours=0
	_ = ComputeExecutionUtility(0, 0, 0, 0, 0) // totalActions=0, maxDaily=0
	_ = ComputeStrategicUtility(0, 0, 0)       // activeStrategies=0
	_ = ComputeFinancialRisk(0, 0, 0)          // targetIncome=0
	_ = ComputeOverloadRisk(0, 0, 0)           // maxDailyHours=0
	_ = ComputeExecutionRisk(0, 0)             // totalActions=0
	_ = ComputeConcentrationRisk(0, 0, 0)      // activeStrategies=0
	// If we get here without panicking, the test passes.
}

func TestNetUtilityBounds(t *testing.T) {
	// Extreme utility with extreme risk.
	net := ComputeNetUtility(1.0, 1.0)
	if net < 0 || net > 1 {
		t.Errorf("net utility out of bounds: %f", net)
	}
	// Zero utility with max risk.
	net = ComputeNetUtility(0, 1.0)
	if net < 0 || net > 1 {
		t.Errorf("net utility out of bounds: %f", net)
	}
}
