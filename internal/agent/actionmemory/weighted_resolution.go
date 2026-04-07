package actionmemory

import "time"

// GatherWeightedCandidates collects WeightedFeedback from all memory layers
// for a given action/goal query and returns them sorted by FinalWeight.
//
// Layers queried:
//  1. provider-context (exact + partial)
//  2. contextual (exact + partial)
//  3. global
//
// Each candidate gets temporal decay and confidence computed at query time.
func GatherWeightedCandidates(
	providerRecords []ProviderContextMemoryRecord,
	contextRecords []ContextMemoryRecord,
	globalFeedback map[string]ActionFeedback,
	actionType, goalType string,
	providerName, modelRole string,
	failureBucket, backlogBucket string,
	now time.Time,
) []WeightedFeedback {
	var candidates []WeightedFeedback

	// --- Provider-context exact ---
	if providerName != "" {
		for i := range providerRecords {
			r := &providerRecords[i]
			if r.ActionType == actionType && r.GoalType == goalType &&
				r.ProviderName == providerName && r.ModelRole == modelRole &&
				r.FailureBucket == failureBucket && r.BacklogBucket == backlogBucket {
				fb := GenerateFeedback(providerContextRecordToActionMemory(r))
				candidates = append(candidates, BuildWeightedFeedback(fb, SourceProviderExact, r.LastUpdated, now))
				break // exact is unique
			}
		}
	}

	// --- Provider-context partial (action + goal + provider) ---
	if providerName != "" {
		var totalRuns, successRuns, failureRuns, neutralRuns int
		var latestUpdate time.Time
		for i := range providerRecords {
			r := &providerRecords[i]
			if r.ActionType == actionType && r.GoalType == goalType && r.ProviderName == providerName {
				totalRuns += r.TotalRuns
				successRuns += r.SuccessRuns
				failureRuns += r.FailureRuns
				neutralRuns += r.NeutralRuns
				if r.LastUpdated.After(latestUpdate) {
					latestUpdate = r.LastUpdated
				}
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
				LastUpdated: latestUpdate,
			}
			fb := GenerateFeedback(agg)
			candidates = append(candidates, BuildWeightedFeedback(fb, SourceProviderPartial, latestUpdate, now))
		}
	}

	// --- Contextual exact ---
	for i := range contextRecords {
		r := &contextRecords[i]
		if r.ActionType == actionType && r.GoalType == goalType &&
			r.FailureBucket == failureBucket && r.BacklogBucket == backlogBucket {
			fb := GenerateFeedback(contextRecordToActionMemory(r))
			candidates = append(candidates, BuildWeightedFeedback(fb, SourceContextExact, r.LastUpdated, now))
			break
		}
	}

	// --- Contextual partial (action + goal) ---
	{
		var totalRuns, successRuns, failureRuns, neutralRuns int
		var latestUpdate time.Time
		for i := range contextRecords {
			r := &contextRecords[i]
			if r.ActionType == actionType && r.GoalType == goalType {
				totalRuns += r.TotalRuns
				successRuns += r.SuccessRuns
				failureRuns += r.FailureRuns
				neutralRuns += r.NeutralRuns
				if r.LastUpdated.After(latestUpdate) {
					latestUpdate = r.LastUpdated
				}
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
				LastUpdated: latestUpdate,
			}
			fb := GenerateFeedback(agg)
			candidates = append(candidates, BuildWeightedFeedback(fb, SourceContextPartial, latestUpdate, now))
		}
	}

	// --- Global ---
	if gfb, ok := globalFeedback[actionType]; ok {
		candidates = append(candidates, BuildWeightedFeedback(gfb, SourceGlobal, gfb.LastUpdated, now))
	}

	return candidates
}
