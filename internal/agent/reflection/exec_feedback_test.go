package reflection

import (
	"context"
	"testing"
	"time"
)

// --- Test helper: mock ExecutionFeedbackProvider ---

type mockExecFeedbackProvider struct {
	entries []ExecutionFeedbackEntry
}

func (m *mockExecFeedbackProvider) GetReflectionFeedback(ctx context.Context) []ExecutionFeedbackEntry {
	return m.entries
}

// --- MetaAnalyze: Execution Feedback Rule Tests ---

func TestRuleExecRepeatedFailure_BelowThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			RepeatedFailureCount: 1, TotalCount: 5,
		},
	}
	insights := ruleExecRepeatedFailure(data, ReflectionInsights{})
	if len(insights.Inefficiencies) != 0 {
		t.Errorf("expected no inefficiency below threshold, got %d", len(insights.Inefficiencies))
	}
	if len(insights.Signals) != 0 {
		t.Errorf("expected no signal below threshold, got %d", len(insights.Signals))
	}
}

func TestRuleExecRepeatedFailure_AtThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			RepeatedFailureCount: 2, TotalCount: 10,
		},
	}
	insights := ruleExecRepeatedFailure(data, ReflectionInsights{})
	if len(insights.Inefficiencies) != 1 {
		t.Fatalf("expected 1 inefficiency, got %d", len(insights.Inefficiencies))
	}
	if insights.Inefficiencies[0].Type != "execution_repeated_failure" {
		t.Errorf("unexpected type: %s", insights.Inefficiencies[0].Type)
	}
	if len(insights.Signals) != 1 || insights.Signals[0].SignalType != SignalExecutionInefficiency {
		t.Errorf("expected execution_inefficiency signal")
	}
	// severity = 2/5 = 0.40
	if insights.Signals[0].Strength < 0.39 || insights.Signals[0].Strength > 0.41 {
		t.Errorf("expected ~0.40 strength, got %f", insights.Signals[0].Strength)
	}
}

func TestRuleExecRepeatedFailure_SeverityCapped(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			RepeatedFailureCount: 10, TotalCount: 20,
		},
	}
	insights := ruleExecRepeatedFailure(data, ReflectionInsights{})
	if len(insights.Signals) != 1 {
		t.Fatalf("expected 1 signal")
	}
	if insights.Signals[0].Strength != 1.0 {
		t.Errorf("expected capped strength 1.0, got %f", insights.Signals[0].Strength)
	}
}

func TestRuleExecFailureCluster_BelowThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			FailureCount: 2, TotalCount: 5,
		},
	}
	insights := ruleExecFailureCluster(data, ReflectionInsights{})
	if len(insights.RiskFlags) != 0 {
		t.Errorf("expected no risk flag, got %d", len(insights.RiskFlags))
	}
}

func TestRuleExecFailureCluster_AtThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			FailureCount: 3, TotalCount: 10,
		},
	}
	insights := ruleExecFailureCluster(data, ReflectionInsights{})
	if len(insights.RiskFlags) != 1 {
		t.Fatalf("expected 1 risk flag, got %d", len(insights.RiskFlags))
	}
	if insights.RiskFlags[0].Type != "execution_failure_cluster" {
		t.Errorf("unexpected type: %s", insights.RiskFlags[0].Type)
	}
	if len(insights.Signals) != 1 || insights.Signals[0].SignalType != SignalExecutionRisk {
		t.Errorf("expected execution_risk signal")
	}
}

func TestRuleExecBlockedReview_BelowThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			BlockedByReviewCount: 1, TotalCount: 5,
		},
	}
	insights := ruleExecBlockedReview(data, ReflectionInsights{})
	if len(insights.Improvements) != 0 {
		t.Errorf("expected no improvement, got %d", len(insights.Improvements))
	}
}

func TestRuleExecBlockedReview_AtThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			BlockedByReviewCount: 2, TotalCount: 5,
		},
	}
	insights := ruleExecBlockedReview(data, ReflectionInsights{})
	if len(insights.Improvements) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(insights.Improvements))
	}
	if insights.Improvements[0].Type != "workflow_friction" {
		t.Errorf("unexpected type: %s", insights.Improvements[0].Type)
	}
	if len(insights.Signals) != 1 || insights.Signals[0].SignalType != SignalWorkflowFriction {
		t.Errorf("expected workflow_friction signal")
	}
}

func TestRuleExecObjectiveAbort_NoAborts(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			ObjectiveAbortCount: 0, TotalCount: 5,
		},
	}
	insights := ruleExecObjectiveAbort(data, ReflectionInsights{})
	if len(insights.RiskFlags) != 0 {
		t.Errorf("expected no risk flag, got %d", len(insights.RiskFlags))
	}
}

func TestRuleExecObjectiveAbort_OneAbort(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			ObjectiveAbortCount: 1, TotalCount: 5,
		},
	}
	insights := ruleExecObjectiveAbort(data, ReflectionInsights{})
	if len(insights.RiskFlags) != 1 {
		t.Fatalf("expected 1 risk flag, got %d", len(insights.RiskFlags))
	}
	if insights.RiskFlags[0].Type != "system_instability" {
		t.Errorf("unexpected type: %s", insights.RiskFlags[0].Type)
	}
	if len(insights.Signals) != 1 || insights.Signals[0].SignalType != SignalSystemInstability {
		t.Errorf("expected system_instability signal")
	}
}

