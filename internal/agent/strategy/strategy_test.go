package strategy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// --- Test 1: Candidate generation for retry goal ---

func TestGenerate_RetryGoal_ProducesFiveCandidates(t *testing.T) {
	now := time.Now().UTC()
	plans := Generate("goal-1", "reduce_retry_rate", now)

	if len(plans) != 5 {
		t.Fatalf("expected 5 candidates for retry goal, got %d", len(plans))
	}

	types := map[StrategyType]bool{}
	for _, p := range plans {
		types[p.StrategyType] = true
		if p.GoalID != "goal-1" {
			t.Errorf("plan %s has wrong goal_id %s", p.StrategyType, p.GoalID)
		}
		if p.GoalType != "reduce_retry_rate" {
			t.Errorf("plan %s has wrong goal_type %s", p.StrategyType, p.GoalType)
		}
		if p.ID == uuid.Nil {
			t.Errorf("plan %s has nil UUID", p.StrategyType)
		}
		if len(p.Steps) == 0 {
			t.Errorf("plan %s has no steps", p.StrategyType)
		}
		if len(p.Steps) > MaxStrategyDepth {
			t.Errorf("plan %s exceeds max depth: %d > %d", p.StrategyType, len(p.Steps), MaxStrategyDepth)
		}
	}

	expected := []StrategyType{
		StrategyDirectRetry,
		StrategyObserveThenRetry,
		StrategyRetryThenRecommend,
		StrategyRecommendOnly,
		StrategyNoop,
	}
	for _, e := range expected {
		if !types[e] {
			t.Errorf("missing expected strategy type %s", e)
		}
	}
}

func TestGenerate_InvestigateFailedGoal_AlsoRetryFamily(t *testing.T) {
	plans := Generate("goal-2", "investigate_failed_jobs", time.Now().UTC())
	if len(plans) != 5 {
		t.Fatalf("expected 5 retry-family candidates, got %d", len(plans))
	}
}

// --- Test 2: Candidate generation for backlog goal ---

func TestGenerate_BacklogGoal_ProducesFourCandidates(t *testing.T) {
	plans := Generate("goal-3", "resolve_queue_backlog", time.Now().UTC())

	if len(plans) != 4 {
		t.Fatalf("expected 4 candidates for backlog goal, got %d", len(plans))
	}

	types := map[StrategyType]bool{}
	for _, p := range plans {
		types[p.StrategyType] = true
	}

	expected := []StrategyType{
		StrategyDirectResync,
		StrategyObserveThenResync,
		StrategyRecommendOnly,
		StrategyNoop,
	}
	for _, e := range expected {
		if !types[e] {
			t.Errorf("missing expected strategy type %s for backlog", e)
		}
	}
}

// --- Test 3: Advisory goal produces minimal strategies ---

func TestGenerate_AdvisoryGoal_ProducesTwoCandidates(t *testing.T) {
	plans := Generate("goal-4", "increase_reliability", time.Now().UTC())

	if len(plans) != 2 {
		t.Fatalf("expected 2 candidates for advisory goal, got %d", len(plans))
	}

	types := map[StrategyType]bool{}
	for _, p := range plans {
		types[p.StrategyType] = true
	}

	if !types[StrategyRecommendOnly] || !types[StrategyNoop] {
		t.Errorf("advisory should have recommendation_only and noop, got %v", types)
	}
}

// --- Test 4: Simpler strategy wins when scores are close ---

func TestSelect_SimplicityBias_PrefersSimpler(t *testing.T) {
	now := time.Now().UTC()

	// Create two plans: one 1-step with utility 0.50, one 2-step with utility 0.54.
	// Difference (0.04) is below SimplicityBias (0.05), so simpler should win.
	simple := StrategyPlan{
		ID:              uuid.New(),
		GoalID:          "g1",
		GoalType:        "reduce_retry_rate",
		StrategyType:    StrategyDirectRetry,
		Steps:           []StrategyStep{{Order: 1, ActionType: "retry_job"}},
		ExpectedUtility: 0.50,
		Confidence:      0.8,
	}
	complex := StrategyPlan{
		ID:           uuid.New(),
		GoalID:       "g1",
		GoalType:     "reduce_retry_rate",
		StrategyType: StrategyObserveThenRetry,
		Steps: []StrategyStep{
			{Order: 1, ActionType: "log_recommendation"},
			{Order: 2, ActionType: "retry_job"},
		},
		ExpectedUtility: 0.54,
		Confidence:      0.7,
	}

	decision := Select([]StrategyPlan{complex, simple}, "g1", "reduce_retry_rate", now)

	selected := SelectedPlan(decision)
	if selected == nil {
		t.Fatal("no strategy selected")
	}
	if selected.StrategyType != StrategyDirectRetry {
		t.Errorf("expected simpler strategy to win, got %s", selected.StrategyType)
	}
	if decision.Reason != "simplicity_bias: simpler strategy chosen" {
		t.Errorf("unexpected reason: %s", decision.Reason)
	}
}

