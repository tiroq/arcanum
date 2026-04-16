package objective

import (
	"context"
	"math"
	"testing"
)

// --- Execution Feedback Utility Tests ---

func TestExecFeedbackUtility_NoData(t *testing.T) {
	result := ComputeExecFeedbackUtility(0.8, 0)
	if result != 0.5 {
		t.Errorf("expected neutral 0.5 with no data, got %f", result)
	}
}

func TestExecFeedbackUtility_HighSuccess(t *testing.T) {
	result := ComputeExecFeedbackUtility(0.9, 10)
	if result < 0.89 || result > 0.91 {
		t.Errorf("expected ~0.90, got %f", result)
	}
}

func TestExecFeedbackUtility_LowSuccess(t *testing.T) {
	result := ComputeExecFeedbackUtility(0.2, 10)
	if result < 0.19 || result > 0.21 {
		t.Errorf("expected ~0.20, got %f", result)
	}
}

func TestExecFeedbackUtility_Clamped(t *testing.T) {
	result := ComputeExecFeedbackUtility(1.5, 5)
	if result != 1.0 {
		t.Errorf("expected clamped 1.0, got %f", result)
	}
}

// --- Execution Feedback Risk Tests ---

func TestExecFeedbackRisk_NoData(t *testing.T) {
	result := ComputeExecFeedbackRisk(0, 0, 0, 0)
	if result != 0.0 {
		t.Errorf("expected 0 with no data, got %f", result)
	}
}

func TestExecFeedbackRisk_RepeatedFailures(t *testing.T) {
	result := ComputeExecFeedbackRisk(5, 0, 0, 10)
	// repeat=5/5=1.0*0.40 + abort=0*0.35 + blocked=0*0.25 = 0.40
	if math.Abs(result-0.40) > 0.01 {
		t.Errorf("expected ~0.40, got %f", result)
	}
}

func TestExecFeedbackRisk_Aborts(t *testing.T) {
	result := ComputeExecFeedbackRisk(0, 3, 0, 10)
	// repeat=0 + abort=3/3=1.0*0.35 + blocked=0 = 0.35
	if math.Abs(result-0.35) > 0.01 {
		t.Errorf("expected ~0.35, got %f", result)
	}
}

func TestExecFeedbackRisk_Blocked(t *testing.T) {
	result := ComputeExecFeedbackRisk(0, 0, 5, 10)
	// repeat=0 + abort=0 + blocked=5/5=1.0*0.25 = 0.25
	if math.Abs(result-0.25) > 0.01 {
		t.Errorf("expected ~0.25, got %f", result)
	}
}

func TestExecFeedbackRisk_Combined(t *testing.T) {
	result := ComputeExecFeedbackRisk(5, 3, 5, 20)
	// repeat=1.0*0.40 + abort=1.0*0.35 + blocked=1.0*0.25 = 1.0
	if math.Abs(result-1.0) > 0.01 {
		t.Errorf("expected 1.0, got %f", result)
	}
}

// --- Blend Tests ---

func TestBlendExecFeedback_NoData(t *testing.T) {
	result := BlendExecFeedback(0.7, 0.3, 0) // no feedback data
	if result != 0.7 {
		t.Errorf("expected passthrough 0.7, got %f", result)
	}
}

func TestBlendExecFeedback_WithData(t *testing.T) {
	result := BlendExecFeedback(0.8, 0.2, 5) // has feedback
	// 0.8*0.70 + 0.2*0.30 = 0.56 + 0.06 = 0.62
	if math.Abs(result-0.62) > 0.01 {
		t.Errorf("expected ~0.62, got %f", result)
	}
}

func TestBlendExecFeedback_HighFeedback(t *testing.T) {
	result := BlendExecFeedback(0.5, 1.0, 10) // feedback higher
	// 0.5*0.70 + 1.0*0.30 = 0.35 + 0.30 = 0.65
	if math.Abs(result-0.65) > 0.01 {
		t.Errorf("expected ~0.65, got %f", result)
	}
}

// --- ComputeFromInputs: Feedback Integration Tests ---

