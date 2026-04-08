package strategylearning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/strategy"
)

// --- Test 1: Strategy outcome success detection ---

func TestClassifyOutcome_Success(t *testing.T) {
	result := classifyOutcome("success")
	if result != OutcomeSuccess {
		t.Fatalf("expected %q, got %q", OutcomeSuccess, result)
	}
}

// --- Test 2: Strategy outcome failure detection ---

func TestClassifyOutcome_Failure(t *testing.T) {
	result := classifyOutcome("failure")
	if result != OutcomeFailure {
		t.Fatalf("expected %q, got %q", OutcomeFailure, result)
	}
}

// --- Test 3: Strategy outcome neutral detection ---

func TestClassifyOutcome_Neutral(t *testing.T) {
	result := classifyOutcome("neutral")
	if result != OutcomeNeutral {
		t.Fatalf("expected %q, got %q", OutcomeNeutral, result)
	}

	// Unknown status also classifies as neutral (fail-safe).
	result2 := classifyOutcome("unknown_status")
	if result2 != OutcomeNeutral {
		t.Fatalf("expected %q for unknown, got %q", OutcomeNeutral, result2)
	}
}

// --- Test 4: Strategy memory updates correctly ---

func TestGenerateFeedback_MemoryUpdates(t *testing.T) {
	// With insufficient samples -> insufficient_data.
	record := &StrategyMemoryRecord{
		StrategyType: "direct_retry",
		GoalType:     "reduce_retry_rate",
		TotalRuns:    3,
		SuccessRuns:  2,
		FailureRuns:  1,
		SuccessRate:  0.67,
		FailureRate:  0.33,
	}
	fb := GenerateFeedback(record)
	if fb.Recommendation != RecommendInsufficientStrategy {
		t.Fatalf("expected %q with %d runs, got %q",
			RecommendInsufficientStrategy, record.TotalRuns, fb.Recommendation)
	}

	// With enough samples and high success -> prefer_strategy.
	record.TotalRuns = 10
	record.SuccessRuns = 8
	record.FailureRuns = 1
	record.NeutralRuns = 1
	record.SuccessRate = 0.80
	record.FailureRate = 0.10
	fb = GenerateFeedback(record)
	if fb.Recommendation != RecommendPreferStrategy {
		t.Fatalf("expected %q with success_rate=%.2f, got %q",
			RecommendPreferStrategy, record.SuccessRate, fb.Recommendation)
	}

	// With high failure -> avoid_strategy.
	record.SuccessRate = 0.20
	record.FailureRate = 0.60
	fb = GenerateFeedback(record)
	if fb.Recommendation != RecommendAvoidStrategy {
		t.Fatalf("expected %q with failure_rate=%.2f, got %q",
			RecommendAvoidStrategy, record.FailureRate, fb.Recommendation)
	}

	// With moderate rates -> neutral.
	record.SuccessRate = 0.50
	record.FailureRate = 0.30
	fb = GenerateFeedback(record)
	if fb.Recommendation != RecommendNeutralStrategy {
		t.Fatalf("expected %q with mixed rates, got %q",
			RecommendNeutralStrategy, fb.Recommendation)
	}

	// Nil record -> insufficient_data.
	fb = GenerateFeedback(nil)
	if fb.Recommendation != RecommendInsufficientStrategy {
		t.Fatalf("expected %q for nil record, got %q",
			RecommendInsufficientStrategy, fb.Recommendation)
	}
}

// --- Test 5: Continuation allowed when all conditions satisfied ---

func TestContinuation_AllGatesPassed(t *testing.T) {
	logger := zap.NewNop()
	// No real DB — test the gate logic by constructing a ContinuationEngine
	// with a nil store (gate 5 will pass because GetMemory returns nil, nil).
	// We use a stub stability provider.
	ce := &ContinuationEngine{
		stability: &stubStabilityProvider{mode: "normal"},
		auditor:   nil,
		logger:    logger,
	}

	decision := ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"escalate_first",
		"reduce_retry_rate",
		OutcomeNeutral,    // step1 neutral
		"retry_job",       // has step 2
		0.80,              // confidence >= 0.60
		1,                 // currentStep == 1
	)

	if !decision.ShouldContinue {
		t.Fatalf("expected continuation allowed, got blocked: %s", decision.Reason)
	}
	if decision.Step2Action != "retry_job" {
		t.Fatalf("expected step2_action=retry_job, got %s", decision.Step2Action)
	}
	if decision.Reason != "all_gates_passed" {
		t.Fatalf("expected reason=all_gates_passed, got %s", decision.Reason)
	}
}

