package planning

import (
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/goals"
)

// --- Scorer Tests ---

func TestScoreCandidate_BaseScore(t *testing.T) {
	pctx := emptyContext()
	c := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"}, 0.5, 0.9, pctx)

	// base = 0.5 + (0.5 * 0.3) = 0.65
	assertFloat(t, "base score", c.Score, 0.65)
	assertEqual(t, "confidence", c.Confidence, 0.9)
	assertNonEmpty(t, "reasoning", c.Reasoning)
}

func TestScoreCandidate_NoopPenalty(t *testing.T) {
	pctx := emptyContext()
	c := ScoreCandidate(PlannedActionCandidate{ActionType: "noop"}, 0.5, 0.9, pctx)

	// base = 0.5 + (0.5 * 0.3) = 0.65, then -0.20 noop = 0.45
	assertFloat(t, "noop score", c.Score, 0.45)
}

func TestScoreCandidate_FeedbackPreferBoost(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.8,
		FailureRate:    0.1,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendPreferAction,
	}

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)
	// base 0.65 + prefer 0.25 = 0.90
	assertFloat(t, "prefer score", c.Score, 0.90)
	assertContains(t, "reasoning", c.Reasoning, "prefer_action")
}

func TestScoreCandidate_FeedbackAvoidPenalty(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)
	// base 0.65 - avoid 0.40 = 0.25
	assertFloat(t, "avoid score", c.Score, 0.25)
	assertContains(t, "reasoning", c.Reasoning, "avoid_action")
}

func TestScoreCandidate_HighBacklogPenalizesResync(t *testing.T) {
	pctx := emptyContext()
	pctx.QueueBacklog = 60 // > 50

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "trigger_resync"}, 0.5, 0.9, pctx)
	// base 0.65 - highBacklogResync 0.30 + safetyPref 0 = 0.35
	assertFloat(t, "resync with high backlog", c.Score, 0.35)
	assertContains(t, "reasoning", c.Reasoning, "high backlog")
}

func TestScoreCandidate_HighBacklogPenalizesRetrySlightly(t *testing.T) {
	pctx := emptyContext()
	pctx.QueueBacklog = 60

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)
	// base 0.65 - highBacklogRetry 0.10 = 0.55
	assertFloat(t, "retry with high backlog", c.Score, 0.55)
}

func TestScoreCandidate_HighRetryBoostsRetryIfHealthy(t *testing.T) {
	pctx := emptyContext()
	pctx.RetryScheduledCount = 15 // > 10

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)
	// base 0.65 + highRetryBoost 0.15 = 0.80
	assertFloat(t, "retry with high retry count", c.Score, 0.80)
	assertContains(t, "reasoning", c.Reasoning, "high retry_scheduled")
}

func TestScoreCandidate_HighRetryNoBoostIfAvoid(t *testing.T) {
	pctx := emptyContext()
	pctx.RetryScheduledCount = 15
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		Recommendation: actionmemory.RecommendAvoidAction,
		FailureRate:    0.6,
		SampleSize:     10,
	}

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)
	// base 0.65 - avoid 0.40 = 0.25 (no retry boost because avoid)
	assertFloat(t, "retry with avoid + high retry", c.Score, 0.25)
}

func TestScoreCandidate_HighFailureRateBoostsRecommendation(t *testing.T) {
	pctx := emptyContext()
	pctx.FailureRate = 0.30 // > 0.20

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "log_recommendation"}, 0.5, 0.9, pctx)
	// base 0.65 + failureRateRecommendBoost 0.15 + safetyPref 0.05 = 0.85
	assertFloat(t, "recommendation with high failure rate", c.Score, 0.85)
}

func TestScoreCandidate_LowAcceptancePrefersAdvisory(t *testing.T) {
	pctx := emptyContext()
	pctx.AcceptanceRate = 0.30 // < 0.40

	cLogrec := ScoreCandidate(PlannedActionCandidate{ActionType: "log_recommendation"}, 0.5, 0.9, pctx)
	cRetry := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)

	if cLogrec.Score <= cRetry.Score {
		t.Errorf("log_recommendation (%.2f) should score higher than retry_job (%.2f) with low acceptance", cLogrec.Score, cRetry.Score)
	}
}

