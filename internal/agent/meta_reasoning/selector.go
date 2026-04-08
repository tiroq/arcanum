package meta_reasoning

// SelectMode chooses a DecisionMode based on deterministic rules applied
// to the current signals. Rule evaluation order is fixed:
//
//  1. Conservative — safe_mode OR failure_rate > 0.5
//  2. Direct — strong confidence (>0.8), low risk (<0.2), sufficient path history
//  3. Exploratory — stagnation OR high missed_win signals
//  4. Graph — default fallback
//
// Deterministic: same inputs always produce the same mode.
func SelectMode(input MetaInput) ModeDecision {
	// Rule 1: Conservative — system is in safe_mode or high failure rate.
	if input.StabilityMode == "safe_mode" {
		return ModeDecision{
			Mode:       ModeConservative,
			Confidence: 1.0,
			Reason:     "stability_safe_mode",
		}
	}
	if input.FailureRate > HighFailureRateThreshold {
		return ModeDecision{
			Mode:       ModeConservative,
			Confidence: clamp01(input.FailureRate),
			Reason:     "high_failure_rate",
		}
	}

	// Rule 2: Direct — strong signal, low risk, sufficient history.
	if input.Confidence > StrongConfidenceThreshold &&
		input.Risk < LowRiskThreshold &&
		input.PathSampleSize >= MinPathSamplesForDirect {
		return ModeDecision{
			Mode:       ModeDirect,
			Confidence: clamp01(input.Confidence),
			Reason:     "strong_signal_low_risk",
		}
	}

	// Rule 3: Exploratory — stagnation or high missed-win count.
	if input.MissedWinCount >= MissedWinThreshold {
		return ModeDecision{
			Mode:       ModeExploratory,
			Confidence: 0.7,
			Reason:     "high_missed_wins",
		}
	}
	if input.RecentNoopRate > StagnationNoopThreshold {
		return ModeDecision{
			Mode:       ModeExploratory,
			Confidence: 0.6,
			Reason:     "stagnation_noop_rate",
		}
	}
	if input.RecentLowValueRate > StagnationLowValueThreshold {
		return ModeDecision{
			Mode:       ModeExploratory,
			Confidence: 0.6,
			Reason:     "stagnation_low_value_rate",
		}
	}

	// Rule 4: Graph — default reasoning mode.
	return ModeDecision{
		Mode:       ModeGraph,
		Confidence: 0.5,
		Reason:     "default_graph_mode",
	}
}

// SelectModeWithScoring combines deterministic rule-based selection with
// historical scoring and inertia. The rule-based selection provides the
// primary mode; scoring and inertia refine it when the rule result is
// the default graph mode.
//
// Flow:
//  1. Apply deterministic rules (SelectMode)
//  2. If rule result is NOT graph (i.e., a specific trigger fired), use it directly
//  3. If rule result IS graph, check if scoring with inertia suggests a better mode
//  4. Only override graph if the scored alternative beats graph by > InertiaThreshold
func SelectModeWithScoring(input MetaInput, memoryByMode map[DecisionMode]*ModeMemoryRecord) ModeDecision {
	ruleDecision := SelectMode(input)

	// Hard rules (conservative, direct, exploratory) always take precedence.
	if ruleDecision.Mode != ModeGraph {
		return ruleDecision
	}

	// For the default case, check if historical scoring suggests a different mode.
	scores := ScoreAllModes(memoryByMode, input)
	scores = ApplyInertia(scores, input.LastMode)

	// Find the best-scoring mode.
	bestScore := ModeScore{Mode: ModeGraph, Score: -1}
	graphScore := ModeScore{Mode: ModeGraph, Score: 0}
	for _, s := range scores {
		if s.Mode == ModeGraph {
			graphScore = s
		}
		if s.Score > bestScore.Score {
			bestScore = s
		}
	}

	// Only override graph if the best scoring mode is significantly better.
	if bestScore.Mode != ModeGraph && (bestScore.Score-graphScore.Score) > InertiaThreshold {
		return ModeDecision{
			Mode:       bestScore.Mode,
			Confidence: clamp01(bestScore.Score),
			Reason:     "scoring_override_" + string(bestScore.Mode),
		}
	}

	return ruleDecision
}