func TestComputeFromInputs_FeedbackAffectsExecutionUtility(t *testing.T) {
	base := ObjectiveInputs{
		TargetMonthlyIncome: 5000,
		MaxDailyWorkHours:   8,
		MinFamilyTimeHours:  4,
		BlockedHoursToday:   2,
		TotalActionCount:    10,
	}

	// Without feedback.
	_, _, summaryNone := ComputeFromInputs(base)

	// With high success feedback — should increase execution utility.
	withGoodFeedback := base
	withGoodFeedback.ExecFeedbackSuccessRate = 0.95
	withGoodFeedback.ExecFeedbackTotalExecutions = 20
	_, _, summaryGood := ComputeFromInputs(withGoodFeedback)

	// With poor feedback — should decrease execution utility.
	withBadFeedback := base
	withBadFeedback.ExecFeedbackSuccessRate = 0.1
	withBadFeedback.ExecFeedbackTotalExecutions = 20
	_, _, summaryBad := ComputeFromInputs(withBadFeedback)

	if summaryGood.UtilityScore < summaryNone.UtilityScore {
		t.Errorf("high-success feedback should not decrease utility: good=%f none=%f",
			summaryGood.UtilityScore, summaryNone.UtilityScore)
	}
	if summaryBad.UtilityScore > summaryNone.UtilityScore {
		t.Errorf("low-success feedback should not increase utility: bad=%f none=%f",
			summaryBad.UtilityScore, summaryNone.UtilityScore)
	}
}

func TestComputeFromInputs_FeedbackAffectsExecutionRisk(t *testing.T) {
	base := ObjectiveInputs{
		TargetMonthlyIncome: 5000,
		MaxDailyWorkHours:   8,
		MinFamilyTimeHours:  4,
		TotalActionCount:    10,
	}

	// Without feedback.
	_, riskNone, _ := ComputeFromInputs(base)

	// With high repeated failures + aborts.
	withRisk := base
	withRisk.ExecFeedbackRepeatedFailures = 5
	withRisk.ExecFeedbackAbortedCount = 3
	withRisk.ExecFeedbackBlockedCount = 3
	withRisk.ExecFeedbackTotalExecutions = 20
	_, riskHigh, _ := ComputeFromInputs(withRisk)

	if riskHigh.ExecutionRisk <= riskNone.ExecutionRisk {
		t.Errorf("feedback failures should increase execution risk: high=%f none=%f",
			riskHigh.ExecutionRisk, riskNone.ExecutionRisk)
	}
}

func TestComputeFromInputs_FeedbackChangesNetUtility(t *testing.T) {
	// Prove that feedback causally changes net_utility.
	base := ObjectiveInputs{
		TargetMonthlyIncome: 5000,
		MaxDailyWorkHours:   8,
		MinFamilyTimeHours:  4,
		BlockedHoursToday:   2,
		TotalActionCount:    10,
	}
	_, _, summaryBase := ComputeFromInputs(base)

	// Catastrophic feedback.
	bad := base
	bad.ExecFeedbackSuccessRate = 0.0
	bad.ExecFeedbackRepeatedFailures = 10
	bad.ExecFeedbackAbortedCount = 5
	bad.ExecFeedbackBlockedCount = 5
	bad.ExecFeedbackTotalExecutions = 30
	_, _, summaryBad := ComputeFromInputs(bad)

	if summaryBad.NetUtility >= summaryBase.NetUtility {
		t.Errorf("catastrophic feedback must reduce net_utility: bad=%f base=%f",
			summaryBad.NetUtility, summaryBase.NetUtility)
	}
}

