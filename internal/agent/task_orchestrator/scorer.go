package taskorchestrator

import (
	"time"
)

// ComputePriority calculates a task's priority score from its attributes and external signals.
//
// Formula:
//
//	priority = objective_score * 0.30 + value_score * 0.25 + urgency * 0.20 + recency_boost * 0.10 - risk_score * 0.15
//
// Special rules:
//   - Tasks waiting > StarvationHours get a recency boost
//   - High-risk tasks (risk > HighRiskThreshold) capped at HighRiskMaxPrio
//   - Objective penalty reduces priority proportionally
//   - Portfolio strategy boost added when aligned
func ComputePriority(task OrchestratedTask, input ScoringInput, portfolioBoost float64, now time.Time) float64 {
	// Objective alignment score: 1.0 by default, reduced by penalty.
	objectiveScore := 1.0
	if input.ObjectiveSignalType == "penalty" && input.ObjectiveSignalStrength > 0 {
		objectiveScore = clamp01(1.0 - input.ObjectiveSignalStrength)
	}

	// Value score: normalise expected value into [0,1] using a reference of 1000.
	valueScore := clamp01(task.ExpectedValue / 1000.0)

	// Urgency: direct from [0,1].
	urgency := clamp01(task.Urgency)

	// Recency boost: tasks waiting longer than StarvationHours get a boost.
	recencyBoost := ComputeRecencyBoost(task.CreatedAt, now)

	// Risk score.
	riskScore := clamp01(task.RiskLevel)

	// Portfolio strategy boost (already bounded by provider, cap here for safety).
	stratBoost := clamp(portfolioBoost, -0.10, 0.12)

	priority := objectiveScore*WeightObjective +
		valueScore*WeightValue +
		urgency*WeightUrgency +
		recencyBoost*WeightRecency -
		riskScore*WeightRisk +
		stratBoost

	priority = clamp01(priority)

	// High-risk cap: tasks with risk > HighRiskThreshold capped at HighRiskMaxPrio.
	if task.RiskLevel > HighRiskThreshold {
		if priority > HighRiskMaxPrio {
			priority = HighRiskMaxPrio
		}
	}

	return priority
}

// ComputeRecencyBoost returns a starvation-protection boost for tasks waiting too long.
// Returns 0 for recent tasks, scaling up to 1.0 for very old tasks.
func ComputeRecencyBoost(createdAt time.Time, now time.Time) float64 {
	waitHours := now.Sub(createdAt).Hours()
	if waitHours < StarvationHours {
		return 0
	}
	// Linear ramp: full boost at 2x starvation threshold.
	boost := (waitHours - StarvationHours) / StarvationHours
	return clamp01(boost)
}

// IsExpired returns true if the task has exceeded its TTL.
func IsExpired(task OrchestratedTask, now time.Time) bool {
	return now.Sub(task.CreatedAt).Hours() > TaskTTLHours
}

// IsInCooldown returns true if the task was recently updated (within CooldownMinutes).
func IsInCooldown(task OrchestratedTask, now time.Time) bool {
	return now.Sub(task.UpdatedAt).Minutes() < CooldownMinutes
}

// ShouldReduceDispatch returns true if capacity is overloaded.
func ShouldReduceDispatch(capacityLoad float64) bool {
	return capacityLoad > OverloadThreshold
}

// clamp01 bounds a value to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clamp bounds a value to [min, max].
func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