// --- Test 5: High-utility strategy wins when clearly better ---

func TestSelect_HighUtility_WinsWhenClearlyBetter(t *testing.T) {
	now := time.Now().UTC()

	// Difference > SimplicityBias, so higher utility should win.
	simple := StrategyPlan{
		ID:              uuid.New(),
		GoalID:          "g1",
		GoalType:        "reduce_retry_rate",
		StrategyType:    StrategyDirectRetry,
		Steps:           []StrategyStep{{Order: 1, ActionType: "retry_job"}},
		ExpectedUtility: 0.40,
		Confidence:      0.8,
	}
	complex := StrategyPlan{
		ID:           uuid.New(),
		GoalID:       "g1",
		GoalType:     "reduce_retry_rate",
		StrategyType: StrategyRetryThenRecommend,
		Steps: []StrategyStep{
			{Order: 1, ActionType: "retry_job"},
			{Order: 2, ActionType: "log_recommendation"},
		},
		ExpectedUtility: 0.60,
		Confidence:      0.7,
	}

	decision := Select([]StrategyPlan{complex, simple}, "g1", "reduce_retry_rate", now)

	selected := SelectedPlan(decision)
	if selected == nil {
		t.Fatal("no strategy selected")
	}
	if selected.StrategyType != StrategyRetryThenRecommend {
		t.Errorf("expected higher-utility strategy to win, got %s", selected.StrategyType)
	}
	if decision.Reason != "highest_utility" {
		t.Errorf("unexpected reason: %s", decision.Reason)
	}
}

// --- Test 6: Blocked action causes strategy rejection ---

func TestScore_BlockedAction_RejectsEntireStrategy(t *testing.T) {
	plans := Generate("g1", "reduce_retry_rate", time.Now().UTC())

	input := ScoreInput{
		ActionFeedback:  map[string]actionmemory.ActionFeedback{},
		CandidateScores: map[string]float64{"retry_job": 0.7, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"retry_job": 0.8, "log_recommendation": 0.6},
		StabilityMode:   "normal",
		BlockedActions:  []string{"retry_job"},
	}

	scored := ScoreStrategies(plans, input)

	for _, p := range scored {
		for _, step := range p.Steps {
			if step.ActionType == "retry_job" {
				if p.ExpectedUtility != 0 {
					t.Errorf("strategy %s with blocked action should have utility 0, got %f",
						p.StrategyType, p.ExpectedUtility)
				}
				if p.RiskScore != 1.0 {
					t.Errorf("strategy %s with blocked action should have risk 1.0, got %f",
						p.StrategyType, p.RiskScore)
				}
				break
			}
		}
	}

	// recommend_only and noop should still be viable.
	for _, p := range scored {
		if p.StrategyType == StrategyRecommendOnly && p.ExpectedUtility == 0 {
			t.Error("recommendation_only should not be affected by retry_job block")
		}
		if p.StrategyType == StrategyNoop && p.ExpectedUtility != 0.10 {
			t.Errorf("noop should have baseline utility 0.10, got %f", p.ExpectedUtility)
		}
	}
}

// --- Test 7: Selection is deterministic ---

