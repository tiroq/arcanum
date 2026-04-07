package planning

import (
	"testing"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// --- Provider-Context Scorer Tests ---

// TestScorer_ProviderAvoidOverridesContextualPrefer verifies that provider-
// context avoid blended with contextual prefer produces a net signal.
func TestScorer_ProviderAvoidOverridesContextualPrefer(t *testing.T) {
	pctx := emptyContext()
	pctx.ProviderName = "openrouter"
	pctx.ModelRole = "default"
	pctx.FailureBucket = "high"
	pctx.BacklogBucket = "low"

	// Contextual: retry_job is good in this context.
	pctx.ContextRecords = []actionmemory.ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1,
			SuccessRate: 0.8, FailureRate: 0.1},
	}

	// Provider-context: retry_job is BAD with openrouter specifically.
	pctx.ProviderContextRecords = []actionmemory.ProviderContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 7, NeutralRuns: 1,
			SuccessRate: 0.2, FailureRate: 0.7},
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Provider avoid should pull score below base.
	if c.Score >= 0.65 {
		t.Errorf("expected score below base, got %.4f", c.Score)
	}
	assertContains(t, "reasoning", c.Reasoning, "provider")
}

// TestScorer_ProviderPreferOverridesGlobalAvoid verifies that good provider
// context can mitigate global avoid.
func TestScorer_ProviderPreferOverridesGlobalAvoid(t *testing.T) {
	pctx := emptyContext()
	pctx.ProviderName = "ollama-local"
	pctx.ModelRole = "default"
	pctx.FailureBucket = "high"
	pctx.BacklogBucket = "low"

	// Global says avoid retry_job.
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}

	// Provider-context: retry_job is GOOD with ollama-local.
	pctx.ProviderContextRecords = []actionmemory.ProviderContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama-local", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1,
			SuccessRate: 0.8, FailureRate: 0.1},
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Provider prefer blended with global avoid → still positive because provider dominates.
	// provider: 0.60 * 0.25 = 0.15, global: 0.40 * (-0.40) = -0.16 → blended = -0.01
	// Score: 0.65 + (-0.01) ≈ 0.64
	if c.Score < 0.60 {
		t.Errorf("expected score near base (provider prefer should mitigate global avoid), got %.4f", c.Score)
	}
}

// TestScorer_NoProviderContext_FallsBackToContextual verifies backward compat.
func TestScorer_NoProviderContext_FallsBackToContextual(t *testing.T) {
	pctx := emptyContext()
	pctx.FailureBucket = "high"
	pctx.BacklogBucket = "low"
	// No provider name — should skip provider-context entirely.

	pctx.ContextRecords = []actionmemory.ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 7, NeutralRuns: 1,
			SuccessRate: 0.2, FailureRate: 0.7},
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Should use contextual-only path: capped 0.7 * (-0.40) = -0.28
	// Score: 0.65 + (-0.28) = 0.37
	assertFloat(t, "contextual fallback", c.Score, 0.37)
}

// TestScorer_NoProviderNoContextual_FallsBackToGlobal verifies full fallback.
func TestScorer_NoProviderNoContextual_FallsBackToGlobal(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// base 0.65 - avoid 0.40 = 0.25
	assertFloat(t, "global fallback", c.Score, 0.25)
}

// TestScorer_DifferentProvidersYieldDifferentScores is the key scenario:
// same action, same goal, but two providers → different scores.
func TestScorer_DifferentProvidersYieldDifferentScores(t *testing.T) {
	basePctx := emptyContext()
	basePctx.FailureBucket = "high"
	basePctx.BacklogBucket = "low"

	basePctx.ProviderContextRecords = []actionmemory.ProviderContextMemoryRecord{
		// openrouter: bad
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 7, NeutralRuns: 1,
			SuccessRate: 0.2, FailureRate: 0.7},
		// ollama-local: good
		{ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama-local", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1,
			SuccessRate: 0.8, FailureRate: 0.1},
	}

	// Score with openrouter
	pctxOpen := basePctx
	pctxOpen.ProviderName = "openrouter"
	pctxOpen.ModelRole = "default"
	scoreOpen := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctxOpen, DefaultScoringParams(),
	)

	// Score with ollama-local
	pctxOllama := basePctx
	pctxOllama.ProviderName = "ollama-local"
	pctxOllama.ModelRole = "default"
	scoreOllama := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctxOllama, DefaultScoringParams(),
	)

	if scoreOllama.Score <= scoreOpen.Score {
		t.Errorf("ollama-local (%.4f) should score higher than openrouter (%.4f) for same action",
			scoreOllama.Score, scoreOpen.Score)
	}
}

// TestScorer_ProviderContextEmptyRecords_NoEffect verifies fail-open.
func TestScorer_ProviderContextEmptyRecords_NoEffect(t *testing.T) {
	pctx := emptyContext()
	pctx.ProviderName = "openrouter"
	pctx.ModelRole = "default"
	// Empty provider context records

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// base = 0.65, no feedback → no bias
	assertFloat(t, "empty provider records", c.Score, 0.65)
	assertContains(t, "reasoning", c.Reasoning, "no historical feedback")
}