func TestRuleExecPositiveReinforcement_InsufficientSuccess(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			SafeSuccessCount: 2, FailureCount: 0, TotalCount: 5,
		},
	}
	insights := ruleExecPositiveReinforcement(data, ReflectionInsights{})
	if len(insights.Improvements) != 0 {
		t.Errorf("expected no improvement with low success count")
	}
}

func TestRuleExecPositiveReinforcement_TooManyFailures(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			SafeSuccessCount: 5, FailureCount: 3, TotalCount: 10,
		},
	}
	insights := ruleExecPositiveReinforcement(data, ReflectionInsights{})
	if len(insights.Improvements) != 0 {
		t.Errorf("expected no improvement when failure ratio too high (30%%)")
	}
}

func TestRuleExecPositiveReinforcement_GoodPattern(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			SafeSuccessCount: 5, FailureCount: 1, TotalCount: 10,
		},
	}
	insights := ruleExecPositiveReinforcement(data, ReflectionInsights{})
	if len(insights.Improvements) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(insights.Improvements))
	}
	if insights.Improvements[0].Type != "positive_reinforcement" {
		t.Errorf("unexpected type: %s", insights.Improvements[0].Type)
	}
	if len(insights.Signals) != 1 || insights.Signals[0].SignalType != SignalPositiveReinforcement {
		t.Errorf("expected positive_reinforcement signal")
	}
}

func TestRuleExecGovernanceFriction_BelowThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			BlockedByGovernanceCount: 1, TotalCount: 5,
		},
	}
	insights := ruleExecGovernanceFriction(data, ReflectionInsights{})
	if len(insights.Improvements) != 0 {
		t.Errorf("expected no improvement, got %d", len(insights.Improvements))
	}
}

func TestRuleExecGovernanceFriction_AtThreshold(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			BlockedByGovernanceCount: 2, TotalCount: 5,
		},
	}
	insights := ruleExecGovernanceFriction(data, ReflectionInsights{})
	if len(insights.Improvements) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(insights.Improvements))
	}
	if insights.Improvements[0].Type != "governance_friction" {
		t.Errorf("unexpected type: %s", insights.Improvements[0].Type)
	}
	if len(insights.Signals) != 1 || insights.Signals[0].SignalType != SignalGovernanceFriction {
		t.Errorf("expected governance_friction signal")
	}
}

// --- MetaAnalyze Integration: verify all 11 rules fire together ---

func TestMetaAnalyze_AllExecRulesFire(t *testing.T) {
	data := AggregatedData{
		ValuePerHour:   5.0, // triggers rule 1
		OwnerLoadScore: 0.9, // triggers rule 2
		AvgAccuracy:    0.3, // triggers rule 3
		ActionsCount:   10,  // needed for rule 4
		SuccessRate:    0.1, // triggers rule 4 (only 1 success)
		ManualActionCounts: map[string]int{
			"deploy": 5, // triggers rule 5
		},
		ExecutionFeedback: ReflectionExecutionSummary{
			RepeatedFailureCount:     3,
			FailureCount:             4,
			BlockedByReviewCount:     3,
			ObjectiveAbortCount:      2,
			SafeSuccessCount:         0,
			BlockedByGovernanceCount: 3,
			TotalCount:               20,
		},
	}
	insights := MetaAnalyze(data)

	// 5 legacy rules + 5 new exec rules (no reinforcement — SafeSuccessCount=0)
	expectedInefficiencies := 2 // low_efficiency + execution_repeated_failure
	expectedRiskFlags := 3      // overload + execution_failure_cluster + system_instability
	expectedImprovements := 3   // automation + workflow_friction + governance_friction
	expectedSignals := 8        // one per rule that fired (8 total, no reinforcement)

	// Also count automation signal + pricing signal.
	// Pricing → inefficiency + signal = adds to inefficiencies and signals.
	// Income instability → risk flag + signal.
	expectedInefficiencies += 1 // pricing_misalignment
	expectedRiskFlags += 1      // income_instability
	expectedSignals += 2        // pricing + income signals

	if len(insights.Inefficiencies) != expectedInefficiencies {
		t.Errorf("expected %d inefficiencies, got %d", expectedInefficiencies, len(insights.Inefficiencies))
	}
	if len(insights.RiskFlags) != expectedRiskFlags {
		t.Errorf("expected %d risk flags, got %d", expectedRiskFlags, len(insights.RiskFlags))
	}
	if len(insights.Improvements) != expectedImprovements {
		t.Errorf("expected %d improvements, got %d", expectedImprovements, len(insights.Improvements))
	}
	if len(insights.Signals) < 8 {
		t.Errorf("expected at least 8 signals, got %d", len(insights.Signals))
	}
}