func TestSelect_Deterministic_SameInputsSameOutput(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Generate with fixed time so UUIDs differ but structure is same.
	// Score them identically, select twice.
	buildPlans := func() []StrategyPlan {
		plans := Generate("g1", "reduce_retry_rate", now)
		input := ScoreInput{
			ActionFeedback:  map[string]actionmemory.ActionFeedback{},
			CandidateScores: map[string]float64{"retry_job": 0.7, "log_recommendation": 0.5},
			CandidateConf:   map[string]float64{"retry_job": 0.8, "log_recommendation": 0.6},
			StabilityMode:   "normal",
		}
		return ScoreStrategies(plans, input)
	}

	scored1 := buildPlans()
	scored2 := buildPlans()

	d1 := Select(scored1, "g1", "reduce_retry_rate", now)
	d2 := Select(scored2, "g1", "reduce_retry_rate", now)

	s1 := SelectedPlan(d1)
	s2 := SelectedPlan(d2)

	if s1 == nil || s2 == nil {
		t.Fatal("both selections should produce a result")
	}
	if s1.StrategyType != s2.StrategyType {
		t.Errorf("determinism violated: %s vs %s", s1.StrategyType, s2.StrategyType)
	}
	if d1.Reason != d2.Reason {
		t.Errorf("determinism violated in reason: %s vs %s", d1.Reason, d2.Reason)
	}
}

// --- Test 8: Only first step executes (Mode A) ---

func TestModeA_OnlyFirstStepExecutes(t *testing.T) {
	if ExecutionMode != "plan_full_execute_first" {
		t.Fatalf("expected Mode A execution, got %s", ExecutionMode)
	}

	plans := Generate("g1", "reduce_retry_rate", time.Now().UTC())

	// Find the observe_then_retry (2-step) plan.
	var multiStep *StrategyPlan
	for i := range plans {
		if plans[i].StrategyType == StrategyObserveThenRetry {
			multiStep = &plans[i]
			break
		}
	}
	if multiStep == nil {
		t.Fatal("observe_then_retry strategy not generated")
	}

	if multiStep.StepCount() != 2 {
		t.Fatalf("expected 2 steps, got %d", multiStep.StepCount())
	}

	// First step is what gets executed in Mode A.
	first := multiStep.FirstStep()
	if first.ActionType != "log_recommendation" {
		t.Errorf("expected first step to be log_recommendation, got %s", first.ActionType)
	}
	if first.Order != 1 {
		t.Errorf("expected order 1, got %d", first.Order)
	}

	// Second step should not execute in Mode A — it has a condition.
	second := multiStep.Steps[1]
	if second.Condition == "" {
		t.Error("second step should have a condition for deferred execution")
	}
}

// --- Test 9: Safe mode suppresses multi-step strategies ---

func TestScore_SafeMode_SuppressesMultiStep(t *testing.T) {
	plans := Generate("g1", "reduce_retry_rate", time.Now().UTC())

	input := ScoreInput{
		ActionFeedback:  map[string]actionmemory.ActionFeedback{},
		CandidateScores: map[string]float64{"retry_job": 0.8, "log_recommendation": 0.6},
		CandidateConf:   map[string]float64{"retry_job": 0.9, "log_recommendation": 0.7},
		StabilityMode:   "safe_mode",
	}

	scored := ScoreStrategies(plans, input)

	for _, p := range scored {
		if p.StepCount() > 1 {
			if p.ExpectedUtility != 0 {
				t.Errorf("safe_mode: multi-step %s should have utility 0, got %f",
					p.StrategyType, p.ExpectedUtility)
			}
			if p.RiskScore != 1.0 {
				t.Errorf("safe_mode: multi-step %s should have risk 1.0, got %f",
					p.StrategyType, p.RiskScore)
			}
		}
	}

	// Single-step strategies should still work.
	for _, p := range scored {
		if p.StepCount() == 1 && p.StrategyType == StrategyDirectRetry {
			if p.ExpectedUtility == 0 {
				t.Error("safe_mode: single-step direct_retry should not be suppressed")
			}
		}
	}
}

// --- Test 10: Throttled mode penalizes multi-step ---

