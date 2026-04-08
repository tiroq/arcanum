package resource_optimization

// ComputeSignals computes resource decision signals from a resource profile.
// All output values are in [0, 1]. Deterministic: same inputs → same outputs.
//
// Returns zero signals if profile is nil or has insufficient samples.
func ComputeSignals(profile ResourceProfile) ResourceDecisionSignals {
	if profile.SampleCount < MinSamplesForSignals {
		return ResourceDecisionSignals{EfficiencyScore: 1.0}
	}

	latencyPenalty := computeLatencyPenalty(profile.AvgLatencyMs, profile.AvgReasoningDepth, profile.AvgPathLength)
	costPenalty := computeCostPenalty(profile.AvgTokenCost, profile.AvgExecutionCost)
	depthPenalty := computeDepthPenalty(profile.AvgReasoningDepth)

	// EfficiencyScore = 1 - weighted sum of penalties.
	efficiency := 1.0 - (latencyPenalty*LatencyWeight + costPenalty*CostWeight + depthPenalty*DepthWeight)
	efficiency = clamp01(efficiency)

	return ResourceDecisionSignals{
		LatencyPenalty:  latencyPenalty,
		CostPenalty:     costPenalty,
		DepthPenalty:    depthPenalty,
		EfficiencyScore: efficiency,
	}
}

// ComputeSignalsFromProfile is a nil-safe variant that accepts a pointer.
// Returns default (no-penalty) signals if profile is nil.
func ComputeSignalsFromProfile(profile *ResourceProfile) ResourceDecisionSignals {
	if profile == nil {
		return ResourceDecisionSignals{EfficiencyScore: 1.0}
	}
	return ComputeSignals(*profile)
}

// computeLatencyPenalty returns a penalty in [0, 1] based on average latency,
// reasoning depth, and path length.
//
// Formula: linear interpolation from LatencyThresholdMs (0) to HighLatencyMs (1),
// with a small additive adjustment for depth and path length.
func computeLatencyPenalty(avgLatencyMs, avgReasoningDepth, avgPathLength float64) float64 {
	if avgLatencyMs <= LatencyThresholdMs {
		return 0
	}

	basePenalty := (avgLatencyMs - LatencyThresholdMs) / (HighLatencyMs - LatencyThresholdMs)

	// Depth amplifier: deeper reasoning = slightly higher penalty.
	depthAmp := 0.0
	if avgReasoningDepth > 1.0 {
		depthAmp = (avgReasoningDepth - 1.0) * 0.05
	}

	// Path length amplifier: longer paths = slightly higher penalty.
	pathAmp := 0.0
	if avgPathLength > 1.0 {
		pathAmp = (avgPathLength - 1.0) * 0.03
	}

	return clamp01(basePenalty + depthAmp + pathAmp)
}

// computeCostPenalty returns a penalty in [0, 1] based on token usage and
// execution cost.
//
// Formula: linear interpolation from CostThreshold (0) to HighCost (1).
func computeCostPenalty(avgTokenCost, avgExecutionCost float64) float64 {
	// Use the higher of the two cost signals.
	cost := avgTokenCost
	if avgExecutionCost > cost {
		cost = avgExecutionCost
	}

	if cost <= CostThreshold {
		return 0
	}

	return clamp01((cost - CostThreshold) / (HighCost - CostThreshold))
}

// computeDepthPenalty returns a penalty in [0, 1] based on reasoning depth.
//
// Formula: linear interpolation from DepthThreshold (0) to HighDepth (1).
func computeDepthPenalty(avgReasoningDepth float64) float64 {
	if avgReasoningDepth <= DepthThreshold {
		return 0
	}
	return clamp01((avgReasoningDepth - DepthThreshold) / (HighDepth - DepthThreshold))
}

// ComputeModeAdjustment returns a bounded score adjustment for meta-reasoning mode selection.
// Positive = boost (prefer this mode), Negative = penalty (avoid this mode).
//
// Rules:
//   - Direct mode gets a boost when confidence is high and efficiency is good.
//   - Graph mode gets a penalty when cost is high and historical benefit (success rate) is low.
//   - Conservative mode is never penalized for resource reasons.
//   - Missing profile → 0 adjustment (fail-open).
//
// Parameters:
//   - mode: the reasoning mode being evaluated
//   - profile: the resource profile for this mode+goalType (may be nil)
//   - confidence: current decision confidence [0, 1]
//   - successRate: historical success rate of this mode [0, 1]
//   - stabilityMode: current stability state
func ComputeModeAdjustment(mode string, profile *ResourceProfile, confidence, successRate float64, stabilityMode string) float64 {
	// Conservative mode is never adjusted for resource efficiency reasons.
	if mode == "conservative" {
		return 0
	}

	// Stability overrides: no resource adjustments in safe_mode.
	if stabilityMode == "safe_mode" {
		return 0
	}

	signals := ComputeSignalsFromProfile(profile)

	switch mode {
	case "direct":
		// Boost direct mode when confidence is high and efficiency is good.
		if confidence >= 0.8 && signals.EfficiencyScore >= 0.7 {
			return ModeDirectBoost
		}
		return 0

	case "graph":
		// Penalize graph mode when it's expensive AND success rate doesn't justify it.
		if signals.EfficiencyScore < 0.5 && successRate < 0.6 {
			return -ModeGraphPenalty
		}
		return 0

	case "exploratory":
		// Slight penalty if exploration is very expensive.
		if signals.EfficiencyScore < 0.3 {
			return -ModeGraphPenalty * 0.5
		}
		return 0
	}

	return 0
}

// ComputePathResourcePenalty returns a bounded penalty for path scoring
// based on path length and the resource profile of the current mode.
// The penalty is in [0, ResourcePenaltyWeight].
//
// Longer paths are penalized when their historical cost profile is high.
// Returns 0 when:
//   - profile is nil (fail-open)
//   - profile has insufficient samples
//   - path length is 1 (single-step)
//   - mode is conservative
func ComputePathResourcePenalty(pathLength int, profile *ResourceProfile, stabilityMode string) float64 {
	// No penalty for single-step paths.
	if pathLength <= 1 {
		return 0
	}

	// No adjustment in safe_mode.
	if stabilityMode == "safe_mode" {
		return 0
	}

	signals := ComputeSignalsFromProfile(profile)

	// Penalty grows with path length and inversely with efficiency.
	inefficiency := 1.0 - signals.EfficiencyScore
	lengthFactor := float64(pathLength-1) / 4.0 // normalize: 1 extra node = 0.25, max = 1.0
	if lengthFactor > 1.0 {
		lengthFactor = 1.0
	}

	penalty := inefficiency * lengthFactor * ResourcePenaltyWeight
	return clamp01(penalty)
}

// DetectPressure returns the current resource pressure state based on profiles.
// Returns "none", "high_latency", "high_cost", or "high_latency_and_cost".
func DetectPressure(profiles []ResourceProfile) string {
	if len(profiles) == 0 {
		return "none"
	}

	highLatency := false
	highCost := false

	for _, p := range profiles {
		if p.SampleCount < MinSamplesForSignals {
			continue
		}
		if p.AvgLatencyMs >= HighLatencyPressureThreshold {
			highLatency = true
		}
		if p.AvgExecutionCost >= HighCostPressureThreshold {
			highCost = true
		}
	}

	if highLatency && highCost {
		return "high_latency_and_cost"
	}
	if highLatency {
		return "high_latency"
	}
	if highCost {
		return "high_cost"
	}
	return "none"
}
