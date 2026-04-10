package capacity

import "math"

// ComputeAvailableCapacity computes the owner's available hours today.
//
// Formula:
//
//	base_capacity = max_daily_work_hours
//	minus blocked_hours (family blocked time)
//	minus overload_penalty (if owner_load_score > OverloadThreshold)
//
// Overload penalty:
//
//	penalty = ((load - OverloadThreshold) / (1 - OverloadThreshold)) * OverloadPenaltyWeight * base
//	This linearly scales from 0 at threshold to OverloadPenaltyWeight * base at load=1.0.
//
// Result is clamped to [0, max_daily_work_hours].
func ComputeAvailableCapacity(maxDailyHours, blockedHours, ownerLoadScore float64) float64 {
	if maxDailyHours <= 0 {
		maxDailyHours = DefaultMaxDailyWorkHours
	}

	available := maxDailyHours - blockedHours

	// Apply overload penalty.
	if ownerLoadScore > OverloadThreshold {
		overloadFraction := (ownerLoadScore - OverloadThreshold) / (1.0 - OverloadThreshold)
		penalty := overloadFraction * OverloadPenaltyWeight * maxDailyHours
		available -= penalty
	}

	return clamp(available, 0, maxDailyHours)
}

// ComputeValuePerHour computes the value-per-hour ratio for an item.
//
// Formula:
//
//	value_per_hour = expected_value / max(estimated_effort, MinimumEffortFloor)
func ComputeValuePerHour(expectedValue, estimatedEffort float64) float64 {
	effort := math.Max(estimatedEffort, MinimumEffortFloor)
	if expectedValue <= 0 {
		return 0
	}
	return expectedValue / effort
}

// ComputeCapacityFitScore computes a bounded [0,1] score reflecting how well
// an item fits the owner's current capacity.
//
// Formula (4 components, weighted):
//
//  1. value_component     = clamp(value_per_hour / normalizer, 0, 1) × 0.35
//     where normalizer = max(HighValuePerHourThreshold, 1)
//
//  2. urgency_component   = urgency × 0.25
//
//  3. effort_fit_component = clamp(1 - (effort / available_hours), 0, 1) × 0.25
//     If available_hours <= 0, effort_fit = 0.
//
//  4. load_component      = clamp(1 - owner_load_score, 0, 1) × 0.15
//
//     capacity_fit_score = value + urgency + effort_fit + load
//     Clamped to [0, 1].
func ComputeCapacityFitScore(
	valuePerHour float64,
	urgency float64,
	estimatedEffort float64,
	availableHours float64,
	ownerLoadScore float64,
) float64 {
	// 1. Value component (35%)
	normalizer := math.Max(HighValuePerHourThreshold, 1.0)
	valueComp := clamp(valuePerHour/normalizer, 0, 1) * 0.35

	// 2. Urgency component (25%)
	urgencyComp := clamp(urgency, 0, 1) * 0.25

	// 3. Effort fit component (25%)
	effortFit := 0.0
	if availableHours > 0 {
		effortFit = clamp(1.0-(estimatedEffort/availableHours), 0, 1)
	}
	effortFitComp := effortFit * 0.25

	// 4. Load component (15%)
	loadComp := clamp(1.0-ownerLoadScore, 0, 1) * 0.15

	score := valueComp + urgencyComp + effortFitComp + loadComp
	return clamp(score, 0, 1)
}

// EvaluateItem evaluates a single item against the current capacity state.
// Returns a CapacityDecision with recommendation and optional defer reason.
func EvaluateItem(item CapacityItem, state CapacityState) CapacityDecision {
	vph := ComputeValuePerHour(item.ExpectedValue, item.EstimatedEffort)
	fitScore := ComputeCapacityFitScore(
		vph, item.Urgency, item.EstimatedEffort,
		state.AvailableHoursToday, state.OwnerLoadScore,
	)

	d := CapacityDecision{
		ItemType:         item.ItemType,
		ItemID:           item.ItemID,
		EstimatedEffort:  item.EstimatedEffort,
		ExpectedValue:    item.ExpectedValue,
		ValuePerHour:     vph,
		CapacityFitScore: fitScore,
	}

	// Determine recommendation.
	if fitScore >= RecommendThreshold {
		d.Recommended = true
	} else {
		d.Recommended = false
		d.DeferReason = deferReason(item, state, fitScore)
	}

	return d
}

// ComputeCapacityPenalty returns a bounded penalty for the decision graph.
// Applied to scored paths to penalise actions when capacity is constrained.
//
// penalty = (1 - available_fraction) × CapacityPenaltyMax
// where available_fraction = available_hours / max_daily_hours
//
// Returns 0 if capacity data is missing (fail-open).
func ComputeCapacityPenalty(availableHours, maxDailyHours, ownerLoadScore float64) float64 {
	if maxDailyHours <= 0 {
		return 0 // fail-open
	}
	availableFraction := clamp(availableHours/maxDailyHours, 0, 1)
	basePenalty := (1.0 - availableFraction) * CapacityPenaltyMax

	// Extra penalty when overloaded.
	if ownerLoadScore > OverloadThreshold {
		overloadExtra := (ownerLoadScore - OverloadThreshold) * CapacityPenaltyMax * 0.5
		basePenalty += overloadExtra
	}

	return clamp(basePenalty, 0, CapacityPenaltyMax)
}

// ComputeCapacityBoost returns a bounded boost for small, high-value tasks
// when capacity is limited. Encourages the system to prefer quick wins.
//
// boost = 0 if effort > SmallTaskThreshold or value_per_hour < HighValuePerHourThreshold
// boost = clamp(fit_score * CapacityBoostMax, 0, CapacityBoostMax)
func ComputeCapacityBoost(fitScore, estimatedEffort, valuePerHour float64) float64 {
	if estimatedEffort > SmallTaskThreshold {
		return 0
	}
	if valuePerHour < HighValuePerHourThreshold {
		return 0
	}
	return clamp(fitScore*CapacityBoostMax, 0, CapacityBoostMax)
}

// deferReason generates a human-readable reason for deferring an item.
func deferReason(item CapacityItem, state CapacityState, fitScore float64) string {
	if item.EstimatedEffort > state.AvailableHoursToday && state.AvailableHoursToday > 0 {
		return "exceeds_available_capacity"
	}
	if state.OwnerLoadScore > OverloadThreshold {
		return "owner_overloaded"
	}
	if ComputeValuePerHour(item.ExpectedValue, item.EstimatedEffort) < HighValuePerHourThreshold*0.2 {
		return "low_value_per_hour"
	}
	return "low_capacity_fit"
}

// clamp constrains v to [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