func TestScore_ThrottledMode_PenalizesMultiStep(t *testing.T) {
	plans := Generate("g1", "reduce_retry_rate", time.Now().UTC())

	normalInput := ScoreInput{
		ActionFeedback:  map[string]actionmemory.ActionFeedback{},
		CandidateScores: map[string]float64{"retry_job": 0.7, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"retry_job": 0.8, "log_recommendation": 0.6},
		StabilityMode:   "normal",
	}

	throttledInput := normalInput
	throttledInput.StabilityMode = "throttled"

	normalScored := ScoreStrategies(Generate("g1", "reduce_retry_rate", time.Now().UTC()), normalInput)
	throttledScored := ScoreStrategies(plans, throttledInput)

	// Find observe_then_retry in both.
	var normalOTR, throttledOTR *StrategyPlan
	for i := range normalScored {
		if normalScored[i].StrategyType == StrategyObserveThenRetry {
			normalOTR = &normalScored[i]
			break
		}
	}
	for i := range throttledScored {
		if throttledScored[i].StrategyType == StrategyObserveThenRetry {
			throttledOTR = &throttledScored[i]
			break
		}
	}

	if normalOTR == nil || throttledOTR == nil {
		t.Fatal("observe_then_retry should exist in both sets")
	}

	if throttledOTR.ExpectedUtility >= normalOTR.ExpectedUtility {
		t.Errorf("throttled multi-step utility (%f) should be lower than normal (%f)",
			throttledOTR.ExpectedUtility, normalOTR.ExpectedUtility)
	}

	// Single-step direct_retry should NOT be penalized in throttled mode.
	var normalDR, throttledDR *StrategyPlan
	for i := range normalScored {
		if normalScored[i].StrategyType == StrategyDirectRetry {
			normalDR = &normalScored[i]
		}
	}
	for i := range throttledScored {
		if throttledScored[i].StrategyType == StrategyDirectRetry {
			throttledDR = &throttledScored[i]
		}
	}
	if normalDR != nil && throttledDR != nil {
		if throttledDR.ExpectedUtility != normalDR.ExpectedUtility {
			t.Errorf("single-step direct_retry should not be penalized in throttled mode: %f vs %f",
				throttledDR.ExpectedUtility, normalDR.ExpectedUtility)
		}
	}
}

// --- Test 11: Feedback adjustments affect strategy scoring ---

func TestScore_FeedbackAdjustment(t *testing.T) {
	plans := Generate("g1", "reduce_retry_rate", time.Now().UTC())

	// With positive feedback for retry_job.
	preferInput := ScoreInput{
		ActionFeedback: map[string]actionmemory.ActionFeedback{
			"retry_job": {
				ActionType:     "retry_job",
				SampleSize:     10,
				SuccessRate:    0.80,
				Recommendation: actionmemory.RecommendPreferAction,
			},
		},
		CandidateScores: map[string]float64{"retry_job": 0.5, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"retry_job": 0.5, "log_recommendation": 0.5},
		StabilityMode:   "normal",
	}

	// With negative feedback for retry_job.
	avoidInput := ScoreInput{
		ActionFeedback: map[string]actionmemory.ActionFeedback{
			"retry_job": {
				ActionType:     "retry_job",
				SampleSize:     10,
				FailureRate:    0.70,
				Recommendation: actionmemory.RecommendAvoidAction,
			},
		},
		CandidateScores: map[string]float64{"retry_job": 0.5, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"retry_job": 0.5, "log_recommendation": 0.5},
		StabilityMode:   "normal",
	}

	preferScored := ScoreStrategies(Generate("g1", "reduce_retry_rate", time.Now().UTC()), preferInput)
	avoidScored := ScoreStrategies(plans, avoidInput)

	var preferDR, avoidDR *StrategyPlan
	for i := range preferScored {
		if preferScored[i].StrategyType == StrategyDirectRetry {
			preferDR = &preferScored[i]
		}
	}
	for i := range avoidScored {
		if avoidScored[i].StrategyType == StrategyDirectRetry {
			avoidDR = &avoidScored[i]
		}
	}

	if preferDR == nil || avoidDR == nil {
		t.Fatal("direct_retry should exist in both scored sets")
	}

	if avoidDR.ExpectedUtility >= preferDR.ExpectedUtility {
		t.Errorf("avoid feedback should lower utility: avoid=%f prefer=%f",
			avoidDR.ExpectedUtility, preferDR.ExpectedUtility)
	}
}

// --- Test 12: GoalFamily classification ---

