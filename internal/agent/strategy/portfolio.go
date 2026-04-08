package strategy

import (
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// --- Portfolio Scoring ---
//
// The portfolio layer wraps the strategy pipeline with a competition model:
// goal → multiple strategies → enrich → score → select best → action
//
// Each StrategyCandidate gets:
//   - ExpectedValue: from underlying strategy utility + signal enrichment
//   - RiskScore: from strategy risk + stability signals
//   - Confidence: from action memory + strategy memory evidence
//   - FinalScore: bounded [0,1] composite

// StrategyCandidate represents one scored strategy competing in the portfolio.
type StrategyCandidate struct {
	StrategyType StrategyType `json:"strategy_type"`
	PlanID       string       `json:"plan_id"`

	// Core portfolio scores.
	ExpectedValue float64 `json:"expected_value"`
	RiskScore     float64 `json:"risk_score"`
	Confidence    float64 `json:"confidence"`
	FinalScore    float64 `json:"final_score"`

	// Signal contributions (for auditability).
	StrategyMemorySignal   float64 `json:"strategy_memory_signal"`
	ContinuationGainSignal float64 `json:"continuation_gain_signal"`
	ActionMemorySignal     float64 `json:"action_memory_signal"`
	StabilitySignal        float64 `json:"stability_signal"`
	PolicySignal           float64 `json:"policy_signal"`

	// Source plan reference.
	Plan *StrategyPlan `json:"plan,omitempty"`
}

// PortfolioWeights defines the scoring formula weights.
// FinalScore = ExpectedValue * WeightExpected + Confidence * WeightConfidence - RiskScore * WeightRisk
const (
	WeightExpected   = 0.5
	WeightConfidence = 0.3
	WeightRisk       = 0.2
)

// PortfolioInput holds all signal sources needed for portfolio enrichment.
type PortfolioInput struct {
	// Base scoring input (action feedback, stability, etc.)
	Base ScoreInput

	// Strategy memory signals (from strategy_learning).
	StrategyMemory map[string]StrategyFeedbackSignal

	// Continuation gain rates per strategy type.
	ContinuationGains map[string]float64

	// Stability mode for portfolio-level overrides.
	StabilityMode string
}

// BuildPortfolio enriches scored strategy plans into portfolio candidates
// and computes portfolio-level FinalScore for each.
// Deterministic: same inputs always produce the same candidates.
func BuildPortfolio(plans []StrategyPlan, input PortfolioInput) []StrategyCandidate {
	candidates := make([]StrategyCandidate, 0, len(plans))

	for i := range plans {
		c := enrichCandidate(&plans[i], input)
		candidates = append(candidates, c)
	}

	return candidates
}

// enrichCandidate builds a StrategyCandidate from a scored plan + signal sources.
func enrichCandidate(plan *StrategyPlan, input PortfolioInput) StrategyCandidate {
	c := StrategyCandidate{
		StrategyType: plan.StrategyType,
		PlanID:       plan.ID.String(),
		Plan:         plan,
	}

	// 1. Base expected value from the strategy scorer.
	ev := plan.ExpectedUtility

	// 2. Strategy memory signal enrichment.
	if input.StrategyMemory != nil {
		if sm, ok := input.StrategyMemory[string(plan.StrategyType)]; ok {
			adj := strategyMemoryAdjustment(sm)
			ev += adj
			c.StrategyMemorySignal = adj
		}
	}

	// 3. Continuation gain enrichment.
	if input.ContinuationGains != nil {
		if gain, ok := input.ContinuationGains[string(plan.StrategyType)]; ok {
			adj := continuationGainAdjustment(gain, plan.StepCount())
			ev += adj
			c.ContinuationGainSignal = adj
		}
	}

	// 4. Action memory aggregate signal.
	actionAdj := actionMemoryAggregateSignal(plan, input.Base.ActionFeedback)
	ev += actionAdj
	c.ActionMemorySignal = actionAdj

	// 5. Stability signal adjustment.
	risk := plan.RiskScore
	stabAdj := stabilityAdjustment(input.StabilityMode, plan.StepCount(), plan.StrategyType)
	c.StabilitySignal = stabAdj
	if stabAdj > 0 {
		risk += stabAdj
	} else {
		ev += stabAdj // negative stability signal penalizes expected value
	}

	// Clamp expected value to [0, 1].
	if ev < 0 {
		ev = 0
	}
	if ev > 1.0 {
		ev = 1.0
	}

	// Clamp risk to [0, 1].
	if risk < 0 {
		risk = 0
	}
	if risk > 1.0 {
		risk = 1.0
	}

	c.ExpectedValue = ev
	c.RiskScore = risk
	c.Confidence = plan.Confidence

	// Compute FinalScore using the portfolio formula.
	c.FinalScore = computeFinalScore(ev, risk, plan.Confidence)

	return c
}

// computeFinalScore applies the portfolio scoring formula.
// Result is bounded [0, 1].
func computeFinalScore(expectedValue, riskScore, confidence float64) float64 {
	score := expectedValue*WeightExpected + confidence*WeightConfidence - riskScore*WeightRisk
	if score < 0 {
		score = 0
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// --- Signal Enrichment Functions ---

// strategyMemoryAdjustment returns an adjustment based on strategy memory.
// Range: [-0.10, +0.08].
func strategyMemoryAdjustment(signal StrategyFeedbackSignal) float64 {
	switch signal.Recommendation {
	case "prefer_strategy":
		// Scale by evidence quality.
		adj := 0.08
		if signal.SampleSize < 10 {
			adj *= float64(signal.SampleSize) / 10.0
		}
		return adj
	case "avoid_strategy":
		adj := -0.10
		if signal.SampleSize < 10 {
			adj *= float64(signal.SampleSize) / 10.0
		}
		return adj
	default:
		return 0
	}
}

// continuationGainAdjustment rewards multi-step strategies with positive continuation history.
// Only applies to strategies with > 1 step. Range: [-0.05, +0.05].
func continuationGainAdjustment(gainRate float64, stepCount int) float64 {
	if stepCount <= 1 {
		return 0
	}
	// Scale: gain rate 0.6+ is positive, below 0.3 is negative.
	if gainRate >= 0.6 {
		return 0.05
	}
	if gainRate <= 0.3 {
		return -0.05
	}
	return 0
}

// actionMemoryAggregateSignal computes an aggregate adjustment from
// action memory across all steps. Range: [-0.05, +0.05].
func actionMemoryAggregateSignal(plan *StrategyPlan, feedback map[string]actionmemory.ActionFeedback) float64 {
	if len(feedback) == 0 || len(plan.Steps) == 0 {
		return 0
	}

	total := 0.0
	count := 0
	for _, step := range plan.Steps {
		if fb, ok := feedback[step.ActionType]; ok {
			switch fb.Recommendation {
			case actionmemory.RecommendPreferAction:
				total += 0.05
			case actionmemory.RecommendAvoidAction:
				total -= 0.05
			}
			count++
		}
	}

	if count == 0 {
		return 0
	}

	avg := total / float64(count)
	// Clamp to [-0.05, +0.05].
	if avg > 0.05 {
		avg = 0.05
	}
	if avg < -0.05 {
		avg = -0.05
	}
	return avg
}

// stabilityAdjustment returns a risk or EV penalty based on stability mode.
// safe_mode → only safest strategies allowed (high risk penalty for non-safe).
// throttled → penalize high-risk strategies.
func stabilityAdjustment(mode string, stepCount int, strategyType StrategyType) float64 {
	switch mode {
	case "safe_mode":
		// In safe_mode, only noop and single-step recommendation are safe.
		if strategyType == StrategyNoop || strategyType == StrategyRecommendOnly {
			return 0
		}
		return 0.8 // massive risk addition
	case "throttled":
		// Throttled: penalize multi-step and aggressive strategies.
		if stepCount > 1 {
			return 0.3
		}
		return 0
	default:
		return 0
	}
}
