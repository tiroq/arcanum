package strategy

import (
	"time"

	"github.com/google/uuid"
)

// Select chooses the best strategy from scored candidates.
// Rules:
//  1. Reject strategies with utility below MinUtilityThreshold (unless noop).
//  2. Sort by expected utility descending.
//  3. If top two strategies have utility within SimplicityBias, prefer simpler.
//  4. If best is multi-step and only marginally better, prefer single-step.
//  5. Noop is always valid as a fallback.
//
// Deterministic: same inputs always produce same selection.
func Select(plans []StrategyPlan, goalID, goalType string, now time.Time) StrategyDecision {
	decision := StrategyDecision{
		GoalID:    goalID,
		GoalType:  goalType,
		CreatedAt: now,
	}

	if len(plans) == 0 {
		decision.Reason = "no_candidates"
		return decision
	}

	// Filter: reject below minimum utility (keep noop).
	var viable []StrategyPlan
	for _, p := range plans {
		if p.ExpectedUtility >= MinUtilityThreshold || p.StrategyType == StrategyNoop {
			viable = append(viable, p)
		}
	}

	if len(viable) == 0 {
		// Should not happen since noop is always present, but fail-safe.
		decision.CandidateStrategies = plans
		decision.Reason = "all_below_threshold"
		return decision
	}

	// Sort viable by expected utility descending, tie-break by fewer steps, then name.
	sortStrategies(viable)

	// Apply simplicity bias: if top two are close, prefer simpler.
	selected := viable[0]
	if len(viable) > 1 {
		second := viable[1]
		utilityDiff := selected.ExpectedUtility - second.ExpectedUtility
		if utilityDiff < SimplicityBias && second.StepCount() < selected.StepCount() {
			selected = second
			decision.Reason = "simplicity_bias: simpler strategy chosen"
		}
	}

	if decision.Reason == "" {
		decision.Reason = "highest_utility"
	}

	selected.Selected = true
	decision.SelectedStrategyID = selected.ID

	// Build candidate list with selected flag set.
	decision.CandidateStrategies = make([]StrategyPlan, len(viable))
	for i, p := range viable {
		if p.ID == selected.ID {
			decision.CandidateStrategies[i] = selected
		} else {
			decision.CandidateStrategies[i] = p
		}
	}

	return decision
}

// sortStrategies sorts by expected utility descending, then by fewer steps,
// then by strategy type name for determinism.
func sortStrategies(plans []StrategyPlan) {
	for i := 1; i < len(plans); i++ {
		for j := i; j > 0; j-- {
			if shouldSwap(plans[j], plans[j-1]) {
				plans[j], plans[j-1] = plans[j-1], plans[j]
			} else {
				break
			}
		}
	}
}

// shouldSwap returns true if a should rank above b.
func shouldSwap(a, b StrategyPlan) bool {
	if a.ExpectedUtility != b.ExpectedUtility {
		return a.ExpectedUtility > b.ExpectedUtility
	}
	if a.StepCount() != b.StepCount() {
		return a.StepCount() < b.StepCount()
	}
	return string(a.StrategyType) < string(b.StrategyType)
}

// SelectedPlan returns the selected plan from a decision, or nil if none.
func SelectedPlan(decision StrategyDecision) *StrategyPlan {
	if decision.SelectedStrategyID == uuid.Nil {
		return nil
	}
	for i := range decision.CandidateStrategies {
		if decision.CandidateStrategies[i].ID == decision.SelectedStrategyID {
			return &decision.CandidateStrategies[i]
		}
	}
	return nil
}
