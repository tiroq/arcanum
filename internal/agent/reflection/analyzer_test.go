package reflection

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	"github.com/tiroq/arcanum/internal/agent/planning"
)

func baseInput(cycleID string) AnalysisInput {
	return AnalysisInput{
		CycleID:   cycleID,
		Timestamp: time.Now().UTC(),
	}
}

func storedDecision(action, goalType string) planning.StoredDecision {
	return planning.StoredDecision{
		ID:             uuid.New(),
		CycleID:        uuid.New().String(),
		GoalID:         uuid.New().String(),
		GoalType:       goalType,
		SelectedAction: action,
		Explanation:    "test",
		PlannedAt:      time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
	}
}

func memoryRecord(actionType string, total, succ, fail, neutral int) actionmemory.ActionMemoryRecord {
	succRate := float64(0)
	failRate := float64(0)
	if total > 0 {
		succRate = float64(succ) / float64(total)
		failRate = float64(fail) / float64(total)
	}
	return actionmemory.ActionMemoryRecord{
		ID:          uuid.New(),
		ActionType:  actionType,
		TargetType:  "job",
		TotalRuns:   total,
		SuccessRuns: succ,
		FailureRuns: fail,
		NeutralRuns: neutral,
		SuccessRate: succRate,
		FailureRate: failRate,
		LastUpdated: time.Now().UTC(),
	}
}

func actionOutcome(actionType string, status outcome.OutcomeStatus) outcome.ActionOutcome {
	return outcome.ActionOutcome{
		ID:            uuid.New(),
		ActionID:      uuid.New(),
		GoalID:        "goal-1",
		ActionType:    actionType,
		TargetType:    "job",
		TargetID:      uuid.New(),
		OutcomeStatus: status,
		BeforeState:   map[string]any{},
		AfterState:    map[string]any{},
		EvaluatedAt:   time.Now().UTC(),
	}
}

// --- Rule A: repeated_low_value_action ---

func TestRuleA_RepeatedLowValue_Triggered(t *testing.T) {
	input := baseInput("cycle-a1")
	// 4 selections of retry_job, but memory shows only 20% success
	for i := 0; i < 4; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 2, 6, 2))

	findings := Analyze(input)
	found := findByRule(findings, RuleRepeatedLowValue)
	if found == nil {
		t.Fatal("expected repeated_low_value_action finding, got none")
	}
	if found.ActionType != "retry_job" {
		t.Errorf("expected action_type=retry_job, got %s", found.ActionType)
	}
	if found.Severity != SeverityWarning {
		t.Errorf("expected severity=warning, got %s", found.Severity)
	}
}

func TestRuleA_RepeatedLowValue_NotTriggered_HighSuccess(t *testing.T) {
	input := baseInput("cycle-a2")
	for i := 0; i < 4; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}
	// 80% success — above threshold
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 8, 1, 1))

	findings := Analyze(input)
	found := findByRule(findings, RuleRepeatedLowValue)
	if found != nil {
		t.Error("expected no repeated_low_value_action finding for high success action")
	}
}

func TestRuleA_RepeatedLowValue_NotTriggered_FewSelections(t *testing.T) {
	input := baseInput("cycle-a3")
	// Only 2 selections (below threshold of 3)
	for i := 0; i < 2; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 1, 8, 1))

	findings := Analyze(input)
	found := findByRule(findings, RuleRepeatedLowValue)
	if found != nil {
		t.Error("expected no finding with fewer than 3 selections")
	}
}

// --- Rule B: planner_ignores_feedback ---

func TestRuleB_IgnoresFeedback_Triggered(t *testing.T) {
	input := baseInput("cycle-b1")
	input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	// Memory says avoid (failure_rate >= 0.5)
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 1, 7, 2))

	findings := Analyze(input)
	found := findByRule(findings, RulePlannerIgnoresFeedback)
	if found == nil {
		t.Fatal("expected planner_ignores_feedback finding, got none")
	}
	if found.ActionType != "retry_job" {
		t.Errorf("expected action_type=retry_job, got %s", found.ActionType)
	}
}

