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

// --- Path/Transition Learning Adjustments (Iteration 21) ---

// PathLearningSignals holds path and transition feedback for scoring adjustments.
type PathLearningSignals struct {
	// PathFeedback: map[pathSignature] → recommendation string
	// Values: "prefer_path", "avoid_path", "neutral"
	PathFeedback map[string]string

	// TransitionFeedback: map[transitionKey] → recommendation string
	// Values: "prefer_transition", "avoid_transition", "neutral"
	TransitionFeedback map[string]string
}

// ApplyPathLearningAdjustments adjusts scored paths based on path and transition learning signals.
// Adjustments are additive and bounded. Returns the adjusted paths.
// If signals is nil, returns paths unchanged (fail-open).
func ApplyPathLearningAdjustments(paths []DecisionPath, signals *PathLearningSignals) []DecisionPath {
	if signals == nil {
		return paths
	}

	adjusted := make([]DecisionPath, len(paths))
	for i, p := range paths {
		adjustment := 0.0

		// Path-level adjustment.
		sig := pathSignatureFromNodes(p.Nodes)
		if rec, ok := signals.PathFeedback[sig]; ok {
			switch rec {
			case "prefer_path":
				adjustment += pathPreferAdjustment
			case "avoid_path":
				adjustment += pathAvoidAdjustment
			}
		}

		// Transition-level adjustments (per edge in the path).
		if len(p.Nodes) > 1 {
			for j := 0; j < len(p.Nodes)-1; j++ {
				tKey := p.Nodes[j].ActionType + "->" + p.Nodes[j+1].ActionType
				if rec, ok := signals.TransitionFeedback[tKey]; ok {
					switch rec {
					case "prefer_transition":
						adjustment += transitionPreferAdjustment
					case "avoid_transition":
						adjustment += transitionAvoidAdjustment
					}
				}
			}
		}

		p.FinalScore = clamp01(p.FinalScore + adjustment)
		adjusted[i] = p
	}

	return adjusted
}

// pathSignatureFromNodes builds a canonical path signature from a slice of nodes.
func pathSignatureFromNodes(nodes []DecisionNode) string {
	if len(nodes) == 0 {
		return ""
	}
	sig := nodes[0].ActionType
	for i := 1; i < len(nodes); i++ {
		sig += ">" + nodes[i].ActionType
	}
	return sig
}

// Path/transition adjustment constants.
const (
	pathPreferAdjustment       = 0.10
	pathAvoidAdjustment        = -0.20
	transitionPreferAdjustment = 0.05
	transitionAvoidAdjustment  = -0.10
)

// --- Comparative Learning Adjustments (Iteration 22) ---

// ComparativeLearningSignals holds comparative feedback for scoring adjustments.
type ComparativeLearningSignals struct {
	// ComparativeFeedback: map[pathSignature] → recommendation string
	// Values: "prefer_path", "avoid_path", "underexplored_path", "neutral"
	ComparativeFeedback map[string]string
}

// Comparative adjustment constants.
const (
	comparativePreferAdjustment        = 0.10
	comparativeAvoidAdjustment         = -0.20
	comparativeUnderexploredAdjustment = 0.05
)

// ApplyComparativeLearningAdjustments adjusts scored paths based on comparative learning signals.
// Adjustments are additive to existing scores (including path learning adjustments).
// If signals is nil, returns paths unchanged (fail-open).
func ApplyComparativeLearningAdjustments(paths []DecisionPath, signals *ComparativeLearningSignals) []DecisionPath {
	if signals == nil {
		return paths
	}

	adjusted := make([]DecisionPath, len(paths))
	for i, p := range paths {
		adjustment := 0.0

		sig := pathSignatureFromNodes(p.Nodes)
		if rec, ok := signals.ComparativeFeedback[sig]; ok {
			switch rec {
			case "prefer_path":
				adjustment += comparativePreferAdjustment
			case "avoid_path":
				adjustment += comparativeAvoidAdjustment
			case "underexplored_path":
				adjustment += comparativeUnderexploredAdjustment
			}
		}

		p.FinalScore = clamp01(p.FinalScore + adjustment)
		adjusted[i] = p
	}

	return adjusted
}

// --- Counterfactual Simulation Adjustments (Iteration 23) ---

// CounterfactualPredictions holds predicted values for scoring adjustments.
type CounterfactualPredictions struct {
	// Predictions: map[pathSignature] → predicted expected value
	Predictions map[string]float64
	// Confidences: map[pathSignature] → prediction confidence
	Confidences map[string]float64
}

// Counterfactual adjustment constant.
const (
	counterfactualPredictionWeight = 0.20
	counterfactualMinConfidence    = 0.01
)

// ApplyCounterfactualAdjustments adjusts scored paths based on counterfactual predictions.
// AdjustedScore = OriginalScore + (PredictedValue - OriginalScore) × PredictionWeight
// If predictions is nil, returns paths unchanged (fail-open).
func ApplyCounterfactualAdjustments(paths []DecisionPath, predictions *CounterfactualPredictions) []DecisionPath {
	if predictions == nil || len(predictions.Predictions) == 0 {
		return paths
	}

	adjusted := make([]DecisionPath, len(paths))
	for i, p := range paths {
		sig := pathSignatureFromNodes(p.Nodes)
		if predValue, ok := predictions.Predictions[sig]; ok {
			conf := 0.0
			if c, cok := predictions.Confidences[sig]; cok {
				conf = c
			}
			if conf > counterfactualMinConfidence {
				delta := (predValue - p.FinalScore) * counterfactualPredictionWeight
				p.FinalScore = clamp01(p.FinalScore + delta)
			}
		}
		adjusted[i] = p
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
