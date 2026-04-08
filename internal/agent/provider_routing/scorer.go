package provider_routing

import "fmt"

// ScoreComponents holds the decomposed scoring signals for a provider.
type ScoreComponents struct {
	LatencyFit      float64 `json:"latency_fit"`
	QuotaHeadroom   float64 `json:"quota_headroom"`
	ReliabilityFit  float64 `json:"reliability_fit"`
	CostEfficiency  float64 `json:"cost_efficiency"`
	ModelCapability float64 `json:"model_capability"` // Iteration 32: capability fit
	FinalScore      float64 `json:"final_score"`
}

// ScoreProvider computes a deterministic score for a given provider against a routing input.
// All components are bounded to [0,1] and combined using fixed weights.
func ScoreProvider(p Provider, input RoutingInput, usage ProviderUsageState) ScoreComponents {
	latency := computeLatencyFit(p, input)
	quota := ComputeHeadroom(p.Limits, usage, input.EstimatedTokens)
	reliability := computeReliabilityFit(p)
	cost := computeCostEfficiency(p, input)
	capability := computeModelCapabilityFit(p, input)

	final := latency*WeightLatencyFit +
		quota*WeightQuotaHeadroom +
		reliability*WeightReliability +
		cost*WeightCostEfficiency +
		capability*WeightModelCapability

	return ScoreComponents{
		LatencyFit:      latency,
		QuotaHeadroom:   quota,
		ReliabilityFit:  reliability,
		CostEfficiency:  cost,
		ModelCapability: capability,
		FinalScore:      clamp01(final),
	}
}

// FormatScoreReason builds a human-readable explanation of the score components.
func FormatScoreReason(c ScoreComponents) string {
	return fmt.Sprintf(
		"latency=%.2f quota=%.2f reliability=%.2f cost=%.2f capability=%.2f → score=%.3f",
		c.LatencyFit, c.QuotaHeadroom, c.ReliabilityFit, c.CostEfficiency, c.ModelCapability, c.FinalScore,
	)
}

// computeLatencyFit returns a [0,1] score based on provider kind vs latency budget.
// Local providers are fast (low latency), cloud providers are slower but stronger.
func computeLatencyFit(p Provider, input RoutingInput) float64 {
	if input.LatencyBudgetMs <= 0 {
		// No latency constraint → all providers fit equally.
		return 0.5
	}

	switch p.Kind {
	case KindLocal:
		// Local providers have native low latency.
		if input.LatencyBudgetMs <= 500 {
			return 1.0 // tight budget → local excels
		}
		return 0.8 // relaxed budget → local still good
	case KindCloud:
		if input.LatencyBudgetMs <= 500 {
			return 0.3 // tight budget → cloud is risky
		}
		if input.LatencyBudgetMs <= 2000 {
			return 0.6 // moderate budget → cloud acceptable
		}
		return 0.8 // relaxed budget → cloud fine
	case KindRouter:
		if input.LatencyBudgetMs <= 500 {
			return 0.2 // tight budget → router adds overhead
		}
		if input.LatencyBudgetMs <= 2000 {
			return 0.5
		}
		return 0.7
	default:
		return 0.5
	}
}

// computeReliabilityFit returns a [0,1] score based on provider health.
func computeReliabilityFit(p Provider) float64 {
	if !p.Health.Enabled {
		return 0.0
	}
	if !p.Health.Reachable {
		return 0.1
	}
	if p.Health.Degraded {
		return 0.4
	}
	return 1.0
}

// computeCostEfficiency returns a [0,1] score. Lower cost = higher efficiency.
func computeCostEfficiency(p Provider, input RoutingInput) float64 {
	// Local and free providers always score high on cost efficiency.
	switch p.Cost.CostClass {
	case CostLocal:
		return 1.0
	case CostFree:
		return 0.95
	case CostCheap:
		return 0.7
	case CostUnknown:
		return 0.5
	default:
		// Use relative cost as inverse score.
		return clamp01(1.0 - p.Cost.RelativeCost)
	}
}

// computeModelCapabilityFit returns a [0,1] score based on how well
// a provider's capabilities match the required capabilities (Iteration 32).
//   - no required capabilities → 1.0 (neutral)
//   - exact match (all required present) → 1.0
//   - partial match → fraction matched
//   - no match → 0.0
func computeModelCapabilityFit(p Provider, input RoutingInput) float64 {
	if len(input.RequiredCapabilities) == 0 {
		return 1.0 // no requirements → neutral
	}

	matched := 0
	for _, req := range input.RequiredCapabilities {
		if p.HasCapability(req) {
			matched++
		}
	}
	return float64(matched) / float64(len(input.RequiredCapabilities))
}