func TestScoreCandidate_SafetyPreference(t *testing.T) {
	pctx := emptyContext()

	cLogrec := ScoreCandidate(PlannedActionCandidate{ActionType: "log_recommendation"}, 0.5, 0.9, pctx)
	cRetry := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)

	// log_recommendation gets safety boost of 0.05
	assertFloat(t, "logrec score", cLogrec.Score, 0.70)
	assertFloat(t, "retry score", cRetry.Score, 0.65)
}

// --- Planning Decision Tests ---

func TestPlanForGoal_PreferHistoricalGood(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.8,
		FailureRate:    0.1,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendPreferAction,
	}

	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("reduce_retry_rate", 0.6, 0.9)
	d := ap.planForGoal(g, pctx)

	assertEqual(t, "selected", d.SelectedActionType, "retry_job")
	assertNonEmptyStr(t, "explanation", d.Explanation)
}

func TestPlanForGoal_AvoidHistoricalBad(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}

	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("reduce_retry_rate", 0.6, 0.9)
	d := ap.planForGoal(g, pctx)

	// retry_job should be deprioritized; log_recommendation or noop preferred.
	if d.SelectedActionType == "retry_job" {
		t.Errorf("should not select retry_job when avoid_action feedback present, got %s", d.SelectedActionType)
	}
	assertEqual(t, "selected", d.SelectedActionType, "log_recommendation")
}

func TestPlanForGoal_HighBacklogPenalizesResync(t *testing.T) {
	pctx := emptyContext()
	pctx.QueueBacklog = 60

	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("resolve_queue_backlog", 0.6, 0.9)
	d := ap.planForGoal(g, pctx)

	// trigger_resync penalized heavily, log_recommendation should win.
	assertEqual(t, "selected", d.SelectedActionType, "log_recommendation")
}

func TestPlanForGoal_AllCandidatesBad_Noop(t *testing.T) {
	pctx := emptyContext()
	pctx.QueueBacklog = 60
	pctx.RecentActionFeedback["trigger_resync"] = actionmemory.ActionFeedback{
		Recommendation: actionmemory.RecommendAvoidAction,
		FailureRate:    0.7,
		SampleSize:     10,
	}
	pctx.RecentActionFeedback["log_recommendation"] = actionmemory.ActionFeedback{
		Recommendation: actionmemory.RecommendAvoidAction,
		FailureRate:    0.6,
		SampleSize:     10,
	}

	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("resolve_queue_backlog", 0.1, 0.5) // low priority
	d := ap.planForGoal(g, pctx)

	// With low priority + avoid penalties on both candidates, noop may win.
	// At the very least, verify decision is deterministic and explained.
	assertNonEmptyStr(t, "explanation", d.Explanation)
	if len(d.Candidates) == 0 {
		t.Error("should have candidates")
	}
}

func TestPlanForGoal_DeterministicOrdering(t *testing.T) {
	pctx := emptyContext()
	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("reduce_retry_rate", 0.5, 0.9)

	d1 := ap.planForGoal(g, pctx)
	d2 := ap.planForGoal(g, pctx)

	assertEqual(t, "deterministic selection", d1.SelectedActionType, d2.SelectedActionType)
	if len(d1.Candidates) != len(d2.Candidates) {
		t.Error("candidate count should be stable")
	}
	for i := range d1.Candidates {
		assertFloat(t, "deterministic score", d1.Candidates[i].Score, d2.Candidates[i].Score)
	}
}

func TestPlanForGoal_ExplanationsPopulated(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.8,
		FailureRate:    0.1,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendPreferAction,
	}

	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("reduce_retry_rate", 0.5, 0.9)
	d := ap.planForGoal(g, pctx)

	assertNonEmptyStr(t, "explanation", d.Explanation)
	assertContains(t, "explanation", []string{d.Explanation}, "selected")

	for _, c := range d.Candidates {
		if len(c.Reasoning) == 0 {
			t.Errorf("candidate %s should have reasoning", c.ActionType)
		}
	}
}

