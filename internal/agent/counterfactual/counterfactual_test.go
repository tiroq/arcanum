package counterfactual

import (
	"math"
	"testing"
)

// --- Simulation Tests ---

func TestSimulateTopKPaths_NilSignals(t *testing.T) {
	result := SimulateTopKPaths("d1", "goal", map[string]float64{"a": 0.8}, nil, nil)

	if len(result.Predictions) != 0 {
		t.Errorf("expected 0 predictions with nil signals, got %d", len(result.Predictions))
	}
}

func TestSimulateTopKPaths_EmptyScores(t *testing.T) {
	signals := &SimulationSignals{}
	result := SimulateTopKPaths("d1", "goal", map[string]float64{}, nil, signals)

	if len(result.Predictions) != 0 {
		t.Errorf("expected 0 predictions with empty scores, got %d", len(result.Predictions))
	}
}

func TestSimulateTopKPaths_Deterministic(t *testing.T) {
	scores := map[string]float64{
		"retry_job":          0.8,
		"log_recommendation": 0.6,
		"noop":               0.01,
	}
	signals := &SimulationSignals{
		PathFeedback:        map[string]string{"retry_job": "prefer_path"},
		ComparativeFeedback: map[string]string{"retry_job": "prefer_path"},
	}

	r1 := SimulateTopKPaths("d1", "goal", scores, nil, signals)
	r2 := SimulateTopKPaths("d1", "goal", scores, nil, signals)

	if len(r1.Predictions) != len(r2.Predictions) {
		t.Fatalf("predictions should be deterministic")
	}
	for i := range r1.Predictions {
		if r1.Predictions[i].PathSignature != r2.Predictions[i].PathSignature {
			t.Errorf("prediction %d signature mismatch", i)
		}
		if r1.Predictions[i].ExpectedValue != r2.Predictions[i].ExpectedValue {
			t.Errorf("prediction %d expected value mismatch", i)
		}
	}
}

func TestSimulateTopKPaths_BoundedToMaxK(t *testing.T) {
	scores := map[string]float64{
		"a": 0.9,
		"b": 0.8,
		"c": 0.7,
		"d": 0.6,
		"e": 0.5,
	}
	signals := &SimulationSignals{}

	result := SimulateTopKPaths("d1", "goal", scores, nil, signals)

	if len(result.Predictions) > MaxSimulatedPaths {
		t.Errorf("expected at most %d predictions, got %d", MaxSimulatedPaths, len(result.Predictions))
	}
}

func TestSimulateTopKPaths_TopPathsSelected(t *testing.T) {
	scores := map[string]float64{
		"a": 0.9,
		"b": 0.1,
		"c": 0.7,
		"d": 0.6,
		"e": 0.5,
	}
	signals := &SimulationSignals{}

	result := SimulateTopKPaths("d1", "goal", scores, nil, signals)

	// Top 3 should be a, c, d (by score).
	expected := map[string]bool{"a": true, "c": true, "d": true}
	for _, pred := range result.Predictions {
		if !expected[pred.PathSignature] {
			t.Errorf("unexpected path in top-K: %s", pred.PathSignature)
		}
	}
}

func TestSimulateTopKPaths_PreferPathIncreasesValue(t *testing.T) {
	scores := map[string]float64{"retry_job": 0.5}
	signals := &SimulationSignals{
		PathFeedback: map[string]string{"retry_job": "prefer_path"},
	}

	result := SimulateTopKPaths("d1", "goal", scores, nil, signals)

	if len(result.Predictions) != 1 {
		t.Fatalf("expected 1 prediction, got %d", len(result.Predictions))
	}
	// prefer_path → value 0.8, which should be > base 0.5
	if result.Predictions[0].ExpectedValue <= 0.5 {
		t.Errorf("prefer_path should increase expected value, got %f", result.Predictions[0].ExpectedValue)
	}
}

func TestSimulateTopKPaths_AvoidPathDecreasesValue(t *testing.T) {
	scores := map[string]float64{"retry_job": 0.5}
	signals := &SimulationSignals{
		PathFeedback: map[string]string{"retry_job": "avoid_path"},
	}

	result := SimulateTopKPaths("d1", "goal", scores, nil, signals)

	if len(result.Predictions) != 1 {
		t.Fatalf("expected 1 prediction, got %d", len(result.Predictions))
	}
	// avoid_path → value 0.2, which should be < base 0.5
	if result.Predictions[0].ExpectedValue >= 0.5 {
		t.Errorf("avoid_path should decrease expected value, got %f", result.Predictions[0].ExpectedValue)
	}
}

