package strategy

// --- Portfolio Selection ---
//
// SelectFromPortfolio chooses the best strategy from enriched portfolio candidates.
// Rules:
//  1. Sort by FinalScore DESC
//  2. Reject score ≤ 0
//  3. Tie → simpler strategy wins
//  4. If all invalid → fallback to noop
//
// Deterministic: same inputs always produce same selection.

// PortfolioSelection captures the portfolio competition result.
type PortfolioSelection struct {
	Candidates      []StrategyCandidate `json:"candidates"`
	Selected        *StrategyCandidate  `json:"selected,omitempty"`
	ExplorationPick *StrategyCandidate  `json:"exploration_pick,omitempty"`
	Reason          string              `json:"reason"`
	ExplorationUsed bool                `json:"exploration_used"`
}

// PortfolioSelectConfig controls selection behavior.
type PortfolioSelectConfig struct {
	// ShouldExplore: deterministic toggle for exploration override.
	ShouldExplore bool
	// StabilityMode: when safe_mode, only allow safest strategies.
	StabilityMode string
}

// SelectFromPortfolio selects the best strategy from portfolio candidates.
func SelectFromPortfolio(candidates []StrategyCandidate, config PortfolioSelectConfig) PortfolioSelection {
	result := PortfolioSelection{
		Candidates: candidates,
	}

	if len(candidates) == 0 {
		result.Reason = "no_candidates"
		return result
	}

	// Sort by FinalScore descending, tie-break by fewer steps, then name.
	sortPortfolioCandidates(candidates)

	// In safe_mode: filter to only safe strategies.
	if config.StabilityMode == "safe_mode" {
		var safe []StrategyCandidate
		for _, c := range candidates {
			if c.StrategyType == StrategyNoop || c.StrategyType == StrategyRecommendOnly {
				safe = append(safe, c)
			}
		}
		if len(safe) > 0 {
			candidates = safe
			result.Reason = "safe_mode_filtered"
		}
	}

	// Filter: reject FinalScore ≤ 0.
	var viable []StrategyCandidate
	for _, c := range candidates {
		if c.FinalScore > 0 || c.StrategyType == StrategyNoop {
			viable = append(viable, c)
		}
	}

	if len(viable) == 0 {
		// Fallback: find noop in original list.
		for i := range candidates {
			if candidates[i].StrategyType == StrategyNoop {
				result.Selected = &candidates[i]
				result.Reason = "all_rejected_fallback_noop"
				return result
			}
		}
		result.Reason = "all_rejected_no_fallback"
		return result
	}

	// Apply simplicity bias: when top two are close, prefer simpler.
	best := viable[0]
	if len(viable) > 1 {
		second := viable[1]
		diff := best.FinalScore - second.FinalScore
		if diff < SimplicityBias && second.Plan != nil && best.Plan != nil &&
			second.Plan.StepCount() < best.Plan.StepCount() {
			best = second
			if result.Reason == "" {
				result.Reason = "simplicity_bias"
			}
		}
	}

	if result.Reason == "" {
		result.Reason = "highest_final_score"
	}

	result.Selected = &best

	// Exploration override: deterministic toggle selects second-best.
	if config.ShouldExplore && len(viable) > 1 {
		secondBest := findSecondBest(viable, best)
		if secondBest != nil && secondBest.FinalScore > 0 {
			result.ExplorationPick = secondBest
			result.Selected = secondBest
			result.ExplorationUsed = true
			result.Reason = "exploration_override_second_best"
		}
	}

	return result
}

// findSecondBest returns the best candidate that is NOT the selected one.
func findSecondBest(viable []StrategyCandidate, selected StrategyCandidate) *StrategyCandidate {
	for i := range viable {
		if viable[i].PlanID != selected.PlanID {
			return &viable[i]
		}
	}
	return nil
}

// sortPortfolioCandidates sorts by FinalScore desc, then step count asc, then name asc.
func sortPortfolioCandidates(candidates []StrategyCandidate) {
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0; j-- {
			if shouldSwapCandidate(candidates[j], candidates[j-1]) {
				candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
			} else {
				break
			}
		}
	}
}

// shouldSwapCandidate returns true if a should rank above b.
func shouldSwapCandidate(a, b StrategyCandidate) bool {
	if a.FinalScore != b.FinalScore {
		return a.FinalScore > b.FinalScore
	}
	// Tie-break: fewer steps first (simpler strategy wins).
	aSteps, bSteps := 1, 1
	if a.Plan != nil {
		aSteps = a.Plan.StepCount()
	}
	if b.Plan != nil {
		bSteps = b.Plan.StepCount()
	}
	if aSteps != bSteps {
		return aSteps < bSteps
	}
	return string(a.StrategyType) < string(b.StrategyType)
}
