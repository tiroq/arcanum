package counterfactual

import "time"

// SimulateTopKPaths simulates the top K candidate paths using existing signals
// and returns their predicted outcomes. Deterministic and bounded.
//
// Parameters:
//   - decisionID: unique identifier for this decision
//   - goalType: goal family context
//   - pathScores: map[pathSignature] → current FinalScore
//   - pathLengths: map[pathSignature] → number of nodes in path
//   - signals: existing learning signals for simulation
//
// Returns a SimulationResult with predictions for up to MaxSimulatedPaths paths.
// If signals is nil, returns an empty result (fail-open).
func SimulateTopKPaths(
	decisionID string,
	goalType string,
	pathScores map[string]float64,
	pathLengths map[string]int,
	signals *SimulationSignals,
) SimulationResult {
	result := SimulationResult{
		DecisionID:  decisionID,
		GoalType:    goalType,
		Predictions: []PathPrediction{},
		CreatedAt:   time.Now().UTC(),
	}

	if signals == nil || len(pathScores) == 0 {
		return result
	}

	// Select top K paths by current score (deterministic: sorted by score DESC, signature ASC).
	topPaths := selectTopK(pathScores, MaxSimulatedPaths)

	for _, sig := range topPaths {
		pathLength := 1
		if l, ok := pathLengths[sig]; ok && l > 0 {
			pathLength = l
		}
		pred := simulatePath(sig, pathScores[sig], pathLength, signals)
		result.Predictions = append(result.Predictions, pred)
	}

	return result
}

// simulatePath produces a deterministic prediction for a single path.
func simulatePath(pathSignature string, currentScore float64, pathLength int, signals *SimulationSignals) PathPrediction {
	breakdown := make(map[string]float64)

	// --- ExpectedValue: weighted combination of existing signals ---
	totalWeight := 0.0
	weightedValue := 0.0

	// Path learning signal.
	if rec, ok := signals.PathFeedback[pathSignature]; ok {
		val := recommendationToValue(rec)
		weightedValue += val * SignalWeightPathLearning
		totalWeight += SignalWeightPathLearning
		breakdown["path_learning"] = val
	}

	// Comparative learning signal.
	if rec, ok := signals.ComparativeFeedback[pathSignature]; ok {
		val := recommendationToValue(rec)
		weightedValue += val * SignalWeightComparativeLearning
		totalWeight += SignalWeightComparativeLearning
		breakdown["comparative_learning"] = val
	}

	// Comparative win rate (direct signal).
	if wr, ok := signals.ComparativeWinRates[pathSignature]; ok {
		weightedValue += wr * SignalWeightComparativeLearning * 0.5
		totalWeight += SignalWeightComparativeLearning * 0.5
		breakdown["comparative_win_rate"] = wr
	}

	// Transition learning signal (aggregate across transitions in path).
	if signals.TransitionFeedback != nil {
		transVal := aggregateTransitionSignal(pathSignature, signals.TransitionFeedback)
		if transVal >= 0 {
			weightedValue += transVal * SignalWeightTransitionLearning
			totalWeight += SignalWeightTransitionLearning
			breakdown["transition_learning"] = transVal
		}
	}

	// Historical failure rate.
	if fr, ok := signals.HistoricalFailureRates[pathSignature]; ok {
		histVal := 1.0 - fr // invert: high failure = low value
		weightedValue += histVal * SignalWeightHistorical
		totalWeight += SignalWeightHistorical
		breakdown["historical"] = histVal
	}

	// Compute ExpectedValue.
	expectedValue := currentScore // default to current score if no signals
	if totalWeight > MinSignalConfidence {
		expectedValue = weightedValue / totalWeight
	}
	expectedValue = clamp01(expectedValue)

	// --- ExpectedRisk ---
	expectedRisk := computeExpectedRisk(pathSignature, pathLength, signals)
	breakdown["expected_risk"] = expectedRisk

	// --- Confidence: based on evidence availability ---
	confidence := computeConfidence(pathSignature, signals)
	breakdown["confidence"] = confidence

	return PathPrediction{
		PathSignature:   pathSignature,
		ExpectedValue:   expectedValue,
		ExpectedRisk:    expectedRisk,
		Confidence:      confidence,
		SourceBreakdown: breakdown,
	}
}

