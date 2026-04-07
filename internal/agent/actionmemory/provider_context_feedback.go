package actionmemory

import "fmt"

// providerContextWeightMax limits the provider-context weight when blending.
// Provider-context can contribute at most 60% — leaving room for contextual
// and global layers in the cascade.
const providerContextWeightMax = 0.60

// providerContextRecordToActionMemory converts a ProviderContextMemoryRecord
// to an ActionMemoryRecord so that GenerateFeedback can be reused.
func providerContextRecordToActionMemory(r *ProviderContextMemoryRecord) *ActionMemoryRecord {
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

// ResolveProviderContextFeedback resolves the best provider-context feedback
// using the fallback chain: exact 7-dim → partial (action+goal+provider) → nil.
//
// Returns nil when no provider-context data is available — the caller must
// fall back to contextual feedback.
func ResolveProviderContextFeedback(records []ProviderContextMemoryRecord, actionType, goalType, providerName, modelRole, failureBucket, backlogBucket string) *ContextualFeedback {
	if providerName == "" {
		return nil
	}

	// 1. Exact match: all 7 dimensions.
	for i := range records {
		r := &records[i]
		if r.ActionType == actionType && r.GoalType == goalType &&
			r.ProviderName == providerName && r.ModelRole == modelRole &&
			r.FailureBucket == failureBucket && r.BacklogBucket == backlogBucket {
			fb := GenerateFeedback(providerContextRecordToActionMemory(r))
			return &ContextualFeedback{
				ActionFeedback: fb,
				ContextMatch:   "provider_exact",
			}
		}
	}

	// 2. Partial match: aggregate across action_type + goal_type + provider_name.
	var totalRuns, successRuns, failureRuns, neutralRuns int
	for i := range records {
		r := &records[i]
		if r.ActionType == actionType && r.GoalType == goalType && r.ProviderName == providerName {
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
			ContextMatch:   "provider_partial",
		}
	}

	return nil
}

// BlendProviderFeedbackAdjustment computes a blended score adjustment from
// provider-context feedback and a fallback signal (contextual or global).
// Provider-context weight is capped at providerContextWeightMax (60%).
func BlendProviderFeedbackAdjustment(providerFb *ContextualFeedback, fallbackAdj float64, fallbackReason string, avoidPenalty, preferBoost float64) (adjustment float64, reasoning string) {
	if providerFb == nil {
		return fallbackAdj, fallbackReason
	}

	hasProviderSignal := providerFb.Recommendation != RecommendInsufficientData &&
		providerFb.Recommendation != RecommendNeutral

	if !hasProviderSignal {
		return fallbackAdj, fallbackReason
	}

	provAdj := feedbackAdjustment(providerFb.Recommendation, avoidPenalty, preferBoost)

	if fallbackAdj == 0 && fallbackReason == "" {
		// No fallback signal — use provider-context only (capped).
		capped := providerContextWeightMax * provAdj
		return capped, fmt.Sprintf(
			"provider-context %s capped %.0f%%: %.2f (match=%s, n=%d)",
			providerFb.Recommendation, providerContextWeightMax*100, capped,
			providerFb.ContextMatch, providerFb.SampleSize,
		)
	}

	// Blend: provider-context weight + remaining weight to fallback.
	blended := providerContextWeightMax*provAdj + (1.0-providerContextWeightMax)*fallbackAdj
	return blended, fmt.Sprintf(
		"blended provider=%s(%.2f) + fallback(%.2f) → %.2f (match=%s)",
		providerFb.Recommendation, provAdj,
		fallbackAdj, blended, providerFb.ContextMatch,
	)
}
