package actionmemory

import (
	"math"
	"testing"
)

// --- BucketFailureRate Tests ---

func TestBucketFailureRate_Low(t *testing.T) {
	if got := BucketFailureRate(0.05); got != "low" {
		t.Errorf("got %s, want low", got)
	}
}

func TestBucketFailureRate_Medium(t *testing.T) {
	if got := BucketFailureRate(0.15); got != "medium" {
		t.Errorf("got %s, want medium", got)
	}
}

func TestBucketFailureRate_High(t *testing.T) {
	if got := BucketFailureRate(0.35); got != "high" {
		t.Errorf("got %s, want high", got)
	}
}

func TestBucketFailureRate_Boundary_Low(t *testing.T) {
	if got := BucketFailureRate(0.0); got != "low" {
		t.Errorf("got %s, want low", got)
	}
}

func TestBucketFailureRate_Boundary_MediumStart(t *testing.T) {
	if got := BucketFailureRate(0.1); got != "medium" {
		t.Errorf("got %s, want medium", got)
	}
}

func TestBucketFailureRate_Boundary_HighStart(t *testing.T) {
	if got := BucketFailureRate(0.3); got != "high" {
		t.Errorf("got %s, want high", got)
	}
}

// --- BucketBacklog Tests ---

func TestBucketBacklog_Low(t *testing.T) {
	if got := BucketBacklog(10); got != "low" {
		t.Errorf("got %s, want low", got)
	}
}

func TestBucketBacklog_Medium(t *testing.T) {
	if got := BucketBacklog(30); got != "medium" {
		t.Errorf("got %s, want medium", got)
	}
}

func TestBucketBacklog_High(t *testing.T) {
	if got := BucketBacklog(60); got != "high" {
		t.Errorf("got %s, want high", got)
	}
}

func TestBucketBacklog_Boundary_Zero(t *testing.T) {
	if got := BucketBacklog(0); got != "low" {
		t.Errorf("got %s, want low", got)
	}
}

func TestBucketBacklog_Boundary_MediumStart(t *testing.T) {
	if got := BucketBacklog(20); got != "medium" {
		t.Errorf("got %s, want medium", got)
	}
}

func TestBucketBacklog_Boundary_HighStart(t *testing.T) {
	if got := BucketBacklog(50); got != "high" {
		t.Errorf("got %s, want high", got)
	}
}

// --- ResolveContextualFeedback Tests ---

func TestResolveContextual_ExactMatch(t *testing.T) {
	records := []ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 7, NeutralRuns: 1, SuccessRate: 0.2, FailureRate: 0.7},
	}

	fb := ResolveContextualFeedback(records, "retry_job", "reduce_retry_rate", "high", "low")
	if fb == nil {
		t.Fatal("expected feedback, got nil")
	}
	if fb.ContextMatch != "exact" {
		t.Errorf("got context_match=%s, want exact", fb.ContextMatch)
	}
	if fb.Recommendation != RecommendAvoidAction {
		t.Errorf("got %s, want avoid_action", fb.Recommendation)
	}
}

func TestResolveContextual_PartialMatch(t *testing.T) {
	records := []ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 5, SuccessRuns: 1, FailureRuns: 3, NeutralRuns: 1, SuccessRate: 0.2, FailureRate: 0.6},
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 5, SuccessRuns: 1, FailureRuns: 3, NeutralRuns: 1, SuccessRate: 0.2, FailureRate: 0.6},
	}

	// Request with failure_bucket=medium which doesn't exist → falls to partial.
	fb := ResolveContextualFeedback(records, "retry_job", "reduce_retry_rate", "medium", "low")
	if fb == nil {
		t.Fatal("expected feedback, got nil")
	}
	if fb.ContextMatch != "partial" {
		t.Errorf("got context_match=%s, want partial", fb.ContextMatch)
	}
	// Partial aggregates: 10 runs, 2 success, 6 failure → failure_rate=0.6 → avoid
	if fb.Recommendation != RecommendAvoidAction {
		t.Errorf("got %s, want avoid_action", fb.Recommendation)
	}
}

func TestResolveContextual_NoMatch(t *testing.T) {
	records := []ContextMemoryRecord{
		{ActionType: "trigger_resync", GoalType: "resolve_queue_backlog", FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1, SuccessRate: 0.8, FailureRate: 0.1},
	}

	fb := ResolveContextualFeedback(records, "retry_job", "reduce_retry_rate", "high", "low")
	if fb != nil {
		t.Errorf("expected nil for no match, got %+v", fb)
	}
}

