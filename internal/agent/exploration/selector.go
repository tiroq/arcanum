package exploration

import (
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/planning"
)

// --- Trigger Detection ---

// ShouldExplore returns true when uncertainty-based exploration is justified
// based on the planning decision. Deterministic: same inputs → same output.
//
// Trigger conditions (all must hold):
//  1. top candidate confidence < TriggerConfidenceThreshold
//     OR score gap between top two < TriggerScoreGapThreshold
//  2. at least one non-selected candidate exists that isn't noop
func ShouldExplore(decision planning.PlanningDecision) (bool, string) {
	candidates := decision.Candidates
	if len(candidates) < 2 {
		return false, "too_few_candidates"
	}

	// Candidates are sorted descending by score from the planner.
	top := candidates[0]
	second := candidates[1]

	lowConfidence := top.Confidence < TriggerConfidenceThreshold
	smallGap := (top.Score - second.Score) < TriggerScoreGapThreshold

	if !lowConfidence && !smallGap {
		return false, "confidence_sufficient"
	}

	// There must be at least one non-noop alternative.
	hasAlternative := false
	for _, c := range candidates[1:] {
		if c.ActionType != "noop" && !c.Rejected {
			hasAlternative = true
			break
		}
	}
	if !hasAlternative {
		return false, "no_viable_alternative"
	}

	reason := ""
	if lowConfidence && smallGap {
		reason = "low_confidence_small_gap"
	} else if lowConfidence {
		reason = "low_confidence"
	} else {
		reason = "small_score_gap"
	}

	return true, reason
}

// --- Candidate Scoring ---

// ScoreExplorationCandidates evaluates all non-selected, non-noop candidates
// for exploration potential. Returns scored candidates sorted by ExplorationScore
// descending.
func ScoreExplorationCandidates(
	decision planning.PlanningDecision,
	globalFeedback map[string]actionmemory.ActionFeedback,
) []ExplorationCandidate {
	if len(decision.Candidates) < 2 {
		return nil
	}

	selected := decision.SelectedActionType
	var result []ExplorationCandidate

	for _, c := range decision.Candidates {
		// Skip the exploitation-selected action and noop.
		if c.ActionType == selected {
			continue
		}
		if c.ActionType == "noop" {
			continue
		}
		if c.Rejected {
			continue
		}

		ec := scoreCandidate(c, globalFeedback)
		if ec.ExplorationScore > 0 {
			result = append(result, ec)
		}
	}

	// Sort by ExplorationScore descending (deterministic: stable sort not needed,
	// but we ensure determinism with tie-breaking on action type).
	sortCandidates(result)
	return result
}

// scoreCandidate computes exploration scores for a single candidate.
func scoreCandidate(
	c planning.PlannedActionCandidate,
	globalFeedback map[string]actionmemory.ActionFeedback,
) ExplorationCandidate {
	fb, hasFeedback := globalFeedback[c.ActionType]

	novelty := computeNovelty(hasFeedback, fb)
	safety := computeSafety(hasFeedback, fb, c)
	uncertainty := computeUncertainty(c)

	// Exploration score: weighted combination.
	// novelty × 0.4 + safety × 0.3 + uncertainty × 0.3
	explorationScore := novelty*0.4 + safety*0.3 + uncertainty*0.3

	reason := ""
	if novelty > 0.5 {
		reason = "underexplored"
	} else if uncertainty > 0.5 {
		reason = "uncertain_evidence"
	}

	return ExplorationCandidate{
		ActionType:        c.ActionType,
		GoalType:          c.GoalType,
		BaseDecisionScore: c.Score,
		UncertaintyScore:  uncertainty,
		NoveltyScore:      novelty,
		SafetyScore:       safety,
		ExplorationScore:  explorationScore,
		Reason:            reason,
	}
}

// computeNovelty returns [0, 1]: higher when fewer samples exist.
func computeNovelty(hasFeedback bool, fb actionmemory.ActionFeedback) float64 {
	if !hasFeedback {
		return 1.0 // No data at all → maximum novelty.
	}
	if fb.SampleSize >= MaxSampleSizeForNovelty {
		return 0.0 // Enough samples → not novel.
	}
	// Linear: 0 samples → 1.0, MaxSampleSizeForNovelty-1 → 0.2
	return 1.0 - (float64(fb.SampleSize) / float64(MaxSampleSizeForNovelty) * 0.8)
}

// computeSafety returns [0, 1]: higher when there's no negative evidence.
func computeSafety(
	hasFeedback bool,
	fb actionmemory.ActionFeedback,
	c planning.PlannedActionCandidate,
) float64 {
	if !hasFeedback {
		return 0.8 // No evidence → moderately safe (unknown, not proven bad).
	}

	switch fb.Recommendation {
	case actionmemory.RecommendAvoidAction:
		// Unsafe: strong evidence of failure. Check confidence.
		conf := actionmemory.SampleWeight(fb.SampleSize)
		if conf >= SafetyAvoidConfidenceThreshold {
			return 0.0 // High confidence avoid → not safe.
		}
		return 0.3 // Low confidence avoid → slightly safe.
	case actionmemory.RecommendInsufficientData:
		return 0.8 // Not enough data → moderately safe.
	case actionmemory.RecommendNeutral:
		return 0.7 // Neutral performance → reasonably safe.
	case actionmemory.RecommendPreferAction:
		return 1.0 // Positive signal → safe.
	}
	return 0.5
}

// computeUncertainty returns [0, 1]: higher when evidence is weak/uncertain.
func computeUncertainty(c planning.PlannedActionCandidate) float64 {
	// Use the candidate's confidence from the planner (already aggregates
	// evidence quality from decay + sample weighting).
	if c.Confidence >= 0.80 {
		return 0.0 // High confidence → low uncertainty.
	}
	if c.Confidence <= 0.20 {
		return 1.0 // Very low confidence → maximum uncertainty.
	}
	// Linear interpolation: 0.20→1.0, 0.80→0.0
	return 1.0 - (c.Confidence-0.20)/(0.80-0.20)
}

// sortCandidates sorts by ExplorationScore descending, ties broken by
// ActionType for determinism.
func sortCandidates(cs []ExplorationCandidate) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			if cs[j].ExplorationScore > cs[j-1].ExplorationScore {
				cs[j], cs[j-1] = cs[j-1], cs[j]
			} else if cs[j].ExplorationScore == cs[j-1].ExplorationScore &&
				cs[j].ActionType < cs[j-1].ActionType {
				cs[j], cs[j-1] = cs[j-1], cs[j]
			} else {
				break
			}
		}
	}
}