func TestRuleB_IgnoresFeedback_NotTriggered_NoAvoid(t *testing.T) {
	input := baseInput("cycle-b2")
	input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	// Memory says prefer (not avoid)
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 8, 1, 1))

	findings := Analyze(input)
	found := findByRule(findings, RulePlannerIgnoresFeedback)
	if found != nil {
		t.Error("expected no finding when feedback is not avoid_action")
	}
}

// --- Rule C: planner_stalling ---

func TestRuleC_Stalling_Triggered(t *testing.T) {
	input := baseInput("cycle-c1")
	// 8 out of 10 decisions are noop (80%)
	for i := 0; i < 8; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("noop", "reduce_retry_rate"))
	}
	for i := 0; i < 2; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}

	findings := Analyze(input)
	found := findByRule(findings, RulePlannerStalling)
	if found == nil {
		t.Fatal("expected planner_stalling finding, got none")
	}
	if found.Severity != SeverityWarning {
		t.Errorf("expected severity=warning, got %s", found.Severity)
	}
}

func TestRuleC_Stalling_NotTriggered_LowNoopRatio(t *testing.T) {
	input := baseInput("cycle-c2")
	// 2 out of 10 are noop (20%) — below threshold
	for i := 0; i < 2; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("noop", "reduce_retry_rate"))
	}
	for i := 0; i < 8; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}

	findings := Analyze(input)
	found := findByRule(findings, RulePlannerStalling)
	if found != nil {
		t.Error("expected no stalling finding with low noop ratio")
	}
}

func TestRuleC_Stalling_NotTriggered_TooFewDecisions(t *testing.T) {
	input := baseInput("cycle-c3")
	// Only 3 decisions (below threshold of 5)
	for i := 0; i < 3; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("noop", "reduce_retry_rate"))
	}

	findings := Analyze(input)
	found := findByRule(findings, RulePlannerStalling)
	if found != nil {
		t.Error("expected no stalling finding with fewer than 5 decisions")
	}
}

// --- Rule D: unstable_action_effectiveness ---

func TestRuleD_Unstable_Triggered(t *testing.T) {
	input := baseInput("cycle-d1")
	// Alternating success/failure — high variance
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			input.RecentOutcomes = append(input.RecentOutcomes, actionOutcome("retry_job", outcome.OutcomeSuccess))
		} else {
			input.RecentOutcomes = append(input.RecentOutcomes, actionOutcome("retry_job", outcome.OutcomeFailure))
		}
	}

	findings := Analyze(input)
	found := findByRule(findings, RuleUnstableEffectiveness)
	if found == nil {
		t.Fatal("expected unstable_action_effectiveness finding, got none")
	}
	if found.ActionType != "retry_job" {
		t.Errorf("expected action_type=retry_job, got %s", found.ActionType)
	}
}

func TestRuleD_Unstable_NotTriggered_Consistent(t *testing.T) {
	input := baseInput("cycle-d2")
	// All successes — zero variance
	for i := 0; i < 10; i++ {
		input.RecentOutcomes = append(input.RecentOutcomes, actionOutcome("retry_job", outcome.OutcomeSuccess))
	}

	findings := Analyze(input)
	found := findByRule(findings, RuleUnstableEffectiveness)
	if found != nil {
		t.Error("expected no unstable finding for consistent outcomes")
	}
}

func TestRuleD_Unstable_NotTriggered_TooFewSamples(t *testing.T) {
	input := baseInput("cycle-d3")
	// Only 3 outcomes (below threshold of 5)
	input.RecentOutcomes = append(input.RecentOutcomes, actionOutcome("retry_job", outcome.OutcomeSuccess))
	input.RecentOutcomes = append(input.RecentOutcomes, actionOutcome("retry_job", outcome.OutcomeFailure))
	input.RecentOutcomes = append(input.RecentOutcomes, actionOutcome("retry_job", outcome.OutcomeSuccess))

	findings := Analyze(input)
	found := findByRule(findings, RuleUnstableEffectiveness)
	if found != nil {
		t.Error("expected no unstable finding with fewer than 5 samples")
	}
}

