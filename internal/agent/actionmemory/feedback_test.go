package actionmemory

import (
	"testing"
)

// --- Feedback Classification Tests ---

func TestGenerateFeedback_InsufficientData_Nil(t *testing.T) {
	fb := GenerateFeedback(nil)
	assertEqual(t, "recommendation", string(fb.Recommendation), "insufficient_data")
}

func TestGenerateFeedback_InsufficientData_LowSample(t *testing.T) {
	record := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   4,
		SuccessRuns: 3,
		FailureRuns: 1,
		NeutralRuns: 0,
		SuccessRate: 0.75,
		FailureRate: 0.25,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "insufficient_data")
	assertEqual(t, "sample_size", fb.SampleSize, 4)
}

func TestGenerateFeedback_AvoidAction(t *testing.T) {
	record := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 2,
		FailureRuns: 5,
		NeutralRuns: 3,
		SuccessRate: 0.2,
		FailureRate: 0.5,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "avoid_action")
	assertEqual(t, "action_type", fb.ActionType, "retry_job")
}

func TestGenerateFeedback_AvoidAction_HighFailure(t *testing.T) {
	record := &ActionMemoryRecord{
		ActionType:  "trigger_resync",
		TotalRuns:   20,
		SuccessRuns: 2,
		FailureRuns: 15,
		NeutralRuns: 3,
		SuccessRate: 0.1,
		FailureRate: 0.75,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "avoid_action")
}

func TestGenerateFeedback_PreferAction(t *testing.T) {
	record := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 8,
		FailureRuns: 1,
		NeutralRuns: 1,
		SuccessRate: 0.8,
		FailureRate: 0.1,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "prefer_action")
}

func TestGenerateFeedback_PreferAction_Threshold(t *testing.T) {
	record := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 7,
		FailureRuns: 1,
		NeutralRuns: 2,
		SuccessRate: 0.7,
		FailureRate: 0.1,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "prefer_action")
}

func TestGenerateFeedback_Neutral(t *testing.T) {
	record := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 5,
		FailureRuns: 3,
		NeutralRuns: 2,
		SuccessRate: 0.5,
		FailureRate: 0.3,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "neutral")
}

// --- Aggregation Correctness Tests ---

func TestOutcomeIncrements_Success(t *testing.T) {
	s, f, n := outcomeIncrements("success")
	assertEqual(t, "success", s, 1)
	assertEqual(t, "failure", f, 0)
	assertEqual(t, "neutral", n, 0)
}

func TestOutcomeIncrements_Failure(t *testing.T) {
	s, f, n := outcomeIncrements("failure")
	assertEqual(t, "success", s, 0)
	assertEqual(t, "failure", f, 1)
	assertEqual(t, "neutral", n, 0)
}

func TestOutcomeIncrements_Neutral(t *testing.T) {
	s, f, n := outcomeIncrements("neutral")
	assertEqual(t, "success", s, 0)
	assertEqual(t, "failure", f, 0)
	assertEqual(t, "neutral", n, 1)
}

func TestOutcomeIncrements_Unknown(t *testing.T) {
	s, f, n := outcomeIncrements("unknown")
	assertEqual(t, "success", s, 0)
	assertEqual(t, "failure", f, 0)
	assertEqual(t, "neutral", n, 1)
}

// --- Rate Calculation Tests ---

func TestRateCalculation_AllSuccess(t *testing.T) {
	record := &ActionMemoryRecord{
		TotalRuns:   10,
		SuccessRuns: 10,
		FailureRuns: 0,
		NeutralRuns: 0,
		SuccessRate: 1.0,
		FailureRate: 0.0,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "prefer_action")
	assertFloat(t, "success_rate", fb.SuccessRate, 1.0)
	assertFloat(t, "failure_rate", fb.FailureRate, 0.0)
}

func TestRateCalculation_AllFailure(t *testing.T) {
	record := &ActionMemoryRecord{
		TotalRuns:   10,
		SuccessRuns: 0,
		FailureRuns: 10,
		NeutralRuns: 0,
		SuccessRate: 0.0,
		FailureRate: 1.0,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "avoid_action")
}

func TestRateCalculation_Mixed(t *testing.T) {
	record := &ActionMemoryRecord{
		TotalRuns:   20,
		SuccessRuns: 8,
		FailureRuns: 6,
		NeutralRuns: 6,
		SuccessRate: 0.4,
		FailureRate: 0.3,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "neutral")
}

// --- Feedback Priority: failure_rate >= 0.5 takes precedence even if success_rate >= 0.7 ---

func TestFeedback_FailurePrecedence(t *testing.T) {
	// Contrived: if somehow both thresholds are met, failure check comes first.
	record := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 7,
		FailureRuns: 5,
		NeutralRuns: 0,
		SuccessRate: 0.7,
		FailureRate: 0.5,
	}
	fb := GenerateFeedback(record)
	assertEqual(t, "recommendation", string(fb.Recommendation), "avoid_action")
}

// --- Helpers ---

func assertEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func assertFloat(t *testing.T, field string, got, want float64) {
	t.Helper()
	if got < want-0.001 || got > want+0.001 {
		t.Errorf("%s: got %f, want %f", field, got, want)
	}
}