func TestMetaAnalyze_ReinforcementFiresWhenClean(t *testing.T) {
	data := AggregatedData{
		ExecutionFeedback: ReflectionExecutionSummary{
			SafeSuccessCount: 6,
			FailureCount:     1,
			TotalCount:       10,
		},
	}
	insights := MetaAnalyze(data)
	found := false
	for _, s := range insights.Signals {
		if s.SignalType == SignalPositiveReinforcement {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected positive_reinforcement signal when safe successes high and failures low")
	}
}

func TestMetaAnalyze_NoExecSignals_WhenNoFeedback(t *testing.T) {
	data := AggregatedData{} // all zeros
	insights := MetaAnalyze(data)
	for _, s := range insights.Signals {
		switch s.SignalType {
		case SignalExecutionInefficiency, SignalExecutionRisk,
			SignalWorkflowFriction, SignalSystemInstability,
			SignalPositiveReinforcement, SignalGovernanceFriction:
			t.Errorf("unexpected execution signal %s when no feedback data", s.SignalType)
		}
	}
}

// --- Aggregator: ExecutionFeedbackProvider integration ---

func TestAggregator_WithExecutionFeedback(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockExecFeedbackProvider{
		entries: []ExecutionFeedbackEntry{
			{Signal: "safe_action_succeeded", Outcome: "completed", Success: true, TaskID: "t1", CreatedAt: now},
			{Signal: "repeated_failure", Outcome: "failed", Success: false, TaskID: "t2", CreatedAt: now},
			{Signal: "blocked_by_review", Outcome: "blocked", Success: false, TaskID: "t3", CreatedAt: now},
			{Signal: "blocked_by_governance", Outcome: "blocked", Success: false, TaskID: "t4", CreatedAt: now},
			{Signal: "objective_penalty_abort", Outcome: "aborted", Success: false, TaskID: "t5", CreatedAt: now},
			{Signal: "task_completed", Outcome: "completed", Success: true, TaskID: "t6", CreatedAt: now},
			{Signal: "unknown_signal", Outcome: "completed", Success: true, TaskID: "t7", CreatedAt: now},
		},
	}

	agg := NewAggregator(nil).WithExecutionFeedback(mock)
	ctx := context.Background()
	data := agg.Aggregate(ctx, now.Add(-1*time.Hour), now)

	ef := data.ExecutionFeedback
	if ef.TotalCount != 7 {
	}
	if ef.SuccessCount != 3 {
		t.Errorf("expected success=3, got %d", ef.SuccessCount)
	}
	if ef.FailureCount != 1 {
		t.Errorf("expected failure=1, got %d", ef.FailureCount)
	}
	if ef.SafeSuccessCount != 1 {
		t.Errorf("expected safe_success=1, got %d", ef.SafeSuccessCount)
	}
	if ef.RepeatedFailureCount != 1 {
		t.Errorf("expected repeated_failure=1, got %d", ef.RepeatedFailureCount)
	}
	if ef.BlockedByReviewCount != 1 {
		t.Errorf("expected blocked_review=1, got %d", ef.BlockedByReviewCount)
	}
	if ef.BlockedByGovernanceCount != 1 {
		t.Errorf("expected blocked_governance=1, got %d", ef.BlockedByGovernanceCount)
	}
	if ef.ObjectiveAbortCount != 1 {
		t.Errorf("expected objective_abort=1, got %d", ef.ObjectiveAbortCount)
	}
}

func TestAggregator_NoExecutionFeedback(t *testing.T) {
	agg := NewAggregator(nil) // no execution feedback provider
	ctx := context.Background()
	data := agg.Aggregate(ctx, time.Now().Add(-1*time.Hour), time.Now())

	ef := data.ExecutionFeedback
	if ef.TotalCount != 0 {
		t.Errorf("expected total=0 when no provider, got %d", ef.TotalCount)
	}
}

func TestAggregator_ExecutionFeedbackFiltersOld(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour)
	mock := &mockExecFeedbackProvider{
		entries: []ExecutionFeedbackEntry{
			{Signal: "safe_action_succeeded", Success: true, CreatedAt: now},
			{Signal: "repeated_failure", Success: false, CreatedAt: old}, // before periodStart
		},
	}

	agg := NewAggregator(nil).WithExecutionFeedback(mock)
	ctx := context.Background()
	data := agg.Aggregate(ctx, now.Add(-1*time.Hour), now)

	if data.ExecutionFeedback.TotalCount != 1 {
		t.Errorf("expected 1 entry after filtering, got %d", data.ExecutionFeedback.TotalCount)
	}
	if data.ExecutionFeedback.SafeSuccessCount != 1 {
		t.Errorf("expected 1 safe success, got %d", data.ExecutionFeedback.SafeSuccessCount)
	}
}

// --- clampMeta utility ---

func TestClampMeta(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{-1.0, 0.0},
		{0.0, 0.0},
		{0.5, 0.5},
		{1.0, 1.0},
		{2.0, 1.0},
	}
	for _, tc := range tests {
		got := clampMeta(tc.in)
		if got != tc.want {
			t.Errorf("clampMeta(%f) = %f, want %f", tc.in, got, tc.want)
		}
	}
}
