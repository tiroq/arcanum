package scheduling

import "math"

// ScoreFit computes a bounded [0,1] fit score for placing a candidate into a slot.
//
// Formula (4 components, weighted):
//
//  1. value_component     = clamp(value_per_hour / normalizer, 0, 1) × 0.35
//     where value_per_hour = expected_value / max(effort, 0.5)
//     and normalizer = HighValuePerHourThreshold
//
//  2. urgency_component   = urgency × 0.25
//
//  3. effort_fit_component = clamp(1 - abs(effort - slotHours) / max(effort, slotHours), 0, 1) × 0.25
//     Perfect fit when effort ≈ slot duration. Poor fit when large mismatch.
//
//  4. load_component      = clamp(1 - owner_load_score, 0, 1) × 0.15
//
// Optional strategy boost:
//
//	strategy_boost = strategy_priority × StrategyPriorityBoostMax
//
// Final: clamp(base + strategy_boost, 0, 1)
func ScoreFit(candidate SchedulingCandidate, slot ScheduleSlot, ownerLoadScore float64) float64 {
	effort := math.Max(candidate.EstimatedEffortHours, 0.5)
	valuePerHour := 0.0
	if candidate.ExpectedValue > 0 {
		valuePerHour = candidate.ExpectedValue / effort
	}

	// 1. Value component (35%)
	normalizer := math.Max(HighValuePerHourThreshold, 1.0)
	valueComp := clamp(valuePerHour/normalizer, 0, 1) * WeightValuePerHour

	// 2. Urgency component (25%)
	urgencyComp := clamp(candidate.Urgency, 0, 1) * WeightUrgency

	// 3. Effort fit component (25%)
	slotHours := slot.DurationHours()
	effortFit := 0.0
	maxDim := math.Max(effort, slotHours)
	if maxDim > 0 {
		effortFit = 1.0 - math.Abs(effort-slotHours)/maxDim
	}
	effortFitComp := clamp(effortFit, 0, 1) * WeightEffortFit

	// 4. Load component (15%)
	loadComp := clamp(1.0-ownerLoadScore, 0, 1) * WeightLoadPenalty

	base := valueComp + urgencyComp + effortFitComp + loadComp

	// Strategy boost.
	strategyBoost := clamp(candidate.StrategyPriority, 0, 1) * StrategyPriorityBoostMax

	return clamp(base+strategyBoost, 0, 1)
}

// ScoreSlots scores all available slots for a candidate and returns sorted results.
// Returns slot scores sorted descending by fit score.
func ScoreSlots(candidate SchedulingCandidate, slots []ScheduleSlot, ownerLoadScore float64) []SlotScore {
	var scores []SlotScore
	for _, slot := range slots {
		if !slot.Available || slot.SlotType != SlotTypeWork {
			continue
		}
		score := ScoreFit(candidate, slot, ownerLoadScore)
		scores = append(scores, SlotScore{
			Slot:     slot,
			FitScore: score,
		})
	}

	// Sort by fit score descending, then by start time ascending (deterministic).
	sortSlotScores(scores)
	return scores
}

// sortSlotScores sorts by fit score DESC, then start time ASC for determinism.
func sortSlotScores(scores []SlotScore) {
	for i := 1; i < len(scores); i++ {
		for j := i; j > 0; j-- {
			swap := false
			if scores[j].FitScore > scores[j-1].FitScore {
				swap = true
			} else if scores[j].FitScore == scores[j-1].FitScore &&
				scores[j].Slot.StartTime.Before(scores[j-1].Slot.StartTime) {
				swap = true
			}
			if swap {
				scores[j], scores[j-1] = scores[j-1], scores[j]
			} else {
				break
			}
		}
	}
}

// RequiresReview determines whether a scheduling decision requires human review.
//
// Review is required for:
//   - external meetings (item_type or slot_type indicates meeting)
//   - calendar writes (any decision that will create a calendar event)
//   - any schedule change that touches family-blocked time adjacency
func RequiresReview(candidate SchedulingCandidate, slot ScheduleSlot, willWriteCalendar bool) (bool, string) {
	if slot.SlotType == SlotTypeMeeting {
		return true, "external meeting requires review"
	}
	if candidate.ItemType == "meeting" {
		return true, "meeting scheduling requires review"
	}
	if willWriteCalendar {
		return true, "calendar write requires approval"
	}
	return false, ""
}

// clamp restricts v to [min, max].
func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