func TestSimulateTopKPaths_PathLengthIncreasesRisk(t *testing.T) {
	scores := map[string]float64{"a>b>c": 0.5}
	lengths := map[string]int{"a>b>c": 3}
	signals := &SimulationSignals{}

	result := SimulateTopKPaths("d1", "goal", scores, lengths, signals)

	if len(result.Predictions) != 1 {
		t.Fatalf("expected 1 prediction, got %d", len(result.Predictions))
	}
	// 3-node path should have higher risk than 1-node path.
	if result.Predictions[0].ExpectedRisk <= 0 {
		t.Errorf("long path should have positive risk, got %f", result.Predictions[0].ExpectedRisk)
	}
}

func TestSimulateTopKPaths_TransitionSignalContributes(t *testing.T) {
	scores := map[string]float64{"a>b": 0.5}
	signals := &SimulationSignals{
		TransitionFeedback: map[string]string{"a->b": "prefer_transition"},
	}

	result := SimulateTopKPaths("d1", "goal", scores, nil, signals)

	if len(result.Predictions) != 1 {
		t.Fatalf("expected 1 prediction, got %d", len(result.Predictions))
	}
	if result.Predictions[0].SourceBreakdown["transition_learning"] <= 0 {
		t.Error("transition learning signal should contribute to prediction")
	}
}

// --- Score Adjustment Tests ---

func TestAdjustScoresWithPredictions_NilPredictions(t *testing.T) {
	scores := map[string]float64{"a": 0.8}
	adjusted := AdjustScoresWithPredictions(scores, nil)

	if adjusted["a"] != 0.8 {
		t.Errorf("nil predictions should return original scores, got %f", adjusted["a"])
	}
}

func TestAdjustScoresWithPredictions_EmptyPredictions(t *testing.T) {
	scores := map[string]float64{"a": 0.8}
	adjusted := AdjustScoresWithPredictions(scores, []PathPrediction{})

	if adjusted["a"] != 0.8 {
		t.Errorf("empty predictions should return original scores, got %f", adjusted["a"])
	}
}

func TestAdjustScoresWithPredictions_HighPredictionIncreasesScore(t *testing.T) {
	scores := map[string]float64{"a": 0.5}
	predictions := []PathPrediction{
		{PathSignature: "a", ExpectedValue: 0.9, Confidence: 0.8},
	}

	adjusted := AdjustScoresWithPredictions(scores, predictions)

	// AdjustedScore = 0.5 + (0.9 - 0.5) * 0.2 = 0.5 + 0.08 = 0.58
	expected := 0.58
	if math.Abs(adjusted["a"]-expected) > 0.001 {
		t.Errorf("expected adjusted score ~%f, got %f", expected, adjusted["a"])
	}
}

func TestAdjustScoresWithPredictions_LowPredictionDecreasesScore(t *testing.T) {
	scores := map[string]float64{"a": 0.8}
	predictions := []PathPrediction{
		{PathSignature: "a", ExpectedValue: 0.2, Confidence: 0.8},
	}

	adjusted := AdjustScoresWithPredictions(scores, predictions)

	// AdjustedScore = 0.8 + (0.2 - 0.8) * 0.2 = 0.8 - 0.12 = 0.68
	expected := 0.68
	if math.Abs(adjusted["a"]-expected) > 0.001 {
		t.Errorf("expected adjusted score ~%f, got %f", expected, adjusted["a"])
	}
}

func TestAdjustScoresWithPredictions_BoundedInfluence(t *testing.T) {
	scores := map[string]float64{"a": 0.5}
	predictions := []PathPrediction{
		{PathSignature: "a", ExpectedValue: 1.0, Confidence: 0.8},
	}

	adjusted := AdjustScoresWithPredictions(scores, predictions)

	// Max delta = (1.0 - 0.5) * 0.2 = 0.1 → adjusted = 0.6
	// Should NOT dominate the score.
	maxDelta := 0.5 * PredictionWeight
	if adjusted["a"]-0.5 > maxDelta+0.001 {
		t.Errorf("prediction should not dominate score: delta %f > max %f", adjusted["a"]-0.5, maxDelta)
	}
}

func TestAdjustScoresWithPredictions_LowConfidenceIgnored(t *testing.T) {
	scores := map[string]float64{"a": 0.5}
	predictions := []PathPrediction{
		{PathSignature: "a", ExpectedValue: 0.9, Confidence: 0.0},
	}

	adjusted := AdjustScoresWithPredictions(scores, predictions)

	// Low confidence → no adjustment.
	if adjusted["a"] != 0.5 {
		t.Errorf("low confidence prediction should not adjust score, got %f", adjusted["a"])
	}
}

