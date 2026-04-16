package goal_planning

// SelectStrategy determines the best strategy for a subgoal based on
// its execution history. This is a deterministic, pure function.
func SelectStrategy(sg Subgoal, objectiveDelta float64) Strategy {
	return SelectStrategyWithVector(sg, objectiveDelta, 0, 0)
}

// SelectStrategyWithVector determines the best strategy factoring in vector
// exploration level and risk tolerance. Pure function.
//
// explorationLevel > 0.50 biases toward diversify over exploit.
// riskTolerance > 0.50 lowers the threshold for deferring high-risk subgoals.
func SelectStrategyWithVector(sg Subgoal, objectiveDelta, explorationLevel, riskTolerance float64) Strategy {
	// High risk from objective penalty → defer, unless risk tolerance is high.
	deferThreshold := -ObjectivePenaltyThreshold
	if riskTolerance > 0 {
		// Higher risk tolerance raises the threshold (makes deferral harder to trigger).
		// At baseline (0.30): factor = 1.0, threshold unchanged.
		// At max (1.00): factor = 1.0 + (1.0-0.30)*0.60 = 1.42, threshold = -0.071.
		deferThreshold *= (1.0 + (riskTolerance-0.30)*0.60)
	}
	if objectiveDelta < deferThreshold {
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

	// High exploration level → prefer diversify even on clean record.
	if explorationLevel > 0.50 && sg.SuccessCount <= 1 {
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
