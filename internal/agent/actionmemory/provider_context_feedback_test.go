package actionmemory

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- ResolveProviderContextFeedback Tests ---

func TestResolveProviderContext_ExactMatch(t *testing.T) {
	records := []ProviderContextMemoryRecord{
		{ID: uuid.New(), ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 7, NeutralRuns: 1,
			SuccessRate: 0.2, FailureRate: 0.7, LastUpdated: time.Now()},
	}

	fb := ResolveProviderContextFeedback(records, "retry_job", "reduce_retry_rate",
		"openrouter", "default", "high", "low")
	if fb == nil {
		t.Fatal("expected feedback, got nil")
	}
	if fb.ContextMatch != "provider_exact" {
		t.Errorf("got context_match=%s, want provider_exact", fb.ContextMatch)
	}
	if fb.Recommendation != RecommendAvoidAction {
		t.Errorf("got %s, want avoid_action", fb.Recommendation)
	}
}

func TestResolveProviderContext_PartialMatch(t *testing.T) {
	records := []ProviderContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 5, SuccessRuns: 1, FailureRuns: 3, NeutralRuns: 1,
			SuccessRate: 0.2, FailureRate: 0.6},
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "fast",
			FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 5, SuccessRuns: 1, FailureRuns: 3, NeutralRuns: 1,
			SuccessRate: 0.2, FailureRate: 0.6},
	}

	// Request with model_role/buckets that don't match exactly → partial on action+goal+provider.
	fb := ResolveProviderContextFeedback(records, "retry_job", "reduce_retry_rate",
		"ollama", "planner", "medium", "medium")
	if fb == nil {
		t.Fatal("expected feedback, got nil")
	}
	if fb.ContextMatch != "provider_partial" {
		t.Errorf("got context_match=%s, want provider_partial", fb.ContextMatch)
	}
	// Partial aggregates: 10 runs, 2 success, 6 failure → failure_rate=0.6 → avoid
	if fb.Recommendation != RecommendAvoidAction {
		t.Errorf("got %s, want avoid_action", fb.Recommendation)
	}
}

func TestResolveProviderContext_NoMatch(t *testing.T) {
	records := []ProviderContextMemoryRecord{
		{ActionType: "trigger_resync", GoalType: "resolve_queue_backlog",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1,
			SuccessRate: 0.8, FailureRate: 0.1},
	}

	fb := ResolveProviderContextFeedback(records, "retry_job", "reduce_retry_rate",
		"ollama", "default", "high", "low")
	if fb != nil {
		t.Errorf("expected nil for no match, got %+v", fb)
	}
}

func TestResolveProviderContext_EmptyProviderName(t *testing.T) {
	records := []ProviderContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1},
	}

	fb := ResolveProviderContextFeedback(records, "retry_job", "reduce_retry_rate",
		"", "default", "high", "low")
	if fb != nil {
		t.Errorf("expected nil when providerName is empty, got %+v", fb)
	}
}

func TestResolveProviderContext_DifferentProvidersSameAction(t *testing.T) {
	records := []ProviderContextMemoryRecord{
		// openrouter: bad stats
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 7, NeutralRuns: 1,
			SuccessRate: 0.2, FailureRate: 0.7},
		// ollama-local: good stats
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama-local", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1,
			SuccessRate: 0.8, FailureRate: 0.1},
	}

	// Query for openrouter → avoid
	fbOpen := ResolveProviderContextFeedback(records, "retry_job", "reduce_retry_rate",
		"openrouter", "default", "high", "low")
	if fbOpen == nil {
		t.Fatal("expected feedback for openrouter")
	}
	if fbOpen.Recommendation != RecommendAvoidAction {
		t.Errorf("openrouter: got %s, want avoid_action", fbOpen.Recommendation)
	}

	// Query for ollama-local → prefer
	fbOllama := ResolveProviderContextFeedback(records, "retry_job", "reduce_retry_rate",
		"ollama-local", "default", "high", "low")
	if fbOllama == nil {
		t.Fatal("expected feedback for ollama-local")
	}
	if fbOllama.Recommendation != RecommendPreferAction {
		t.Errorf("ollama-local: got %s, want prefer_action", fbOllama.Recommendation)
	}
}