func TestAdjustScoresWithPredictions_UnpredictedPathUnchanged(t *testing.T) {
	scores := map[string]float64{"a": 0.5, "b": 0.7}
	predictions := []PathPrediction{
		{PathSignature: "a", ExpectedValue: 0.9, Confidence: 0.8},
	}

	adjusted := AdjustScoresWithPredictions(scores, predictions)

	if adjusted["b"] != 0.7 {
		t.Errorf("path without prediction should be unchanged, got %f", adjusted["b"])
	}
}

func TestAdjustScoresWithPredictions_ClampedToZeroOne(t *testing.T) {
	scores := map[string]float64{"a": 0.99}
	predictions := []PathPrediction{
		{PathSignature: "a", ExpectedValue: 1.0, Confidence: 0.8},
	}

	adjusted := AdjustScoresWithPredictions(scores, predictions)

	if adjusted["a"] > 1.0 || adjusted["a"] < 0.0 {
		t.Errorf("adjusted score should be clamped to [0,1], got %f", adjusted["a"])
	}
}

// --- selectTopK Tests ---

func TestSelectTopK_SortsDescending(t *testing.T) {
	scores := map[string]float64{
		"c": 0.3,
		"a": 0.9,
		"b": 0.5,
	}

	result := selectTopK(scores, 3)

	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("expected [a, b, c], got %v", result)
	}
}

func TestSelectTopK_TieBreakAlphabetical(t *testing.T) {
	scores := map[string]float64{
		"b": 0.5,
		"a": 0.5,
	}

	result := selectTopK(scores, 2)

	if result[0] != "a" || result[1] != "b" {
		t.Errorf("tie should break alphabetically, got %v", result)
	}
}

func TestSelectTopK_LimitsToK(t *testing.T) {
	scores := map[string]float64{
		"a": 0.9,
		"b": 0.8,
		"c": 0.7,
		"d": 0.6,
	}

	result := selectTopK(scores, 2)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" {
		t.Errorf("expected [a, b], got %v", result)
	}
}

// --- OutcomeToValue Tests ---

func TestOutcomeToValue_Success(t *testing.T) {
	v := OutcomeToValue("success")
	if v != OutcomeValueSuccess {
		t.Errorf("expected %f, got %f", OutcomeValueSuccess, v)
	}
}

func TestOutcomeToValue_Failure(t *testing.T) {
	v := OutcomeToValue("failure")
	if v != OutcomeValueFailure {
		t.Errorf("expected %f, got %f", OutcomeValueFailure, v)
	}
}

func TestOutcomeToValue_Neutral(t *testing.T) {
	v := OutcomeToValue("neutral")
	if v != OutcomeValueNeutral {
		t.Errorf("expected %f, got %f", OutcomeValueNeutral, v)
	}
}

func TestOutcomeToValue_Unknown(t *testing.T) {
	v := OutcomeToValue("unknown")
	if v != OutcomeValueNeutral {
		t.Errorf("expected neutral for unknown, got %f", v)
	}
}

// --- recommendationToValue Tests ---

func TestRecommendationToValue_Prefer(t *testing.T) {
	v := recommendationToValue("prefer_path")
	if v != 0.8 {
		t.Errorf("expected 0.8, got %f", v)
	}
}

func TestRecommendationToValue_Avoid(t *testing.T) {
	v := recommendationToValue("avoid_path")
	if v != 0.2 {
		t.Errorf("expected 0.2, got %f", v)
	}
}

func TestRecommendationToValue_Underexplored(t *testing.T) {
	v := recommendationToValue("underexplored_path")
	if v != 0.5 {
		t.Errorf("expected 0.5, got %f", v)
	}
}

func TestRecommendationToValue_Neutral(t *testing.T) {
	v := recommendationToValue("neutral")
	if v != 0.5 {
		t.Errorf("expected 0.5, got %f", v)
	}
}

// --- splitPathSignature Tests ---

func TestSplitPathSignature_Single(t *testing.T) {
	result := splitPathSignature("retry_job")
	if len(result) != 1 || result[0] != "retry_job" {
		t.Errorf("expected [retry_job], got %v", result)
	}
}

func TestSplitPathSignature_Multi(t *testing.T) {
	result := splitPathSignature("a>b>c")
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("expected [a, b, c], got %v", result)
	}
}

