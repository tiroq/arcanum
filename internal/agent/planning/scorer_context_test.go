package planning

import (
	"testing"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// TestScorer_ContextualAvoidOverridesGlobalPrefer verifies that contextual
// avoid_action blended with global prefer_action produces a net negative.
func TestScorer_ContextualAvoidOverridesGlobalPrefer(t *testing.T) {
	pctx := emptyContext()
	// Global says prefer retry_job.
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.8,
		FailureRate:    0.1,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendPreferAction,
	}
	// Contextual: for reduce_retry_rate + high failure, retry_job is bad.
	pctx.ContextRecords = []actionmemory.ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 7, NeutralRuns: 1, SuccessRate: 0.2, FailureRate: 0.7},
	}
	pctx.FailureBucket = "high"
	pctx.BacklogBucket = "low"

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// base 0.65 + blended(0.7*(-0.40) + 0.3*(+0.25)) = 0.65 + (-0.205) = 0.445
	assertFloat(t, "contextual avoid overrides", c.Score, 0.445)
	assertContains(t, "reasoning", c.Reasoning, "blended")
}

// TestScorer_ContextualPreferOverridesGlobalAvoid verifies that contextual
// prefer blended with global avoid produces a positive signal.
func TestScorer_ContextualPreferOverridesGlobalAvoid(t *testing.T) {
	pctx := emptyContext()
	// Global says avoid retry_job.
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}
	// Contextual: in this specific context, retry_job works great.
	pctx.ContextRecords = []actionmemory.ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1, SuccessRate: 0.8, FailureRate: 0.1},
	}
	pctx.FailureBucket = "low"
	pctx.BacklogBucket = "low"

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// base 0.65 + blended(0.7*(+0.25) + 0.3*(-0.40)) = 0.65 + 0.055 = 0.705
	assertFloat(t, "contextual prefer overrides", c.Score, 0.705)
}

// TestScorer_NoContextualData_FallsBackToGlobal verifies backward compat.
func TestScorer_NoContextualData_FallsBackToGlobal(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}
	// No context records — should use global only.

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// base 0.65 - avoid 0.40 = 0.25 (same as before contextual existed)
	assertFloat(t, "global fallback", c.Score, 0.25)
	assertContains(t, "reasoning", c.Reasoning, "avoid_action")
}

// TestScorer_ContextualDifferentGoalTypes verifies context-aware per-goal.
func TestScorer_ContextualDifferentGoalTypes(t *testing.T) {
	pctx := emptyContext()
	// Context: retry_job is BAD for reduce_retry_rate, GOOD for investigate_failed_jobs.
	pctx.ContextRecords = []actionmemory.ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "medium", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 2, FailureRuns: 6, NeutralRuns: 2, SuccessRate: 0.2, FailureRate: 0.6},
		{ActionType: "retry_job", GoalType: "investigate_failed_jobs", FailureBucket: "medium", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1, SuccessRate: 0.8, FailureRate: 0.1},
	}
	pctx.FailureBucket = "medium"
	pctx.BacklogBucket = "low"

	// Score for reduce_retry_rate — contextual avoid
	c1 := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Score for investigate_failed_jobs — contextual prefer
	c2 := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "investigate_failed_jobs"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	if c2.Score <= c1.Score {
		t.Errorf("contextual should differ by goal: reduce_retry=%.2f, investigate=%.2f", c1.Score, c2.Score)
	}
}

// TestScorer_PartialContextMatch verifies partial fallback works.
func TestScorer_PartialContextMatch(t *testing.T) {
	pctx := emptyContext()
	// Records with different buckets, same action+goal.
	pctx.ContextRecords = []actionmemory.ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "high", BacklogBucket: "high",
			TotalRuns: 5, SuccessRuns: 1, FailureRuns: 3, NeutralRuns: 1, SuccessRate: 0.2, FailureRate: 0.6},
		{ActionType: "retry_job", GoalType: "reduce_retry_rate", FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 5, SuccessRuns: 1, FailureRuns: 3, NeutralRuns: 1, SuccessRate: 0.2, FailureRate: 0.6},
	}
	// Request with buckets that don't match exactly → aggregates partial.
	pctx.FailureBucket = "medium"
	pctx.BacklogBucket = "medium"

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Partial: 10 total, 2 success, 6 failure → failure_rate=0.6 → avoid
	// Blended (no global): capped 0.7 * (-0.40) = -0.28
	// Score: 0.65 + (-0.28) = 0.37
	assertFloat(t, "partial match", c.Score, 0.37)
}

// TestPlanner_ContextualOverridesGlobalSelection verifies end-to-end.
func TestPlanner_ContextualOverridesGlobalSelection(t *testing.T) {
	pctx := emptyContext()
	// Global: retry_job is avoid (overall bad).
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}
	// Contextual: for investigate_failed_jobs + low failure, retry works well.
	pctx.ContextRecords = []actionmemory.ContextMemoryRecord{
		{ActionType: "retry_job", GoalType: "investigate_failed_jobs", FailureBucket: "low", BacklogBucket: "low",
			TotalRuns: 10, SuccessRuns: 8, FailureRuns: 1, NeutralRuns: 1, SuccessRate: 0.8, FailureRate: 0.1},
	}
	pctx.FailureBucket = "low"
	pctx.BacklogBucket = "low"

	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("investigate_failed_jobs", 0.6, 0.9)
	d := ap.planForGoal(g, pctx)

	// With contextual prefer (0.055 net positive), retry_job should still be chosen
	// despite global avoid. Blended: 0.65 + 0.055 = 0.705 vs log_rec 0.70.
	// retry_job at 0.705 > log_recommendation at 0.70 → retry selected.
	assertEqual(t, "contextual override", d.SelectedActionType, "retry_job")
}