// --- BlendProviderFeedbackAdjustment Tests ---

func TestBlendProvider_ProviderAvoidFallbackPrefer(t *testing.T) {
	provFb := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendAvoidAction, SampleSize: 10},
		ContextMatch:   "provider_exact",
	}

	adj, reason := BlendProviderFeedbackAdjustment(provFb, 0.25, "global prefer", 0.40, 0.25)
	// blended = 0.60 * (-0.40) + 0.40 * (+0.25) = -0.24 + 0.10 = -0.14
	assertProviderFloat(t, "provider avoid + fallback prefer", adj, -0.14)
	if reason == "" {
		t.Error("expected reasoning")
	}
}

func TestBlendProvider_ProviderPreferFallbackAvoid(t *testing.T) {
	provFb := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendPreferAction, SampleSize: 10},
		ContextMatch:   "provider_exact",
	}

	adj, _ := BlendProviderFeedbackAdjustment(provFb, -0.40, "global avoid", 0.40, 0.25)
	// blended = 0.60 * (+0.25) + 0.40 * (-0.40) = 0.15 - 0.16 = -0.01
	assertProviderFloat(t, "provider prefer + fallback avoid", adj, -0.01)
}

func TestBlendProvider_ProviderOnlyCapped(t *testing.T) {
	provFb := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendAvoidAction, SampleSize: 10},
		ContextMatch:   "provider_partial",
	}

	adj, _ := BlendProviderFeedbackAdjustment(provFb, 0, "", 0.40, 0.25)
	// capped = 0.60 * (-0.40) = -0.24
	assertProviderFloat(t, "provider only capped", adj, -0.24)
}

func TestBlendProvider_NilProviderPassthrough(t *testing.T) {
	adj, reason := BlendProviderFeedbackAdjustment(nil, -0.40, "global avoid", 0.40, 0.25)
	assertProviderFloat(t, "nil provider passthrough", adj, -0.40)
	if reason != "global avoid" {
		t.Errorf("expected passthrough reason, got %q", reason)
	}
}

func TestBlendProvider_InsufficientProviderPassthrough(t *testing.T) {
	provFb := &ContextualFeedback{
		ActionFeedback: ActionFeedback{Recommendation: RecommendInsufficientData, SampleSize: 3},
		ContextMatch:   "provider_exact",
	}

	adj, reason := BlendProviderFeedbackAdjustment(provFb, -0.28, "ctx avoid capped", 0.40, 0.25)
	assertProviderFloat(t, "insufficient provider", adj, -0.28)
	if reason != "ctx avoid capped" {
		t.Errorf("expected passthrough reason, got %q", reason)
	}
}

// --- Backward Compatibility ---

func TestResolveProviderContext_EmptyRecords(t *testing.T) {
	fb := ResolveProviderContextFeedback(nil, "retry_job", "reduce_retry_rate",
		"openrouter", "default", "high", "low")
	if fb != nil {
		t.Errorf("expected nil for empty records, got %+v", fb)
	}
}

func TestResolveProviderContext_ExactPreferredOverPartial(t *testing.T) {
	records := []ProviderContextMemoryRecord{
		// Exact match: good stats
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "default",
			FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1,
			SuccessRate: 0.8, FailureRate: 0.1},
		// Different bucket: bad stats
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "high",
			TotalRuns: 10, SuccessRuns: 1, FailureRuns: 8, NeutralRuns: 1,
			SuccessRate: 0.1, FailureRate: 0.8},
	}

	fb := ResolveProviderContextFeedback(records, "retry_job", "reduce_retry_rate",
		"ollama", "default", "low", "low")
	if fb == nil {
		t.Fatal("expected feedback")
	}
	if fb.ContextMatch != "provider_exact" {
		t.Errorf("should prefer exact match, got %s", fb.ContextMatch)
	}
	if fb.Recommendation != RecommendPreferAction {
		t.Errorf("exact match should be prefer, got %s", fb.Recommendation)
	}
}

func assertProviderFloat(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %.4f, want %.4f", label, got, want)
	}
}
