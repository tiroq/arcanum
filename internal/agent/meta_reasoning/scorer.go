package meta_reasoning

// ScoreMode computes a ModeScore for a given mode using historical memory and current signals.
//
// ModeScore = SuccessRate * 0.5 + Confidence * 0.3 - Risk * 0.2
//
// Deterministic: same inputs always produce the same score.
func ScoreMode(mode DecisionMode, memory *ModeMemoryRecord, input MetaInput) ModeScore {
	memoryRate := 0.5 // neutral default when no memory
	if memory != nil && memory.SelectionCount > 0 {
		memoryRate = memory.SuccessRate
	}

	score := memoryRate*0.5 + input.Confidence*0.3 - input.Risk*0.2
	score = clamp01(score)

	return ModeScore{
		Mode:       mode,
		Score:      score,
		MemoryRate: memoryRate,
		Confidence: input.Confidence,
		Risk:       input.Risk,
	}
}

// ScoreAllModes computes scores for all modes given their memory records.
// memoryByMode maps DecisionMode → *ModeMemoryRecord (nil when no history exists).
func ScoreAllModes(memoryByMode map[DecisionMode]*ModeMemoryRecord, input MetaInput) []ModeScore {
	scores := make([]ModeScore, 0, len(AllModes))
	for _, mode := range AllModes {
		mem := memoryByMode[mode]
		scores = append(scores, ScoreMode(mode, mem, input))
	}
	return scores
}

// ApplyInertia adjusts scores to prevent frequent mode switching.
// If the candidate mode matches lastMode and the gap to the best alternative
// is below InertiaThreshold, the candidate receives an InertiaBoost.
// Returns a new slice (does not mutate input).
func ApplyInertia(scores []ModeScore, lastMode *DecisionMode) []ModeScore {
	if lastMode == nil || len(scores) == 0 {
		return scores
	}

	result := make([]ModeScore, len(scores))
	copy(result, scores)

	// Find the current mode's index and the best score among other modes.
	lastIdx := -1
	bestOtherScore := 0.0
	for i, s := range result {
		if s.Mode == *lastMode {
			lastIdx = i
		} else {
			if s.Score > bestOtherScore {
				bestOtherScore = s.Score
			}
		}
	}

	if lastIdx < 0 {
		return result
	}

	gap := bestOtherScore - result[lastIdx].Score
	if gap > 0 && gap < InertiaThreshold {
		result[lastIdx].Score = clamp01(result[lastIdx].Score + InertiaBoost)
	}

	return result
}

// clamp01 restricts a value to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