// computeExpectedRisk computes risk from historical failure rate, path length, and comparative loss rate.
func computeExpectedRisk(pathSignature string, pathLength int, signals *SimulationSignals) float64 {
	totalWeight := 0.0
	weightedRisk := 0.0

	// Historical failure rate.
	if fr, ok := signals.HistoricalFailureRates[pathSignature]; ok {
		weightedRisk += fr * RiskWeightHistorical
		totalWeight += RiskWeightHistorical
	}

	// Path length penalty.
	lengthRisk := 0.0
	if pathLength > 1 {
		lengthRisk = clamp01(float64(pathLength-1) * PathLengthRiskPenalty)
	}
	weightedRisk += lengthRisk * RiskWeightPathLength
	totalWeight += RiskWeightPathLength

	// Comparative loss rate.
	if lr, ok := signals.ComparativeLossRates[pathSignature]; ok {
		weightedRisk += lr * RiskWeightComparative
		totalWeight += RiskWeightComparative
	}

	if totalWeight < MinSignalConfidence {
		return 0.1 // default low risk
	}

	return clamp01(weightedRisk / totalWeight)
}

// computeConfidence estimates prediction confidence from signal availability.
func computeConfidence(pathSignature string, signals *SimulationSignals) float64 {
	signalCount := 0
	maxSignals := 5 // path, comparative, comparative_wr, transition, historical

	if _, ok := signals.PathFeedback[pathSignature]; ok {
		signalCount++
	}
	if _, ok := signals.ComparativeFeedback[pathSignature]; ok {
		signalCount++
	}
	if _, ok := signals.ComparativeWinRates[pathSignature]; ok {
		signalCount++
	}
	if signals.TransitionFeedback != nil && len(signals.TransitionFeedback) > 0 {
		signalCount++
	}
	if _, ok := signals.HistoricalFailureRates[pathSignature]; ok {
		signalCount++
	}

	if signalCount == 0 {
		return 0.1 // minimal confidence when no signals
	}

	return clamp01(float64(signalCount) / float64(maxSignals))
}

// recommendationToValue maps a recommendation string to a [0, 1] value.
func recommendationToValue(rec string) float64 {
	switch rec {
	case "prefer_path", "prefer_transition":
		return 0.8
	case "avoid_path", "avoid_transition":
		return 0.2
	case "underexplored_path":
		return 0.5
	default: // "neutral"
		return 0.5
	}
}

// aggregateTransitionSignal computes an average signal from transition feedback
// for transitions that match the path signature (actions separated by '>').
func aggregateTransitionSignal(pathSignature string, transitionFeedback map[string]string) float64 {
	// Parse path actions from signature.
	actions := splitPathSignature(pathSignature)
	if len(actions) < 2 {
		return -1 // no transitions to aggregate
	}

	total := 0.0
	count := 0
	for i := 0; i < len(actions)-1; i++ {
		tKey := actions[i] + "->" + actions[i+1]
		if rec, ok := transitionFeedback[tKey]; ok {
			total += recommendationToValue(rec)
			count++
		}
	}

	if count == 0 {
		return -1
	}

	return total / float64(count)
}

// splitPathSignature splits a path signature "a>b>c" into ["a", "b", "c"].
func splitPathSignature(sig string) []string {
	if sig == "" {
		return nil
	}
	result := []string{}
	current := ""
	for _, c := range sig {
		if c == '>' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// selectTopK selects the top K path signatures by score (deterministic).
// Ties broken by signature ascending (alphabetical).
func selectTopK(pathScores map[string]float64, k int) []string {
	// Collect all signatures.
	sigs := make([]string, 0, len(pathScores))
	for sig := range pathScores {
		sigs = append(sigs, sig)
	}

	// Insertion sort: stable, deterministic.
	for i := 1; i < len(sigs); i++ {
		key := sigs[i]
		j := i - 1
		for j >= 0 && (pathScores[sigs[j]] < pathScores[key] ||
			(pathScores[sigs[j]] == pathScores[key] && sigs[j] > key)) {
			sigs[j+1] = sigs[j]
			j--
		}
		sigs[j+1] = key
	}

	if len(sigs) > k {
		sigs = sigs[:k]
	}
	return sigs
}

// AdjustScoresWithPredictions adjusts path scores using simulation predictions.
// Returns a map[pathSignature] → adjusted score.
//
// AdjustedScore = OriginalScore + (PredictedValue - OriginalScore) × PredictionWeight
//
// If predictions is nil or empty, returns original scores unchanged (fail-open).
func AdjustScoresWithPredictions(pathScores map[string]float64, predictions []PathPrediction) map[string]float64 {
	if len(predictions) == 0 {
		return pathScores
	}

	// Build prediction lookup.
	predMap := make(map[string]PathPrediction, len(predictions))
	for _, p := range predictions {
		predMap[p.PathSignature] = p
	}

	adjusted := make(map[string]float64, len(pathScores))
	for sig, score := range pathScores {
		if pred, ok := predMap[sig]; ok && pred.Confidence > MinSignalConfidence {
			delta := (pred.ExpectedValue - score) * PredictionWeight
			adjusted[sig] = clamp01(score + delta)
		} else {
			adjusted[sig] = score
		}
	}

	return adjusted
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
