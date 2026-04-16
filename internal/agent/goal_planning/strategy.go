package goal_planning

// SelectStrategy determines the best strategy for a subgoal based on
// its execution history. This is a deterministic, pure function.
func SelectStrategy(sg Subgoal, objectiveDelta float64) Strategy {
	// High risk from objective penalty → defer.
	if objectiveDelta < -ObjectivePenaltyThreshold {
		return StrategyDeferHighRisk
	}

	// Repeated failures → change approach.
	if sg.FailureCount >= RepeatedFailureThreshold {
		return StrategyReduceFailure
	}

	// Mixed results with some success → diversify.
	if sg.FailureCount > 0 && sg.SuccessCount > 0 {
		return StrategyDiversify
	}

	// Success path → exploit.
	return StrategyExploitSuccess
}

// ApplyStrategyToEmission adjusts a task emission based on the selected strategy.
// Returns the modified emission. This is a pure function.
func ApplyStrategyToEmission(em TaskEmission, strategy Strategy) TaskEmission {
	switch strategy {
	case StrategyDeferHighRisk:
		// Reduce priority and increase risk estimate for deferred items.
		em.Priority = clamp01(em.Priority * 0.60)
		em.RiskLevel = clamp01(em.RiskLevel + 0.20)
	case StrategyReduceFailure:
		// Boost priority slightly to try alternate approach, reduce expected value.
		em.ExpectedValue = clamp01(em.ExpectedValue * 0.70)
	case StrategyDiversify:
		// Keep moderate priority, slightly boost risk awareness.
		em.RiskLevel = clamp01(em.RiskLevel + 0.10)
	case StrategyExploitSuccess:
		// Boost priority for proven paths.
		em.Priority = clamp01(em.Priority + SuccessReinforcementBoost)
	}
	em.StrategyType = string(strategy)
	return em
}

// ShouldReplan determines whether a subgoal needs replanning based on
// its execution history and objective state.
func ShouldReplan(sg Subgoal, objectiveDelta float64) (bool, ReplanTrigger) {
	// Objective penalty → immediate replan.
	if objectiveDelta < -ObjectivePenaltyThreshold {
		return true, TriggerObjectivePenalty
	}

	// Repeated failure → replan with different strategy.
	if sg.FailureCount >= RepeatedFailureThreshold {
		return true, TriggerRepeatedFailure
	}

	// Single failure → replan subgoal.
	if sg.FailureCount > 0 && sg.SuccessCount == 0 {
		return true, TriggerExecFailure
	}

	// Strong success → reinforce (not a replan trigger but a signal).
	if sg.SuccessCount >= 3 && sg.FailureCount == 0 {
		return true, TriggerReinforcement
	}

	return false, ""
}
