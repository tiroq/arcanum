package strategylearning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/strategy"
)

// --- Test 1: Step1 success, Step2 neutral → no gain → continuation discouraged ---

func TestContinuationGain_Step1SuccessStep2Neutral(t *testing.T) {
	// When step1 succeeded and step2 is neutral, there is no continuation gain.
	record := &StrategyMemoryRecord{
		StrategyType:         "escalate_first",
		GoalType:             "reduce_retry_rate",
		TotalRuns:            10,
		ContinuationUsedRuns: 7,
		ContinuationGainRuns: 1, // only 1 gain out of 7 → 0.14 rate
		ContinuationGainRate: 0.14,
		SuccessRate:          0.60,
		FailureRate:          0.20,
	}

	fb := GenerateFeedback(record)
	if !fb.AvoidContinuation {
		t.Fatal("expected AvoidContinuation=true when gain rate is 0.14")
	}
	if fb.PreferContinuation {
		t.Fatal("expected PreferContinuation=false when gain rate is 0.14")
	}
}

// --- Test 2: Step1 neutral, Step2 success → gain → continuation preferred ---

func TestContinuationGain_Step1NeutralStep2Success(t *testing.T) {
	record := &StrategyMemoryRecord{
		StrategyType:         "escalate_first",
		GoalType:             "reduce_retry_rate",
		TotalRuns:            10,
		ContinuationUsedRuns: 8,
		ContinuationGainRuns: 6, // 6 out of 8 → 0.75 rate
		ContinuationGainRate: 0.75,
		SuccessRate:          0.60,
		FailureRate:          0.20,
	}

	fb := GenerateFeedback(record)
	if !fb.PreferContinuation {
		t.Fatal("expected PreferContinuation=true when gain rate is 0.75")
	}
	if fb.AvoidContinuation {
		t.Fatal("expected AvoidContinuation=false when gain rate is 0.75")
	}
}

// --- Test 3: Step2 always neutral → system learns to stop continuation ---

func TestContinuationGain_AlwaysNeutral_LearnsToStop(t *testing.T) {
	record := &StrategyMemoryRecord{
		StrategyType:         "direct_retry",
		GoalType:             "reduce_retry_rate",
		TotalRuns:            15,
		ContinuationUsedRuns: 10,
		ContinuationGainRuns: 0, // zero gain
		ContinuationGainRate: 0.0,
		SuccessRate:          0.40,
		FailureRate:          0.30,
	}

	fb := GenerateFeedback(record)
	if !fb.AvoidContinuation {
		t.Fatal("expected AvoidContinuation=true when gain rate is 0.0")
	}

	// Also verify gate 8 blocks the continuation engine.
	logger := zap.NewNop()
	ce := &ContinuationEngine{
		stability: &stubContinuationStabilityProvider{mode: "normal"},
		store:     &stubMemoryReader{record: record},
		auditor:   nil,
		logger:    logger,
	}

	decision := ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"direct_retry",
		"reduce_retry_rate",
		OutcomeNeutral,
		"retry_job",
		0.80,
		1,
	)
	if decision.ShouldContinue {
		t.Fatalf("expected continuation blocked by low gain, got: %s", decision.Reason)
	}
	if decision.Reason != "low_continuation_gain: 0.00" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

// --- Test 4: Mixed outcomes → correct rate calculation ---

func TestContinuationGain_MixedOutcomes(t *testing.T) {
	record := &StrategyMemoryRecord{
		StrategyType:         "escalate_first",
		GoalType:             "reduce_retry_rate",
		TotalRuns:            20,
		ContinuationUsedRuns: 10,
		ContinuationGainRuns: 4, // 4/10 = 0.40
		ContinuationGainRate: 0.40,
		SuccessRate:          0.50,
		FailureRate:          0.20,
	}

	fb := GenerateFeedback(record)
	// 0.40 is between 0.30 and 0.60 → neutral on continuation.
	if fb.PreferContinuation {
		t.Fatal("expected PreferContinuation=false at 0.40 gain rate (neutral zone)")
	}
	if fb.AvoidContinuation {
		t.Fatal("expected AvoidContinuation=false at 0.40 gain rate (neutral zone)")
	}
}