// --- Rule E: effective_action_pattern ---

func TestRuleE_Effective_Triggered(t *testing.T) {
	input := baseInput("cycle-e1")
	for i := 0; i < 5; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}
	// 80% success rate
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 8, 1, 1))

	findings := Analyze(input)
	found := findByRule(findings, RuleEffectivePattern)
	if found == nil {
		t.Fatal("expected effective_action_pattern finding, got none")
	}
	if found.Severity != SeverityInfo {
		t.Errorf("expected severity=info, got %s", found.Severity)
	}
}

func TestRuleE_Effective_NotTriggered_LowSuccess(t *testing.T) {
	input := baseInput("cycle-e2")
	for i := 0; i < 5; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}
	// 50% success — below threshold
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 5, 3, 2))

	findings := Analyze(input)
	found := findByRule(findings, RuleEffectivePattern)
	if found != nil {
		t.Error("expected no effective finding for low success rate")
	}
}

func TestRuleE_Effective_NotTriggered_FewSelections(t *testing.T) {
	input := baseInput("cycle-e3")
	// Only 2 selections (below threshold of 3)
	for i := 0; i < 2; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 9, 0, 1))

	findings := Analyze(input)
	found := findByRule(findings, RuleEffectivePattern)
	if found != nil {
		t.Error("expected no effective finding with fewer than 3 selections")
	}
}

// --- Multiple rules can fire simultaneously ---

func TestMultipleRulesCanFire(t *testing.T) {
	input := baseInput("cycle-multi")

	// Set up conditions for Rule A (repeated low value for retry_job)
	for i := 0; i < 4; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("retry_job", "reduce_retry_rate"))
	}
	input.ActionMemory = append(input.ActionMemory, memoryRecord("retry_job", 10, 1, 7, 2))

	// Also set up Rule E (effective action for trigger_resync)
	for i := 0; i < 5; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("trigger_resync", "resolve_queue_backlog"))
	}
	input.ActionMemory = append(input.ActionMemory, memoryRecord("trigger_resync", 10, 9, 0, 1))

	findings := Analyze(input)

	foundA := findByRule(findings, RuleRepeatedLowValue)
	foundB := findByRule(findings, RulePlannerIgnoresFeedback)
	foundE := findByRule(findings, RuleEffectivePattern)

	if foundA == nil {
		t.Error("expected Rule A finding")
	}
	if foundB == nil {
		t.Error("expected Rule B finding (retry_job has avoid feedback)")
	}
	if foundE == nil {
		t.Error("expected Rule E finding for trigger_resync")
	}
}

// --- Empty input produces no findings ---

func TestAnalyze_EmptyInput(t *testing.T) {
	input := baseInput("cycle-empty")
	findings := Analyze(input)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for empty input, got %d", len(findings))
	}
}

// --- Noop and log_recommendation are excluded from A/B/E ---

func TestNoopExcludedFromRuleABE(t *testing.T) {
	input := baseInput("cycle-noop")
	for i := 0; i < 10; i++ {
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("noop", "reduce_retry_rate"))
		input.RecentDecisions = append(input.RecentDecisions, storedDecision("log_recommendation", "reduce_retry_rate"))
	}
	input.ActionMemory = append(input.ActionMemory, memoryRecord("noop", 20, 0, 20, 0))
	input.ActionMemory = append(input.ActionMemory, memoryRecord("log_recommendation", 20, 0, 20, 0))

	findings := Analyze(input)

	for _, f := range findings {
		if f.Rule == RuleRepeatedLowValue || f.Rule == RulePlannerIgnoresFeedback || f.Rule == RuleEffectivePattern {
			if f.ActionType == "noop" || f.ActionType == "log_recommendation" {
				t.Errorf("noop/log_recommendation should be excluded from rule %s", f.Rule)
			}
		}
	}
}

// --- Helper ---

func findByRule(findings []Finding, rule Rule) *Finding {
	for i := range findings {
		if findings[i].Rule == rule {
			return &findings[i]
		}
	}
	return nil
}
