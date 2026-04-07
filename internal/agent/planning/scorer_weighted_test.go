package planning

import (
	"testing"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// --- Weighted Path Integration Tests ---

// TestScorer_WeightedPath_FreshAvoidFullPenalty verifies that fresh strong
// evidence produces full penalty via the weighted path.
func TestScorer_WeightedPath_FreshAvoidFullPenalty(t *testing.T) {
	now := time.Now().UTC()
	pctx := PlanningContext{
		RecentActionFeedback: map[string]actionmemory.ActionFeedback{
			"retry_job": {
				ActionType:     "retry_job",
				SampleSize:     20,
				FailureRate:    0.60,
				Recommendation: actionmemory.RecommendAvoidAction,
				LastUpdated:    now.Add(-30 * time.Minute),
			},
		},
		Timestamp: now,
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// base=0.65, fresh+strong avoid should produce near-full penalty.
	// Confidence=sqrt(1.0*1.0)=1.0, adj=-0.40*1.0=-0.40.
	assertFloat(t, "weighted fresh avoid", c.Score, 0.25)
	assertContains(t, "reasoning", c.Reasoning, "hierarchical")
}

// TestScorer_WeightedPath_StaleAvoidReducedPenalty verifies stale evidence
// reduces the penalty via confidence scaling.
func TestScorer_WeightedPath_StaleAvoidReducedPenalty(t *testing.T) {
	now := time.Now().UTC()
	pctx := PlanningContext{
		RecentActionFeedback: map[string]actionmemory.ActionFeedback{
			"retry_job": {
				ActionType:     "retry_job",
				SampleSize:     20,
				FailureRate:    0.60,
				Recommendation: actionmemory.RecommendAvoidAction,
				LastUpdated:    now.Add(-14 * 24 * time.Hour), // 14 days old
			},
		},
		Timestamp: now,
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Stale (14d) recency=0.40, samples=20→1.0, confidence=sqrt(0.40)≈0.632
	// adj=-0.40*0.632≈-0.253. Score=0.65-0.253≈0.397
	if c.Score <= 0.25 {
		t.Errorf("stale penalty should be less than fresh (0.25), got %.3f", c.Score)
	}
	if c.Score >= 0.65 {
		t.Errorf("should still apply some penalty, got %.3f", c.Score)
	}
}

// TestScorer_WeightedPath_FreshProviderWinsOverStaleGlobal verifies
// provider-specific evidence wins when fresher than global.
func TestScorer_WeightedPath_FreshProviderWinsOverStaleGlobal(t *testing.T) {
	now := time.Now().UTC()
	pctx := PlanningContext{
		RecentActionFeedback: map[string]actionmemory.ActionFeedback{
			"retry_job": {
				ActionType:     "retry_job",
				SampleSize:     50,
				FailureRate:    0.60,
				Recommendation: actionmemory.RecommendAvoidAction,
				LastUpdated:    now.Add(-10 * 24 * time.Hour), // stale global
			},
		},
		ProviderContextRecords: []actionmemory.ProviderContextMemoryRecord{{
			ActionType:    "retry_job",
			GoalType:      "reduce_retry_rate",
			ProviderName:  "ollama",
			ModelRole:     "fast",
			FailureBucket: "low",
			BacklogBucket: "low",
			TotalRuns:     15,
			SuccessRuns:   12,
			FailureRuns:   3,
			SuccessRate:   0.80,
			FailureRate:   0.20,
			LastUpdated:   now.Add(-20 * time.Minute), // fresh provider
		}},
		ProviderName:  "ollama",
		ModelRole:     "fast",
		FailureBucket: "low",
		BacklogBucket: "low",
		Timestamp:     now,
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Fresh provider prefer should win: score should be above base.
	if c.Score <= 0.65 {
		t.Errorf("fresh provider prefer should boost score above base 0.65, got %.3f", c.Score)
	}
}

// TestScorer_WeightedPath_CategoricalFallback verifies that when records
// have no LastUpdated (zero timestamps), the categorical path is used.
func TestScorer_WeightedPath_CategoricalFallback(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
		// No LastUpdated → zero time → categorical fallback
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Should use categorical: base 0.65 - 0.40 = 0.25
	assertFloat(t, "categorical fallback", c.Score, 0.25)
	assertContains(t, "reasoning", c.Reasoning, "avoid_action")
}

// TestScorer_WeightedPath_SmallSampleReducesPenalty verifies that tiny
// samples reduce the confidence-weighted penalty.
func TestScorer_WeightedPath_SmallSampleReducesPenalty(t *testing.T) {
	now := time.Now().UTC()
	pctx := PlanningContext{
		RecentActionFeedback: map[string]actionmemory.ActionFeedback{
			"retry_job": {
				ActionType:     "retry_job",
				SampleSize:     3,
				FailureRate:    0.60,
				Recommendation: actionmemory.RecommendAvoidAction,
				LastUpdated:    now.Add(-30 * time.Minute),
			},
		},
		Timestamp: now,
	}

	c := ScoreCandidateWithParams(
		PlannedActionCandidate{ActionType: "retry_job", GoalType: "reduce_retry_rate"},
		0.5, 0.9, pctx, DefaultScoringParams(),
	)

	// Fresh but tiny: recency=1.0, sample=0.30, confidence=sqrt(0.30)≈0.548
	// adj=-0.40*0.548≈-0.219. Score=0.65-0.219≈0.43
	if c.Score <= 0.25 {
		t.Errorf("tiny sample should reduce penalty vs full (0.25), got %.3f", c.Score)
	}
	if c.Score >= 0.65 {
		t.Errorf("should still apply some penalty, got %.3f", c.Score)
	}
}
