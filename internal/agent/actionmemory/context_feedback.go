package actionmemory

import "fmt"

// contextWeightMax limits the contextual feedback weight when blending
// with global feedback. Contextual data can contribute at most 70% of
// the combined signal; global contributes at least 30%.
const contextWeightMax = 0.70

// contextRecordToActionMemory converts a ContextMemoryRecord to an
// ActionMemoryRecord so that GenerateFeedback can be reused unchanged.
func contextRecordToActionMemory(r *ContextMemoryRecord) *ActionMemoryRecord {
	if r == nil {
		return nil
	}
	return &ActionMemoryRecord{
		ActionType:  r.ActionType,
		TotalRuns:   r.TotalRuns,
		SuccessRuns: r.SuccessRuns,
		FailureRuns: r.FailureRuns,
		NeutralRuns: r.NeutralRuns,
		SuccessRate: r.SuccessRate,
		FailureRate: r.FailureRate,
		LastUpdated: r.LastUpdated,
	}
}

// ResolveContextualFeedback resolves the best contextual feedback for an
// action using the in-memory fallback chain: exact → partial → nil.
//
// Returns nil when no contextual data is available — the caller must
// fall back to global feedback.
func ResolveContextualFeedback(records []ContextMemoryRecord, actionType, goalType, failureBucket, backlogBucket string) *ContextualFeedback {
	// 1. Exact match: all 5 dimensions.
	for i := range records {
		r := &records[i]
		if r.ActionType == actionType && r.GoalType == goalType &&
			r.FailureBucket == failureBucket && r.BacklogBucket == backlogBucket {
			fb := GenerateFeedback(contextRecordToActionMemory(r))
			return &ContextualFeedback{
				ActionFeedback: fb,
				ContextMatch:   "exact",
			}
		}
	}

	// 2. Partial match: aggregate across action_type + goal_type.
	var totalRuns, successRuns, failureRuns, neutralRuns int
	for i := range records {
		r := &records[i]
		if r.ActionType == actionType && r.GoalType == goalType {
			totalRuns += r.TotalRuns
			successRuns += r.SuccessRuns
			failureRuns += r.FailureRuns
			neutralRuns += r.NeutralRuns
		}
	}
	if totalRuns > 0 {
		agg := &ActionMemoryRecord{
			ActionType:  actionType,
			TotalRuns:   totalRuns,
			SuccessRuns: successRuns,
			FailureRuns: failureRuns,
			NeutralRuns: neutralRuns,
			SuccessRate: float64(successRuns) / float64(totalRuns),
			FailureRate: float64(failureRuns) / float64(totalRuns),
		}
		fb := GenerateFeedback(agg)
		return &ContextualFeedback{
			ActionFeedback: fb,
			ContextMatch:   "partial",
		}
	}

	return nil
}

// BlendFeedbackAdjustment computes a blended score adjustment from
// contextual and global feedback signals. The contextual weight is
// capped at contextWeightMax (70%).
//
// Returns the score adjustment and a human-readable reasoning string.
func BlendFeedbackAdjustment(contextual *ContextualFeedback, global *ActionFeedback, avoidPenalty, preferBoost float64) (adjustment float64, reasoning string) {
	ctxAdj := 0.0
	globalAdj := 0.0

	hasCtx := contextual != nil &&
		contextual.Recommendation != RecommendInsufficientData &&
		contextual.Recommendation != RecommendNeutral
	hasGlobal := global != nil &&
		global.Recommendation != RecommendInsufficientData &&
		global.Recommendation != RecommendNeutral

	if hasCtx {
		ctxAdj = feedbackAdjustment(contextual.Recommendation, avoidPenalty, preferBoost)
	}
	if hasGlobal {
		globalAdj = feedbackAdjustment(global.Recommendation, avoidPenalty, preferBoost)
	}

	switch {
	case hasCtx && hasGlobal:
		blended := contextWeightMax*ctxAdj + (1.0-contextWeightMax)*globalAdj
		return blended, fmt.Sprintf(
			"blended ctx=%s(%.2f) + global=%s(%.2f) → %.2f (match=%s)",
			contextual.Recommendation, ctxAdj,
			global.Recommendation, globalAdj,
			blended, contextual.ContextMatch,
		)

	case hasCtx:
		capped := contextWeightMax * ctxAdj
		return capped, fmt.Sprintf(
			"contextual %s capped %.0f%%: %.2f (match=%s, n=%d)",
			contextual.Recommendation, contextWeightMax*100, capped,
			contextual.ContextMatch, contextual.SampleSize,
		)

	case hasGlobal:
		return globalAdj, fmt.Sprintf(
			"global %s: %.2f (n=%d)",
			global.Recommendation, globalAdj, global.SampleSize,
		)

	default:
		return 0, ""
	}
}

// feedbackAdjustment converts a recommendation to a numeric score adjustment.
func feedbackAdjustment(rec Recommendation, avoidPenalty, preferBoost float64) float64 {
	switch rec {
	case RecommendAvoidAction:
		return -avoidPenalty
	case RecommendPreferAction:
		return preferBoost
	default:
		return 0
	}
}

// FeedbackAdjustmentValue is the exported version of feedbackAdjustment.
func FeedbackAdjustmentValue(rec Recommendation, avoidPenalty, preferBoost float64) float64 {
	return feedbackAdjustment(rec, avoidPenalty, preferBoost)
}
