package strategy

import (
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// ScoreInput holds the signals needed to score strategy candidates.
type ScoreInput struct {
	ActionFeedback  map[string]actionmemory.ActionFeedback
	CandidateScores map[string]float64 // action_type -> planner score
	CandidateConf   map[string]float64 // action_type -> planner confidence
	StabilityMode   string             // "normal", "throttled", "safe_mode"
	BlockedActions  []string

	// StrategyFeedback holds per-strategy_type learning signals.
	// Keys are strategy type names. Values encode recommendation as a string.
	// When set, prefer_strategy boosts utility and avoid_strategy penalizes it.
	StrategyFeedback map[string]StrategyFeedbackSignal
}

// StrategyFeedbackSignal carries strategy-level learning signal.
type StrategyFeedbackSignal struct {
	Recommendation string  // "prefer_strategy", "avoid_strategy", "neutral", "insufficient_data"
	SuccessRate    float64
	FailureRate    float64
	SampleSize     int
}

// ScoreStrategies assigns expected utility, risk, and confidence to each
// candidate strategy plan. Deterministic: same inputs produce same scores.
func ScoreStrategies(plans []StrategyPlan, input ScoreInput) []StrategyPlan {
	blocked := make(map[string]bool, len(input.BlockedActions))
	for _, a := range input.BlockedActions {
		blocked[a] = true
	}

	for i := range plans {
		scorePlan(&plans[i], input, blocked)
	}
	return plans
}

// scorePlan computes scores for a single strategy plan.
func scorePlan(plan *StrategyPlan, input ScoreInput, blocked map[string]bool) {
	steps := plan.Steps
	if len(steps) == 0 {
		plan.ExpectedUtility = 0
		plan.RiskScore = 0
		plan.Confidence = 0
		return
	}

	// Check for blocked actions — reject the entire strategy.
	for _, step := range steps {
		if blocked[step.ActionType] {
			plan.ExpectedUtility = 0
			plan.RiskScore = 1.0
			plan.Confidence = 0
			plan.Explanation += " [REJECTED: blocked action " + step.ActionType + "]"
			return
		}
	}

	// A. Step-level action quality: average quality across steps.
	totalQuality := 0.0
	qualityCount := 0
	for _, step := range steps {
		q := actionQuality(step.ActionType, input)
		totalQuality += q
		qualityCount++
	}
	avgQuality := totalQuality / float64(qualityCount)

	// B. Risk accumulation: more steps = more risk.
	stepCount := len(steps)
	risk := float64(stepCount-1) * RiskPerStep

	// C. Confidence: base confidence from action evidence, degraded per extra step.
	conf := stepConfidence(steps, input)
	for s := 1; s < stepCount; s++ {
		conf *= MultiStepConfidenceMultiplier
	}

	// D. Stability penalty for multi-step in non-normal mode.
	if stepCount > 1 && input.StabilityMode == "throttled" {
		avgQuality *= StabilityThrottlePenalty
		risk += 0.3
	}
	// In safe_mode, multi-step strategies are fully suppressed.
	if stepCount > 1 && input.StabilityMode == "safe_mode" {
		avgQuality = 0
		risk = 1.0
	}

	// E. Exploration discount.
	if plan.Exploratory {
		avgQuality -= ExploratoryUtilityDiscount
	}

	// F. Strategy-level feedback bias (Iteration 18).
	if input.StrategyFeedback != nil {
		if fb, ok := input.StrategyFeedback[string(plan.StrategyType)]; ok {
			switch fb.Recommendation {
			case "prefer_strategy":
				avgQuality += StrategyPreferBoost
			case "avoid_strategy":
				avgQuality += StrategyAvoidPenalty
			}
		}
	}

	// Noop has a fixed baseline utility.
	if plan.StrategyType == StrategyNoop {
		avgQuality = 0.10
		risk = 0.0
		conf = 1.0
	}

	// Expected utility = quality - risk penalty.
	utility := avgQuality - risk*0.5
	if utility < 0 {
		utility = 0
	}

	plan.ExpectedUtility = utility
	plan.RiskScore = risk
	plan.Confidence = conf
}

// actionQuality computes a quality score for a single action type
// based on planner scores and action feedback.
func actionQuality(actionType string, input ScoreInput) float64 {
	// Start with the planner's computed score if available.
	base := 0.5
	if s, ok := input.CandidateScores[actionType]; ok {
		base = s
	}

	// Adjust based on action memory feedback.
	if fb, ok := input.ActionFeedback[actionType]; ok {
		switch fb.Recommendation {
		case actionmemory.RecommendPreferAction:
			base += 0.10
		case actionmemory.RecommendAvoidAction:
			base -= 0.20
		case actionmemory.RecommendNeutral:
			// no adjustment
		}
	}

	if base < 0 {
		base = 0
	}
	if base > 1.0 {
		base = 1.0
	}
	return base
}

// stepConfidence computes aggregate confidence for a strategy's steps.
func stepConfidence(steps []StrategyStep, input ScoreInput) float64 {
	total := 0.0
	count := 0
	for _, step := range steps {
		c := 0.5 // default moderate confidence
		if v, ok := input.CandidateConf[step.ActionType]; ok {
			c = v
		}
		// Adjust for evidence quality.
		if fb, ok := input.ActionFeedback[step.ActionType]; ok {
			weight := actionmemory.SampleWeight(fb.SampleSize)
			c = c*0.6 + weight*0.4
		}
		total += c
		count++
	}
	if count == 0 {
		return 0.5
	}
	return total / float64(count)
}