func TestComputeFromInputs_NoFeedback_BehaviorUnchanged(t *testing.T) {
	// Verify that zero feedback data does NOT change any score vs pre-55A behavior.
	inputs := ObjectiveInputs{
		VerifiedMonthlyIncome: 3000,
		TargetMonthlyIncome:   5000,
		BestOpenOppScore:      0.7,
		OpenOpportunityCount:  5,
		PressureScore:         0.3,
		OwnerLoadScore:        0.4,
		AvailableHoursToday:   5,
		MaxDailyWorkHours:     8,
		BlockedHoursToday:     3,
		MinFamilyTimeHours:    4,
		DiversificationIndex:  0.6,
		DominantAllocation:    0.4,
		ActiveStrategies:      3,
		PortfolioROI:          30,
		PricingConfidence:     0.7,
		WinRate:               0.6,
		FailedActionCount:     2,
		PendingActionCount:    3,
		TotalActionCount:      10,
		// All exec feedback fields default to zero.
	}

	objState, riskState, summary := ComputeFromInputs(inputs)

	// Execution utility: computed from external actions, NOT blended because TotalExecutions=0.
	rawExecUtil := ComputeExecutionUtility(2, 3, 10, 5, 8)
	if math.Abs(objState.ExecutionReadinessScore-rawExecUtil) > 0.001 {
		t.Errorf("with zero feedback, execution readiness should match raw: got=%f want=%f",
			objState.ExecutionReadinessScore, rawExecUtil)
	}

	// Execution risk: same.
	rawExecRisk := ComputeExecutionRisk(2, 10)
	if math.Abs(riskState.ExecutionRisk-rawExecRisk) > 0.001 {
		t.Errorf("with zero feedback, execution risk should match raw: got=%f want=%f",
			riskState.ExecutionRisk, rawExecRisk)
	}

	// Utility and risk should be bounded.
	if summary.UtilityScore < 0 || summary.UtilityScore > 1 {
		t.Errorf("utility out of bounds: %f", summary.UtilityScore)
	}
	if summary.RiskScore < 0 || summary.RiskScore > 1 {
		t.Errorf("risk out of bounds: %f", summary.RiskScore)
	}
	if summary.NetUtility < 0 || summary.NetUtility > 1 {
		t.Errorf("net_utility out of bounds: %f", summary.NetUtility)
	}
}

// --- Engine: ExecutionMetricsProvider integration ---

type mockExecMetricsProvider struct {
	successRate      float64
	repeatedFailures int
	abortedCount     int
	blockedCount     int
	totalExecutions  int
}

func (m *mockExecMetricsProvider) GetExecMetrics(_ context.Context) (float64, int, int, int, int) {
	return m.successRate, m.repeatedFailures, m.abortedCount, m.blockedCount, m.totalExecutions
}

func TestEngine_GatherInputs_WithExecMetrics(t *testing.T) {
	e := &Engine{}
	mock := &mockExecMetricsProvider{
		successRate:      0.85,
		repeatedFailures: 3,
		abortedCount:     1,
		blockedCount:     2,
		totalExecutions:  15,
	}
	e.WithExecutionMetrics(mock)

	ctx := context.Background()
	inputs := e.GatherInputs(ctx)

	if math.Abs(inputs.ExecFeedbackSuccessRate-0.85) > 0.001 {
		t.Errorf("expected success rate 0.85, got %f", inputs.ExecFeedbackSuccessRate)
	}
	if inputs.ExecFeedbackRepeatedFailures != 3 {
		t.Errorf("expected repeated 3, got %d", inputs.ExecFeedbackRepeatedFailures)
	}
	if inputs.ExecFeedbackAbortedCount != 1 {
		t.Errorf("expected aborted 1, got %d", inputs.ExecFeedbackAbortedCount)
	}
	if inputs.ExecFeedbackBlockedCount != 2 {
		t.Errorf("expected blocked 2, got %d", inputs.ExecFeedbackBlockedCount)
	}
	if inputs.ExecFeedbackTotalExecutions != 15 {
		t.Errorf("expected total 15, got %d", inputs.ExecFeedbackTotalExecutions)
	}
}

func TestEngine_GatherInputs_NoExecMetrics(t *testing.T) {
	e := &Engine{} // no provider
	ctx := context.Background()
	inputs := e.GatherInputs(ctx)

	if inputs.ExecFeedbackTotalExecutions != 0 {
		t.Errorf("expected 0 executions without provider, got %d", inputs.ExecFeedbackTotalExecutions)
	}
	if inputs.ExecFeedbackSuccessRate != 0 {
		t.Errorf("expected 0 success rate without provider, got %f", inputs.ExecFeedbackSuccessRate)
	}
}
