package goal_planning

import (
	"time"
)

// PlanTasks generates task emissions for active subgoals that need work.
// This is a deterministic, pure function — it produces emissions but does not
// execute them. The engine is responsible for persisting and emitting.
func PlanTasks(subgoals []Subgoal, now time.Time) []TaskEmission {
	var emissions []TaskEmission

	for _, sg := range subgoals {
		if sg.Status != SubgoalActive {
			continue
		}

		// Skip if already completed.
		if sg.ProgressScore >= MinProgressToComplete {
			continue
		}

		// Skip if task was emitted recently (cooldown).
		if !sg.LastTaskEmitted.IsZero() &&
			now.Sub(sg.LastTaskEmitted).Minutes() < TaskEmissionCooldownMinutes {
			continue
		}

		// Skip if dependency not met.
		if !IsDependencyMet(sg, subgoals) {
			continue
		}

		urgency := ComputeTaskUrgency(sg, now)
		priority := ComputeTaskPriority(urgency, sg.Priority, sg.ProgressScore)

		// Risk level: higher for lower-progress, high-priority subgoals.
		riskLevel := clamp01((1.0 - sg.ProgressScore) * 0.30)

		// Expected value: derived from goal priority.
		expectedValue := clamp01(sg.Priority * 0.80)

		emissions = append(emissions, TaskEmission{
			SubgoalID:     sg.ID,
			GoalID:        sg.GoalID,
			ActionType:    sg.PreferredAction,
			Urgency:       urgency,
			ExpectedValue: expectedValue,
			RiskLevel:     riskLevel,
			StrategyType:  sg.GoalID, // use goal ID as strategy type
			Priority:      priority,
		})
	}

	// Sort by priority descending (deterministic).
	sortEmissions(emissions)

	return emissions
}

// sortEmissions sorts emissions by priority descending, then by subgoal ID for determinism.
func sortEmissions(emissions []TaskEmission) {
	for i := 1; i < len(emissions); i++ {
		for j := i; j > 0; j-- {
			if emissions[j].Priority > emissions[j-1].Priority ||
				(emissions[j].Priority == emissions[j-1].Priority && emissions[j].SubgoalID < emissions[j-1].SubgoalID) {
				emissions[j], emissions[j-1] = emissions[j-1], emissions[j]
			} else {
				break
			}
		}
	}
}
