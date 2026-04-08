package strategy

import (
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// --- Signal Decomposition (Iteration 19.1) ---

// ConfidenceComponents decomposes confidence into evidence-quality sub-signals.
type ConfidenceComponents struct {
	SampleConfidence  float64 `json:"sample_confidence"`  // evidence quality from sample size
	RecencyConfidence float64 `json:"recency_confidence"` // evidence quality from recency
}

// Compose returns the weighted composite confidence, clamped to [0,1].
// Formula: SampleConfidence * 0.6 + RecencyConfidence * 0.4
func (cc ConfidenceComponents) Compose() float64 {
	return clamp01(cc.SampleConfidence*0.6 + cc.RecencyConfidence*0.4)
}

// RiskComponents decomposes risk into source-specific sub-signals.
type RiskComponents struct {
	StabilityRisk  float64 `json:"stability_risk"`  // from stability mode
	HistoricalRisk float64 `json:"historical_risk"` // from strategy/action memory track record
	PolicyRisk     float64 `json:"policy_risk"`     // from step count, blocked actions, etc.
}

// Compose returns the weighted composite risk, clamped to [0,1].
// Formula: StabilityRisk * 0.5 + HistoricalRisk * 0.3 + PolicyRisk * 0.2
func (rc RiskComponents) Compose() float64 {
	return clamp01(rc.StabilityRisk*0.5 + rc.HistoricalRisk*0.3 + rc.PolicyRisk*0.2)
}

// DecisionSignals captures the full normalized + decomposed signal state
// for a single portfolio candidate. Used for auditability.
type DecisionSignals struct {
	EV         float64 `json:"ev"`
	Confidence float64 `json:"confidence"`
	Risk       float64 `json:"risk"`

	ConfidenceComponents ConfidenceComponents `json:"confidence_components"`
	RiskComponents       RiskComponents       `json:"risk_components"`
}

// --- Portfolio Scoring ---
//
// The portfolio layer wraps the strategy pipeline with a competition model:
// goal → multiple strategies → enrich → score → select best → action
//
// Pipeline (Iteration 19.1):
//   collect raw signals → normalize → decompose → recombine → score

// StrategyCandidate represents one scored strategy competing in the portfolio.
type StrategyCandidate struct {
	StrategyType StrategyType `json:"strategy_type"`
	PlanID       string       `json:"plan_id"`

	// Core portfolio scores (all normalized to [0,1]).
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

	// Decomposed decision signals (Iteration 19.1).
	Signals DecisionSignals `json:"signals"`

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
// Pipeline: collect raw signals → normalize → decompose → recombine → score.
func enrichCandidate(plan *StrategyPlan, input PortfolioInput) StrategyCandidate {
	c := StrategyCandidate{
		StrategyType: plan.StrategyType,
		PlanID:       plan.ID.String(),
		Plan:         plan,
	}

	// --- Phase 1: Collect raw expected value signals ---
	ev := plan.ExpectedUtility

	// Strategy memory signal enrichment.
	if input.StrategyMemory != nil {
		if sm, ok := input.StrategyMemory[string(plan.StrategyType)]; ok {
			adj := strategyMemoryAdjustment(sm)
			ev += adj
			c.StrategyMemorySignal = adj
		}
	}

	// Continuation gain enrichment.
	if input.ContinuationGains != nil {
		if gain, ok := input.ContinuationGains[string(plan.StrategyType)]; ok {
			adj := continuationGainAdjustment(gain, plan.StepCount())
			ev += adj
			c.ContinuationGainSignal = adj
		}
	}

	// Action memory aggregate signal.
	actionAdj := actionMemoryAggregateSignal(plan, input.Base.ActionFeedback)
	ev += actionAdj
	c.ActionMemorySignal = actionAdj

	// --- Phase 2: Decompose confidence ---
	confComponents := decomposeConfidence(plan, input)
	compositeConfidence := confComponents.Compose()

	// --- Phase 3: Decompose risk ---
	riskComponents := decomposeRisk(plan, input)
	compositeRisk := riskComponents.Compose()

	// Record stability signal for auditability.
	c.StabilitySignal = riskComponents.StabilityRisk

	// --- Phase 4: Normalize all signals to [0,1] ---
	ev = clamp01(ev)
	compositeConfidence = clamp01(compositeConfidence)
	compositeRisk = clamp01(compositeRisk)

	c.ExpectedValue = ev
	c.RiskScore = compositeRisk
	c.Confidence = compositeConfidence

	// Store decomposed signals for auditability.
	c.Signals = DecisionSignals{
		EV:                   ev,
		Confidence:           compositeConfidence,
		Risk:                 compositeRisk,
		ConfidenceComponents: confComponents,
		RiskComponents:       riskComponents,
	}

	// --- Phase 5: Compute FinalScore ---
	c.FinalScore = computeFinalScore(ev, compositeRisk, compositeConfidence)

	return c
}

// decomposeConfidence builds ConfidenceComponents from available signals.
func decomposeConfidence(plan *StrategyPlan, input PortfolioInput) ConfidenceComponents {
	cc := ConfidenceComponents{
		SampleConfidence:  0.5, // default: moderate
		RecencyConfidence: 0.5, // default: moderate
	}

	// SampleConfidence: derived from action memory sample sizes across steps.
	if len(input.Base.ActionFeedback) > 0 && len(plan.Steps) > 0 {
		totalWeight := 0.0
		count := 0
		for _, step := range plan.Steps {
			if fb, ok := input.Base.ActionFeedback[step.ActionType]; ok {
				totalWeight += actionmemory.SampleWeight(fb.SampleSize)
				count++
			}
		}
		if count > 0 {
			cc.SampleConfidence = clamp01(totalWeight / float64(count))
		}
	}

	// Blend with strategy-level evidence if available.
	if input.StrategyMemory != nil {
		if sm, ok := input.StrategyMemory[string(plan.StrategyType)]; ok {
			if sm.SampleSize > 0 {
				stratWeight := clamp01(float64(sm.SampleSize) / 20.0)
				cc.SampleConfidence = cc.SampleConfidence*0.6 + stratWeight*0.4
			}
		}
	}

	// RecencyConfidence: base from plan confidence (which already incorporates
	// step degradation and action-level recency from scorer).
	cc.RecencyConfidence = clamp01(plan.Confidence)

	// Multi-step confidence degradation (recency signal weakens per step).
	if plan.StepCount() > 1 {
		cc.RecencyConfidence *= clamp01(1.0 - 0.10*float64(plan.StepCount()-1))
	}

	return cc
}

// decomposeRisk builds RiskComponents from available signals.
func decomposeRisk(plan *StrategyPlan, input PortfolioInput) RiskComponents {
	rc := RiskComponents{
		StabilityRisk:  0.0,
		HistoricalRisk: 0.0,
		PolicyRisk:     0.0,
	}

	// StabilityRisk: from stability mode.
	switch input.StabilityMode {
	case "safe_mode":
		if plan.StrategyType == StrategyNoop || plan.StrategyType == StrategyRecommendOnly {
			rc.StabilityRisk = 0.0
		} else {
			rc.StabilityRisk = 1.0
		}
	case "throttled":
		if plan.StepCount() > 1 {
			rc.StabilityRisk = 0.6
		} else {
			rc.StabilityRisk = 0.1
		}
	default:
		rc.StabilityRisk = 0.0
	}

	// HistoricalRisk: from strategy memory (failure rate).
	if input.StrategyMemory != nil {
		if sm, ok := input.StrategyMemory[string(plan.StrategyType)]; ok {
			rc.HistoricalRisk = clamp01(sm.FailureRate)
		}
	}

	// PolicyRisk: from step count + base plan risk.
	rc.PolicyRisk = clamp01(plan.RiskScore)
	if plan.StepCount() > 1 {
		rc.PolicyRisk = clamp01(rc.PolicyRisk + float64(plan.StepCount()-1)*RiskPerStep)
	}

	return rc
}

// computeFinalScore applies the portfolio scoring formula.
// Result is bounded [0, 1].
func computeFinalScore(expectedValue, riskScore, confidence float64) float64 {
	score := expectedValue*WeightExpected + confidence*WeightConfidence - riskScore*WeightRisk
	return clamp01(score)
}

// clamp01 clamps a value to [0, 1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
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
// Kept for backward compatibility — new code uses decomposeRisk().
func stabilityAdjustment(mode string, stepCount int, strategyType StrategyType) float64 {
	switch mode {
	case "safe_mode":
		if strategyType == StrategyNoop || strategyType == StrategyRecommendOnly {
			return 0
		}
		return 0.8
	case "throttled":
		if stepCount > 1 {
			return 0.3
		}
		return 0
	default:
		return 0
	}
}