func TestResolveContextual_EmptyRecords(t *testing.T) {
	fb := ResolveContextualFeedback(nil, "retry_job", "reduce_retry_rate", "high", "low")
	if fb != nil {
		t.Errorf("expected nil for empty records, got %+v", fb)
	}
}

func TestResolveContextual_ExactPreferredOverPartial(t *testing.T) {
	records := []ContextMemoryRecord{
		// Exact match: good success rate
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1, SuccessRate: 0.8, FailureRate: 0.1},
		// Different bucket: bad success rate
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "high", BacklogBucket: "high",
			TotalRuns: 10, SuccessRuns: 1, FailureRuns: 8, NeutralRuns: 1, SuccessRate: 0.1, FailureRate: 0.8},
	}

	fb := ResolveContextualFeedback(records, "retry_job", "reduce_retry_rate", "low", "low")
	if fb == nil {
		t.Fatal("expected feedback")
	}
	if fb.ContextMatch != "exact" {
		t.Errorf("should prefer exact match, got %s", fb.ContextMatch)
	}
	if fb.Recommendation != RecommendPreferAction {
		t.Errorf("exact match should be prefer, got %s", fb.Recommendation)
	}
}

// --- BlendFeedbackAdjustment Tests ---

func TestBlend_ContextAvoidGlobalPrefer(t *testing.T) {
	ctx := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendAvoidAction, SampleSize: 10},
		ContextMatch:   "exact",
	}
	global := &ActionFeedback{Recommendation: RecommendPreferAction, SampleSize: 10}

	adj, reason := BlendFeedbackAdjustment(ctx, global, 0.40, 0.25)
	// blended = 0.7 * (-0.40) + 0.3 * (+0.25) = -0.28 + 0.075 = -0.205
	assertFloatClose(t, "blended", adj, -0.205)
	if reason == "" {
		t.Error("expected reasoning")
	}
}

func TestBlend_ContextPreferGlobalAvoid(t *testing.T) {
	ctx := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendPreferAction, SampleSize: 10},
		ContextMatch:   "exact",
	}
	global := &ActionFeedback{Recommendation: RecommendAvoidAction, SampleSize: 10}

	adj, _ := BlendFeedbackAdjustment(ctx, global, 0.40, 0.25)
	// blended = 0.7 * (+0.25) + 0.3 * (-0.40) = 0.175 - 0.12 = 0.055
	assertFloatClose(t, "blended", adj, 0.055)
}

func TestBlend_ContextOnlyNoCap(t *testing.T) {
	ctx := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendAvoidAction, SampleSize: 10},
		ContextMatch:   "partial",
	}

	adj, _ := BlendFeedbackAdjustment(ctx, nil, 0.40, 0.25)
	// capped = 0.7 * (-0.40) = -0.28
	assertFloatClose(t, "context-only", adj, -0.28)
}

func TestBlend_GlobalOnlyFull(t *testing.T) {
	global := &ActionFeedback{Recommendation: RecommendAvoidAction, SampleSize: 10}

	adj, _ := BlendFeedbackAdjustment(nil, global, 0.40, 0.25)
	assertFloatClose(t, "global-only", adj, -0.40)
}

func TestBlend_BothNeutral(t *testing.T) {
	ctx := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendNeutral, SampleSize: 10},
		ContextMatch:   "exact",
	}
	global := &ActionFeedback{Recommendation: RecommendNeutral, SampleSize: 10}

	adj, _ := BlendFeedbackAdjustment(ctx, global, 0.40, 0.25)
	assertFloatClose(t, "both neutral", adj, 0)
}

func TestBlend_BothNil(t *testing.T) {
	adj, reason := BlendFeedbackAdjustment(nil, nil, 0.40, 0.25)
	assertFloatClose(t, "both nil", adj, 0)
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestBlend_InsufficientDataIgnored(t *testing.T) {
	ctx := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendInsufficientData, SampleSize: 3},
		ContextMatch:   "exact",
	}
	global := &ActionFeedback{Recommendation: RecommendAvoidAction, SampleSize: 10}

	adj, _ := BlendFeedbackAdjustment(ctx, global, 0.40, 0.25)
	// contextual is insufficient → treated as no contextual data → global only
	assertFloatClose(t, "insufficient ctx", adj, -0.40)
}

// --- Helper ---

func assertFloatClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.001 {
		t.Errorf("%s: got %.4f, want %.4f", label, got, want)
	}
}