// --- Test 5: Insufficient data (<5 continuation uses) → no gating ---

func TestContinuationGain_InsufficientData(t *testing.T) {
	record := &StrategyMemoryRecord{
		StrategyType:         "direct_retry",
		GoalType:             "reduce_retry_rate",
		TotalRuns:            10,
		ContinuationUsedRuns: 3, // below MinContinuationSampleSize (5)
		ContinuationGainRuns: 0,
		ContinuationGainRate: 0.0,
		SuccessRate:          0.50,
		FailureRate:          0.25,
	}

	fb := GenerateFeedback(record)
	if fb.AvoidContinuation {
		t.Fatal("expected AvoidContinuation=false with only 3 continuation samples")
	}
	if fb.PreferContinuation {
		t.Fatal("expected PreferContinuation=false with only 3 continuation samples")
	}
}

// --- Test 6: Backward compatibility → no regression ---

func TestContinuationGain_BackwardCompatibility(t *testing.T) {
	// Record with zero continuation fields (pre-18.1 data).
	record := &StrategyMemoryRecord{
		StrategyType:         "direct_retry",
		GoalType:             "reduce_retry_rate",
		TotalRuns:            10,
		SuccessRuns:          8,
		FailureRuns:          1,
		NeutralRuns:          1,
		SuccessRate:          0.80,
		FailureRate:          0.10,
		ContinuationUsedRuns: 0,
		ContinuationGainRuns: 0,
		ContinuationGainRate: 0.0,
	}

	fb := GenerateFeedback(record)
	// Strategy-level feedback should still work.
	if fb.Recommendation != RecommendPreferStrategy {
		t.Fatalf("expected %q, got %q", RecommendPreferStrategy, fb.Recommendation)
	}
	// Continuation signals should be neutral (insufficient data).
	if fb.PreferContinuation {
		t.Fatal("expected PreferContinuation=false with zero continuation data")
	}
	if fb.AvoidContinuation {
		t.Fatal("expected AvoidContinuation=false with zero continuation data")
	}

	// Continuation engine with nil store should still allow (fail-open).
	logger := zap.NewNop()
	ce := &ContinuationEngine{
		stability: &stubContinuationStabilityProvider{mode: "normal"},
		store:     nil,
		auditor:   nil,
		logger:    logger,
	}
	decision := ce.EvaluateContinuation(
		context.Background(),
		uuid.New().String(),
		"direct_retry",
		"reduce_retry_rate",
		OutcomeNeutral,
		"retry_job",
		0.80,
		1,
	)
	if !decision.ShouldContinue {
		t.Fatalf("expected continuation allowed with nil store, got: %s", decision.Reason)
	}
}

// --- Test 7: Division by zero safe ---

func TestContinuationGain_DivisionByZeroSafe(t *testing.T) {
	// Record with ContinuationUsedRuns = 0 should not divide by zero.
	record := &StrategyMemoryRecord{
		StrategyType:         "direct_retry",
		GoalType:             "reduce_retry_rate",
		TotalRuns:            10,
		ContinuationUsedRuns: 0,
		ContinuationGainRuns: 0,
		ContinuationGainRate: 0.0,
		SuccessRate:          0.50,
		FailureRate:          0.20,
	}

	// Should not panic.
	fb := GenerateFeedback(record)
	if fb.PreferContinuation || fb.AvoidContinuation {
		t.Fatal("expected no continuation signal with 0 continuation runs")
	}
}

// --- Test 8: Planner reacts to continuation signals ---

