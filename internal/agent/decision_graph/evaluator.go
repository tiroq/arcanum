package decision_graph

import "math"

// EvaluatePath computes the aggregate scores for a decision path.
//
// TotalValue = average of node expected values.
// TotalRisk = aggregated risk (risk compounds, not sums).
// TotalConfidence = minimum confidence across all nodes.
// FinalScore = TotalValue * 0.5 + TotalConfidence * 0.3 - TotalRisk * 0.2
//
// Deterministic: same path always produces the same score.
func EvaluatePath(path DecisionPath, config GraphConfig) DecisionPath {
	if len(path.Nodes) == 0 {
		return path
	}

	// TotalValue = mean of expected values.
	totalValue := 0.0
	for _, n := range path.Nodes {
		totalValue += n.ExpectedValue
	}
	totalValue /= float64(len(path.Nodes))

	// TotalRisk = aggregated (compounding, not linear sum).
	// Formula: 1 - product(1 - risk_i), clamped to [0,1].
	riskProduct := 1.0
	for _, n := range path.Nodes {
		riskProduct *= (1.0 - clamp01(n.Risk))
	}
	totalRisk := clamp01(1.0 - riskProduct)

	// Throttled mode: penalize long paths.
	if config.StabilityMode == "throttled" && len(path.Nodes) > 1 {
		totalRisk = clamp01(totalRisk + config.LongPathPenalty*float64(len(path.Nodes)-1))
	}

	// TotalConfidence = minimum confidence across all nodes.
	totalConfidence := math.MaxFloat64
	for _, n := range path.Nodes {
		if n.Confidence < totalConfidence {
			totalConfidence = n.Confidence
		}
	}
	if totalConfidence == math.MaxFloat64 {
		totalConfidence = 0
	}

	// FinalScore = weighted combination.
	finalScore := totalValue*WeightValue + totalConfidence*WeightConfidence - totalRisk*WeightRisk
	finalScore = clamp01(finalScore)

	path.TotalValue = totalValue
	path.TotalRisk = totalRisk
	path.TotalConfidence = totalConfidence
	path.FinalScore = finalScore

	return path
}

// EvaluateAllPaths scores all paths in a list.
func EvaluateAllPaths(paths []DecisionPath, config GraphConfig) []DecisionPath {
	scored := make([]DecisionPath, len(paths))
	for i, p := range paths {
		scored[i] = EvaluatePath(p, config)
	}
	return scored
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