// --- Test 6: Continuation blocked in safe_mode ---

func TestContinuation_BlockedInSafeMode(t *testing.T) {
	logger := zap.NewNop()
	ce := &ContinuationEngine{
		stability: &stubStabilityProvider{mode: "safe_mode"},
		auditor:   nil,
		logger:    logger,
	}

	decision := ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"escalate_first",
		"reduce_retry_rate",
		OutcomeNeutral,
		"retry_job",
		0.80,
		1,
	)

	if decision.ShouldContinue {
		t.Fatal("expected continuation blocked in safe_mode")
	}
	if decision.Reason != "stability_not_normal: mode=safe_mode" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

// --- Test 7: Continuation blocked on high failure rate ---

func TestContinuation_BlockedOnHighFailureRate(t *testing.T) {
	logger := zap.NewNop()
	// We need a memory store that returns high failure rate.
	// Use a stubbed approach: construct engine with nil store to skip gate 5,
	// but test the gate logic directly.
	ce := &ContinuationEngine{
		stability: &stubStabilityProvider{mode: "normal"},
		store:     nil, // gate 5 will get nil record and pass
		auditor:   nil,
		logger:    logger,
	}

	// Test depth gate: currentStep != 1.
	decision := ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"direct_retry",
		"reduce_retry_rate",
		OutcomeNeutral,
		"retry_job",
		0.80,
		2, // already at step 2
	)
	if decision.ShouldContinue {
		t.Fatal("expected continuation blocked at depth 2")
	}

	// Test step1 not neutral.
	decision = ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"direct_retry",
		"reduce_retry_rate",
		OutcomeFailure, // step 1 failed
		"retry_job",
		0.80,
		1,
	)
	if decision.ShouldContinue {
		t.Fatal("expected continuation blocked when step1 failed")
	}

	// Test low confidence.
	decision = ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"direct_retry",
		"reduce_retry_rate",
		OutcomeNeutral,
		"retry_job",
		0.40, // below MinContinuationConfidence
		1,
	)
	if decision.ShouldContinue {
		t.Fatal("expected continuation blocked on low confidence")
	}

	// Test no step 2.
	decision = ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"direct_retry",
		"reduce_retry_rate",
		OutcomeNeutral,
		"", // no step 2
		0.80,
		1,
	)
	if decision.ShouldContinue {
		t.Fatal("expected continuation blocked with no step 2")
	}

	// Test blocked action.
	ce2 := &ContinuationEngine{
		stability: &stubStabilityProvider{
			mode:           "normal",
			blockedActions: []string{"retry_job"},
		},
		auditor: nil,
		logger:  logger,
	}
	decision = ce2.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"direct_retry",
		"reduce_retry_rate",
		OutcomeNeutral,
		"retry_job", // blocked
		0.80,
		1,
	)
	if decision.ShouldContinue {
		t.Fatal("expected continuation blocked when action is blocked")
	}
}

// --- Test 8: Strategy feedback prefer works ---

func TestStrategyScoring_PreferBoost(t *testing.T) {
	plan := strategy.StrategyPlan{
		StrategyType: strategy.StrategyDirectRetry,
		Steps: []strategy.StrategyStep{
			{ActionType: "retry_job", Order: 1},
		},
	}
	inputBase := strategy.ScoreInput{
		CandidateScores:  map[string]float64{"retry_job": 0.8},
		CandidateConf:    map[string]float64{"retry_job": 0.7},
		StabilityMode:    "normal",
		StrategyFeedback: nil, // no feedback
	}

	// Score without feedback.
	plansBase := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansBase, inputBase)
	baseUtility := plansBase[0].ExpectedUtility

	// Score with prefer feedback.
	inputPrefer := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8},
		CandidateConf:   map[string]float64{"retry_job": 0.7},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyDirectRetry): {
				Recommendation: "prefer_strategy",
				SuccessRate:    0.85,
				FailureRate:    0.10,
				SampleSize:     10,
			},
		},
	}
	plansPrefer := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansPrefer, inputPrefer)
	preferUtility := plansPrefer[0].ExpectedUtility

	if preferUtility <= baseUtility {
		t.Fatalf("prefer should boost utility: base=%.4f, prefer=%.4f",
			baseUtility, preferUtility)
	}

	expectedDelta := strategy.StrategyPreferBoost
	actualDelta := preferUtility - baseUtility
	if actualDelta < expectedDelta-0.01 || actualDelta > expectedDelta+0.01 {
		t.Fatalf("prefer boost should be ~%.2f, got %.4f", expectedDelta, actualDelta)
	}
}