func TestGoalFamilyClassification(t *testing.T) {
	tests := []struct {
		goalType string
		expected GoalFamily
	}{
		{"reduce_retry_rate", GoalFamilyRetry},
		{"investigate_failed_jobs", GoalFamilyRetry},
		{"resolve_queue_backlog", GoalFamilyBacklog},
		{"increase_reliability", GoalFamilyAdvisory},
		{"increase_model_quality", GoalFamilyAdvisory},
		{"reduce_latency", GoalFamilyAdvisory},
		{"unknown_goal", GoalFamilyAdvisory},
	}

	for _, tt := range tests {
		got := GoalFamilyForType(tt.goalType)
		if got != tt.expected {
			t.Errorf("GoalFamilyForType(%s) = %s, want %s", tt.goalType, got, tt.expected)
		}
	}
}

// --- Test 13: Step count and first step accessors ---

func TestStrategyPlan_StepAccessors(t *testing.T) {
	plan := StrategyPlan{
		Steps: []StrategyStep{
			{Order: 1, ActionType: "log_recommendation"},
			{Order: 2, ActionType: "retry_job"},
		},
	}

	if plan.StepCount() != 2 {
		t.Errorf("expected 2 steps, got %d", plan.StepCount())
	}

	first := plan.FirstStep()
	if first.ActionType != "log_recommendation" {
		t.Errorf("first step should be log_recommendation, got %s", first.ActionType)
	}

	// Empty plan.
	empty := StrategyPlan{}
	if empty.StepCount() != 0 {
		t.Errorf("empty plan should have 0 steps")
	}
	emptyFirst := empty.FirstStep()
	if emptyFirst.ActionType != "" {
		t.Errorf("empty plan first step should have empty action type")
	}
}

// --- Test 14: SelectedPlan returns nil for no selection ---

func TestSelectedPlan_NilOnEmptyDecision(t *testing.T) {
	decision := StrategyDecision{}
	if SelectedPlan(decision) != nil {
		t.Error("expected nil for empty decision")
	}
}

// --- Test 15: Below-threshold strategies are filtered in selection ---

func TestSelect_BelowThreshold_Filtered(t *testing.T) {
	now := time.Now().UTC()

	plans := []StrategyPlan{
		{
			ID:              uuid.New(),
			GoalID:          "g1",
			GoalType:        "reduce_retry_rate",
			StrategyType:    StrategyDirectRetry,
			Steps:           []StrategyStep{{Order: 1, ActionType: "retry_job"}},
			ExpectedUtility: 0.02, // below MinUtilityThreshold
		},
		{
			ID:              uuid.New(),
			GoalID:          "g1",
			GoalType:        "reduce_retry_rate",
			StrategyType:    StrategyNoop,
			Steps:           []StrategyStep{{Order: 1, ActionType: "noop"}},
			ExpectedUtility: 0.01, // below threshold but noop is kept
		},
	}

	decision := Select(plans, "g1", "reduce_retry_rate", now)

	selected := SelectedPlan(decision)
	if selected == nil {
		t.Fatal("noop should always be selectable")
	}
	if selected.StrategyType != StrategyNoop {
		t.Errorf("expected noop (only viable), got %s", selected.StrategyType)
	}
}

// --- Test 16: Noop has fixed baseline utility ---

func TestScore_Noop_FixedBaseline(t *testing.T) {
	plans := []StrategyPlan{
		{
			ID:           uuid.New(),
			StrategyType: StrategyNoop,
			Steps:        []StrategyStep{{Order: 1, ActionType: "noop"}},
		},
	}

	input := ScoreInput{
		StabilityMode: "safe_mode",
	}

	scored := ScoreStrategies(plans, input)
	if len(scored) != 1 {
		t.Fatal("expected 1 plan")
	}

	if scored[0].ExpectedUtility != 0.10 {
		t.Errorf("noop utility should be 0.10, got %f", scored[0].ExpectedUtility)
	}
	if scored[0].RiskScore != 0.0 {
		t.Errorf("noop risk should be 0.0, got %f", scored[0].RiskScore)
	}
	if scored[0].Confidence != 1.0 {
		t.Errorf("noop confidence should be 1.0, got %f", scored[0].Confidence)
	}
}

// --- Test 17: End-to-end generate → score → select ---