func TestSplitPathSignature_Empty(t *testing.T) {
	result := splitPathSignature("")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// --- aggregateTransitionSignal Tests ---

func TestAggregateTransitionSignal_SingleTransition(t *testing.T) {
	tf := map[string]string{"a->b": "prefer_transition"}
	val := aggregateTransitionSignal("a>b", tf)

	if val != 0.8 {
		t.Errorf("expected 0.8 for prefer_transition, got %f", val)
	}
}

func TestAggregateTransitionSignal_NoTransitions(t *testing.T) {
	tf := map[string]string{}
	val := aggregateTransitionSignal("a", tf)

	if val != -1 {
		t.Errorf("expected -1 for single node (no transitions), got %f", val)
	}
}

func TestAggregateTransitionSignal_MixedTransitions(t *testing.T) {
	tf := map[string]string{
		"a->b": "prefer_transition",
		"b->c": "avoid_transition",
	}
	val := aggregateTransitionSignal("a>b>c", tf)

	// Average of 0.8 and 0.2 = 0.5
	expected := 0.5
	if math.Abs(val-expected) > 0.001 {
		t.Errorf("expected ~%f, got %f", expected, val)
	}
}

// --- Confidence Tests ---

func TestComputeConfidence_NoSignals(t *testing.T) {
	signals := &SimulationSignals{}
	conf := computeConfidence("a", signals)

	if conf != 0.1 {
		t.Errorf("expected 0.1 for no signals, got %f", conf)
	}
}

func TestComputeConfidence_AllSignals(t *testing.T) {
	signals := &SimulationSignals{
		PathFeedback:           map[string]string{"a": "neutral"},
		ComparativeFeedback:    map[string]string{"a": "neutral"},
		ComparativeWinRates:    map[string]float64{"a": 0.5},
		TransitionFeedback:     map[string]string{"x->y": "neutral"},
		HistoricalFailureRates: map[string]float64{"a": 0.1},
	}
	conf := computeConfidence("a", signals)

	if conf != 1.0 {
		t.Errorf("expected 1.0 for all signals, got %f", conf)
	}
}

func TestComputeConfidence_PartialSignals(t *testing.T) {
	signals := &SimulationSignals{
		PathFeedback: map[string]string{"a": "neutral"},
	}
	conf := computeConfidence("a", signals)

	expected := 0.2 // 1/5
	if math.Abs(conf-expected) > 0.001 {
		t.Errorf("expected %f for 1 signal, got %f", expected, conf)
	}
}

// --- classifyComparativeFeedback Tests ---

func TestClassifyComparativeFeedback_Underexplored(t *testing.T) {
	r := ComparativeMemoryInfo{MissedWinCount: 3, SelectionCount: 10, WinRate: 0.8}
	result := classifyComparativeFeedback(r)
	if result != "underexplored_path" {
		t.Errorf("expected underexplored_path, got %s", result)
	}
}

func TestClassifyComparativeFeedback_Prefer(t *testing.T) {
	r := ComparativeMemoryInfo{MissedWinCount: 0, SelectionCount: 10, WinRate: 0.8}
	result := classifyComparativeFeedback(r)
	if result != "prefer_path" {
		t.Errorf("expected prefer_path, got %s", result)
	}
}

func TestClassifyComparativeFeedback_Avoid(t *testing.T) {
	r := ComparativeMemoryInfo{MissedWinCount: 0, SelectionCount: 10, LossRate: 0.6}
	result := classifyComparativeFeedback(r)
	if result != "avoid_path" {
		t.Errorf("expected avoid_path, got %s", result)
	}
}

func TestClassifyComparativeFeedback_Neutral_LowSamples(t *testing.T) {
	r := ComparativeMemoryInfo{MissedWinCount: 0, SelectionCount: 3, WinRate: 0.9}
	result := classifyComparativeFeedback(r)
	if result != "neutral" {
		t.Errorf("expected neutral for low samples, got %s", result)
	}
}

// --- Risk Tests ---

func TestComputeExpectedRisk_HighFailureRate(t *testing.T) {
	signals := &SimulationSignals{
		HistoricalFailureRates: map[string]float64{"a": 0.8},
	}
	risk := computeExpectedRisk("a", 1, signals)

	if risk <= 0 {
		t.Errorf("high failure rate should produce positive risk, got %f", risk)
	}
}

func TestComputeExpectedRisk_LongPath(t *testing.T) {
	signals := &SimulationSignals{}
	risk1 := computeExpectedRisk("a", 1, signals)
	risk3 := computeExpectedRisk("a", 3, signals)

	if risk3 <= risk1 {
		t.Errorf("longer path should have higher risk: len=1 risk=%f, len=3 risk=%f", risk1, risk3)
	}
}

// --- clamp01 Tests ---

func TestClamp01_Below(t *testing.T) {
	if clamp01(-0.5) != 0 {
		t.Error("clamp01(-0.5) should be 0")
	}
}

func TestClamp01_Above(t *testing.T) {
	if clamp01(1.5) != 1 {
		t.Error("clamp01(1.5) should be 1")
	}
}

func TestClamp01_InRange(t *testing.T) {
	if clamp01(0.5) != 0.5 {
		t.Error("clamp01(0.5) should be 0.5")
	}
}
