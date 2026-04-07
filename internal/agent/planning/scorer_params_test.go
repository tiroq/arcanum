package planning

import (
	"testing"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// TestScoreCandidateWithParams_CustomNoopPenalty verifies that
// ScoreCandidateWithParams uses the supplied NoopBasePenalty.
func TestScoreCandidateWithParams_CustomNoopPenalty(t *testing.T) {
	pctx := emptyContext()
	params := DefaultScoringParams()
	params.NoopBasePenalty = 0.10 // half of default

	c := ScoreCandidateWithParams(PlannedActionCandidate{ActionType: "noop"}, 0.5, 0.9, pctx, params)
	// base 0.65 - 0.10 = 0.55
	assertFloat(t, "custom noop", c.Score, 0.55)
}

// TestScoreCandidateWithParams_CustomFeedbackAvoid verifies dynamic avoid penalty.
func TestScoreCandidateWithParams_CustomFeedbackAvoid(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.2,
		FailureRate:    0.6,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendAvoidAction,
	}

	params := DefaultScoringParams()
	params.FeedbackAvoidPenalty = 0.20 // half of default 0.40

	c := ScoreCandidateWithParams(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx, params)
	// base 0.65 - avoid 0.20 = 0.45
	assertFloat(t, "custom avoid", c.Score, 0.45)
}

// TestScoreCandidateWithParams_CustomFeedbackPrefer verifies dynamic prefer boost.
func TestScoreCandidateWithParams_CustomFeedbackPrefer(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.8,
		FailureRate:    0.1,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendPreferAction,
	}

	params := DefaultScoringParams()
	params.FeedbackPreferBoost = 0.10 // less than default 0.25

	c := ScoreCandidateWithParams(PlannedActionCandidate{ActionType: "retry_job"}, 0.5, 0.9, pctx, params)
	// base 0.65 + prefer 0.10 = 0.75
	assertFloat(t, "custom prefer", c.Score, 0.75)
}

// TestScoreCandidateWithParams_DefaultsMatchScoreCandidate verifies backward compat.
func TestScoreCandidateWithParams_DefaultsMatchScoreCandidate(t *testing.T) {
	pctx := emptyContext()
	pctx.RecentActionFeedback["retry_job"] = actionmemory.ActionFeedback{
		ActionType:     "retry_job",
		SuccessRate:    0.8,
		FailureRate:    0.1,
		SampleSize:     10,
		Recommendation: actionmemory.RecommendPreferAction,
	}

	old := ScoreCandidate(PlannedActionCandidate{ActionType: "retry_job"}, 0.7, 0.85, pctx)
	new := ScoreCandidateWithParams(PlannedActionCandidate{ActionType: "retry_job"}, 0.7, 0.85, pctx, DefaultScoringParams())

	if old.Score != new.Score {
		t.Errorf("backward compat: ScoreCandidate=%.4f vs WithParams=%.4f", old.Score, new.Score)
	}
	if old.Confidence != new.Confidence {
		t.Errorf("backward compat confidence: %.4f vs %.4f", old.Confidence, new.Confidence)
	}
}