func TestStrategyScoring_ContinuationPreferBoost(t *testing.T) {
	// Multi-step plan (2 steps) → continuation signals apply.
	plan := strategy.StrategyPlan{
		StrategyType: strategy.StrategyObserveThenRetry,
		Steps: []strategy.StrategyStep{
			{ActionType: "retry_job", Order: 1},
			{ActionType: "trigger_resync", Order: 2},
		},
	}

	// Score without continuation signals.
	inputBase := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8, "trigger_resync": 0.7},
		CandidateConf:   map[string]float64{"retry_job": 0.7, "trigger_resync": 0.6},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyObserveThenRetry): {
				Recommendation: "neutral",
			},
		},
	}
	plansBase := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansBase, inputBase)
	baseUtility := plansBase[0].ExpectedUtility

	// Score with prefer continuation.
	inputPrefer := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8, "trigger_resync": 0.7},
		CandidateConf:   map[string]float64{"retry_job": 0.7, "trigger_resync": 0.6},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyObserveThenRetry): {
				Recommendation:     "neutral",
				PreferContinuation: true,
			},
		},
	}
	plansPrefer := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansPrefer, inputPrefer)
	preferUtility := plansPrefer[0].ExpectedUtility

	if preferUtility <= baseUtility {
		t.Fatalf("prefer continuation should boost multi-step utility: base=%.4f, prefer=%.4f",
			baseUtility, preferUtility)
	}

	// Score with avoid continuation.
	inputAvoid := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8, "trigger_resync": 0.7},
		CandidateConf:   map[string]float64{"retry_job": 0.7, "trigger_resync": 0.6},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyObserveThenRetry): {
				Recommendation:    "neutral",
				AvoidContinuation: true,
			},
		},
	}
	plansAvoid := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansAvoid, inputAvoid)
	avoidUtility := plansAvoid[0].ExpectedUtility

	if avoidUtility >= baseUtility {
		t.Fatalf("avoid continuation should penalize multi-step utility: base=%.4f, avoid=%.4f",
			baseUtility, avoidUtility)
	}
}

func TestStrategyScoring_ContinuationSignals_SingleStep_NoEffect(t *testing.T) {
	// Single-step plan → continuation signals should NOT apply.
	plan := strategy.StrategyPlan{
		StrategyType: strategy.StrategyDirectRetry,
		Steps: []strategy.StrategyStep{
			{ActionType: "retry_job", Order: 1},
		},
	}

	inputBase := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8},
		CandidateConf:   map[string]float64{"retry_job": 0.7},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyDirectRetry): {
				Recommendation: "neutral",
			},
		},
	}
	plansBase := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansBase, inputBase)
	baseUtility := plansBase[0].ExpectedUtility

	inputWithCont := strategy.ScoreInput{
		CandidateScores: map[string]float64{"retry_job": 0.8},
		CandidateConf:   map[string]float64{"retry_job": 0.7},
		StabilityMode:   "normal",
		StrategyFeedback: map[string]strategy.StrategyFeedbackSignal{
			string(strategy.StrategyDirectRetry): {
				Recommendation:     "neutral",
				PreferContinuation: true,
			},
		},
	}
	plansWithCont := []strategy.StrategyPlan{plan}
	strategy.ScoreStrategies(plansWithCont, inputWithCont)

	if plansWithCont[0].ExpectedUtility != baseUtility {
		t.Fatalf("continuation signals should not affect single-step: base=%.4f, with=%.4f",
			baseUtility, plansWithCont[0].ExpectedUtility)
	}
}

// --- Stubs for continuation engine tests ---

// stubMemoryReader implements MemoryReader for testing.
type stubMemoryReader struct {
	record *StrategyMemoryRecord
}

func (s *stubMemoryReader) GetMemory(_ context.Context, _, _ string) (*StrategyMemoryRecord, error) {
	return s.record, nil
}

// stubContinuationStabilityProvider implements StabilityProvider for testing.
type stubContinuationStabilityProvider struct {
	mode           string
	blockedActions []string
}

func (s *stubContinuationStabilityProvider) GetMode(_ context.Context) string {
	return s.mode
}

func (s *stubContinuationStabilityProvider) GetBlockedActions(_ context.Context) []string {
	return s.blockedActions
}