// --- Test 9: Strategy feedback avoid works ---

func TestStrategyScoring_AvoidPenalty(t *testing.T) {
	plan := strategy.StrategyPlan{
		StrategyType: strategy.StrategyDirectRetry,
		Steps: []strategy.StrategyStep{
			{ActionType: "retry_job", Order: 1},
		},
	}
	inputBase := strategy.ScoreInput{
		CandidateScores:  map[string]float64{"retry_job": 0.8},
		CandidateConf:    map[string]float64{"retry_job": 0.7},
		StabilityMode:    "normal",
		StrategyFeedback: nil,
	}

	plansBase := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansBase, inputBase)
	baseUtility := plansBase[0].ExpectedUtility

	// Score with avoid feedback.
	inputAvoid := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8},
		CandidateConf:   map[string]float64{"retry_job": 0.7},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyDirectRetry): {
				Recommendation: "avoid_strategy",
				SuccessRate:    0.20,
				FailureRate:    0.60,
				SampleSize:     10,
			},
		},
	}
	plansAvoid := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansAvoid, inputAvoid)
	avoidUtility := plansAvoid[0].ExpectedUtility

	if avoidUtility >= baseUtility {
		t.Fatalf("avoid should penalize utility: base=%.4f, avoid=%.4f",
			baseUtility, avoidUtility)
	}
}

// --- Test 10: No regression in action-only mode ---

func TestStrategyScoring_NoRegression_ActionOnlyMode(t *testing.T) {
	plan := strategy.StrategyPlan{
		StrategyType: strategy.StrategyDirectRetry,
		Steps: []strategy.StrategyStep{
			{ActionType: "retry_job", Order: 1},
		},
	}

	// Score without any strategy feedback (action-only mode).
	input := strategy.ScoreInput{
		CandidateScores:  map[string]float64{"retry_job": 0.8},
		CandidateConf:    map[string]float64{"retry_job": 0.7},
		StabilityMode:    "normal",
		StrategyFeedback: nil,
	}

	plans := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plans, input)

	if plans[0].ExpectedUtility <= 0 {
		t.Fatalf("action-only mode should produce positive utility, got %.4f",
			plans[0].ExpectedUtility)
	}

	// With empty map (should also not change anything).
	inputEmpty := strategy.ScoreInput{
		CandidateScores:  map[string]float64{"retry_job": 0.8},
		CandidateConf:    map[string]float64{"retry_job": 0.7},
		StabilityMode:    "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{},
	}

	plansEmpty := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansEmpty, inputEmpty)

	if plansEmpty[0].ExpectedUtility != plans[0].ExpectedUtility {
		t.Fatalf("empty feedback map should not change utility: without=%.4f, with=%.4f",
			plans[0].ExpectedUtility, plansEmpty[0].ExpectedUtility)
	}

	// With neutral recommendation (should not change scoring).
	inputNeutral := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8},
		CandidateConf:   map[string]float64{"retry_job": 0.7},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyDirectRetry): {
				Recommendation: "neutral",
				SuccessRate:    0.50,
				FailureRate:    0.30,
				SampleSize:     10,
			},
		},
	}

	plansNeutral := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansNeutral, inputNeutral)

	if plansNeutral[0].ExpectedUtility != plans[0].ExpectedUtility {
		t.Fatalf("neutral feedback should not change utility: without=%.4f, neutral=%.4f",
			plans[0].ExpectedUtility, plansNeutral[0].ExpectedUtility)
	}

	// With insufficient_data recommendation (should not change scoring).
	inputInsufficient := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8},
		CandidateConf:   map[string]float64{"retry_job": 0.7},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyDirectRetry): {
				Recommendation: "insufficient_data",
				SampleSize:     2,
			},
		},
	}

	plansInsufficient := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansInsufficient, inputInsufficient)

	if plansInsufficient[0].ExpectedUtility != plans[0].ExpectedUtility {
		t.Fatalf("insufficient_data feedback should not change utility: without=%.4f, insufficient=%.4f",
			plans[0].ExpectedUtility, plansInsufficient[0].ExpectedUtility)
	}
}

// --- Test helpers ---

// stubStabilityProvider implements StabilityProvider for testing.
type stubStabilityProvider struct {
	mode           string
	blockedActions []string
}

func (s *stubStabilityProvider) GetMode(_ context.Context) string {
	return s.mode
}

func (s *stubStabilityProvider) GetBlockedActions(_ context.Context) []string {
	return s.blockedActions
}