func TestEndToEnd_RetryGoal_SelectsBestStrategy(t *testing.T) {
	now := time.Now().UTC()

	plans := Generate("g1", "reduce_retry_rate", now)

	input := ScoreInput{
		ActionFeedback:  map[string]actionmemory.ActionFeedback{},
		CandidateScores: map[string]float64{"retry_job": 0.8, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"retry_job": 0.9, "log_recommendation": 0.6},
		StabilityMode:   "normal",
	}

	scored := ScoreStrategies(plans, input)
	decision := Select(scored, "g1", "reduce_retry_rate", now)

	selected := SelectedPlan(decision)
	if selected == nil {
		t.Fatal("should select a strategy")
	}

	// With high retry_job score, direct_retry should win.
	if selected.StrategyType != StrategyDirectRetry {
		t.Errorf("expected direct_retry with high retry score, got %s", selected.StrategyType)
	}
	if decision.SelectedStrategyID == uuid.Nil {
		t.Error("selected strategy ID should not be nil")
	}
	if decision.GoalID != "g1" {
		t.Error("decision goal_id mismatch")
	}
}

// --- Test 18: End-to-end backlog goal ---

func TestEndToEnd_BacklogGoal_SelectsBestStrategy(t *testing.T) {
	now := time.Now().UTC()

	plans := Generate("g2", "resolve_queue_backlog", now)

	input := ScoreInput{
		ActionFeedback:  map[string]actionmemory.ActionFeedback{},
		CandidateScores: map[string]float64{"trigger_resync": 0.8, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"trigger_resync": 0.9, "log_recommendation": 0.6},
		StabilityMode:   "normal",
	}

	scored := ScoreStrategies(plans, input)
	decision := Select(scored, "g2", "resolve_queue_backlog", now)

	selected := SelectedPlan(decision)
	if selected == nil {
		t.Fatal("should select a strategy")
	}

	if selected.StrategyType != StrategyDirectResync {
		t.Errorf("expected direct_resync with high resync score, got %s", selected.StrategyType)
	}
}

// --- Test 19: All strategy plans have bounded depth ---

func TestAllStrategies_BoundedDepth(t *testing.T) {
	goalTypes := []string{
		"reduce_retry_rate",
		"investigate_failed_jobs",
		"resolve_queue_backlog",
		"increase_reliability",
		"increase_model_quality",
		"reduce_latency",
	}

	for _, gt := range goalTypes {
		plans := Generate("g", gt, time.Now().UTC())
		for _, p := range plans {
			if p.StepCount() > MaxStrategyDepth {
				t.Errorf("goal %s strategy %s has %d steps, exceeds max %d",
					gt, p.StrategyType, p.StepCount(), MaxStrategyDepth)
			}
		}
	}
}

// --- Test 20: Risk accumulation increases with steps ---

func TestScore_RiskAccumulation_IncreasesWithSteps(t *testing.T) {
	oneStep := StrategyPlan{
		ID:           uuid.New(),
		StrategyType: StrategyDirectRetry,
		Steps:        []StrategyStep{{Order: 1, ActionType: "retry_job"}},
	}
	twoStep := StrategyPlan{
		ID:           uuid.New(),
		StrategyType: StrategyObserveThenRetry,
		Steps: []StrategyStep{
			{Order: 1, ActionType: "log_recommendation"},
			{Order: 2, ActionType: "retry_job"},
		},
	}

	input := ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.7, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"retry_job": 0.8, "log_recommendation": 0.6},
		StabilityMode:   "normal",
	}

	plans := []StrategyPlan{oneStep, twoStep}
	scored := ScoreStrategies(plans, input)

	if scored[0].RiskScore >= scored[1].RiskScore {
		t.Errorf("two-step should have higher risk (%f) than one-step (%f)",
			scored[1].RiskScore, scored[0].RiskScore)
	}

	// One-step risk should be 0 (stepCount-1 = 0).
	if scored[0].RiskScore != 0 {
		t.Errorf("one-step risk should be 0, got %f", scored[0].RiskScore)
	}

	// Two-step risk should be RiskPerStep.
	expectedRisk := RiskPerStep
	if scored[1].RiskScore != expectedRisk {
		t.Errorf("two-step risk should be %f, got %f", expectedRisk, scored[1].RiskScore)
	}
}