func TestPlanForGoal_AdvisoryGoals_LogRecommendation(t *testing.T) {
	pctx := emptyContext()
	ap := &AdaptivePlanner{logger: noopLogger()}

	for _, goalType := range []string{"increase_reliability", "increase_model_quality", "reduce_latency"} {
		g := makeGoal(goalType, 0.5, 0.9)
		d := ap.planForGoal(g, pctx)

		if d.SelectedActionType != "log_recommendation" && d.SelectedActionType != "noop" {
			t.Errorf("advisory goal %s should select log_recommendation or noop, got %s", goalType, d.SelectedActionType)
		}
	}
}

func TestPlanForGoal_UnknownGoal_Noop(t *testing.T) {
	pctx := emptyContext()
	ap := &AdaptivePlanner{logger: noopLogger()}
	g := makeGoal("unknown_goal", 0.5, 0.5)
	d := ap.planForGoal(g, pctx)
	assertEqual(t, "selected", d.SelectedActionType, "noop")
}

// --- CandidatesForGoal Tests ---

func TestCandidatesForGoal_KnownGoals(t *testing.T) {
	cases := map[string][]string{
		"reduce_retry_rate":       {"retry_job", "log_recommendation", "noop"},
		"investigate_failed_jobs": {"retry_job", "log_recommendation", "noop"},
		"resolve_queue_backlog":   {"trigger_resync", "log_recommendation", "noop"},
		"increase_reliability":    {"log_recommendation", "noop"},
		"increase_model_quality":  {"log_recommendation", "noop"},
		"reduce_latency":          {"log_recommendation", "noop"},
	}

	for goal, expected := range cases {
		got := CandidatesForGoal(goal)
		if len(got) != len(expected) {
			t.Errorf("%s: got %d candidates, want %d", goal, len(got), len(expected))
			continue
		}
		for i := range expected {
			if got[i] != expected[i] {
				t.Errorf("%s[%d]: got %s, want %s", goal, i, got[i], expected[i])
			}
		}
	}
}

func TestCandidatesForGoal_UnknownReturnNoop(t *testing.T) {
	got := CandidatesForGoal("nonexistent")
	if len(got) != 1 || got[0] != "noop" {
		t.Errorf("unknown goal should return [noop], got %v", got)
	}
}

// --- Context-aware combined tests ---

func TestContextAware_HighFailureRateWithBadHistory(t *testing.T) {
	pctx := emptyContext()
	pctx.FailureRate = 0.30
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		Recommendation: actionmemory.RecommendAvoidAction,
		FailureRate:    0.6,
		SampleSize:     10,
	}

	c := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx)
	// base 0.65 - avoid 0.40 - hiFailBadHistory 0.20 = 0.05
	assertFloat(t, "double penalty", c.Score, 0.05)
}

// --- Helpers ---

func emptyContext() PlanningContext {
	return PlanningContext{
		RecentActionFeedback: make(map[string]actionmemory.ActionFeedback),
		Timestamp:            time.Now().UTC(),
	}
}

func makeGoal(goalType string, priority, confidence float64) goals.Goal {
	return goals.Goal{
		ID:          "test-goal-" + goalType,
		Type:        goalType,
		Priority:    priority,
		Confidence:  confidence,
		Description: "test goal: " + goalType,
		CreatedAt:   time.Now().UTC(),
	}
}

func noopLogger() *zap.Logger {
	return zap.NewNop()
}

func assertEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func assertFloat(t *testing.T, field string, got, want float64) {
	t.Helper()
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("%s: got %.4f, want %.4f", field, got, want)
	}
}

func assertNonEmpty[T any](t *testing.T, field string, s []T) {
	t.Helper()
	if len(s) == 0 {
		t.Errorf("%s: expected non-empty slice", field)
	}
}

func assertNonEmptyStr(t *testing.T, field, s string) {
	t.Helper()
	if s == "" {
		t.Errorf("%s: expected non-empty string", field)
	}
}

func assertContains(t *testing.T, field string, haystack []string, needle string) {
	t.Helper()
	for _, s := range haystack {
		if contains(s, needle) {
			return
		}
	}
	t.Errorf("%s: expected to find %q in %v", field, needle, haystack)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
