package actuation

import "sort"

// rules.go contains pure, deterministic rule evaluation for actuation.
// Each rule maps a reflection signal to one or more corrective action types.
// No ML, no randomness, no hidden heuristics.

// EvaluateRules maps reflection signals and objective state to actuation decisions.
// Returns a deterministic, bounded set of proposed actions.
// Rules are evaluated in fixed order. Duplicate action types are merged (highest priority wins).
func EvaluateRules(inputs ActuationInputs) []proposedAction {
	var proposals []proposedAction

	// --- Signal-driven rules ---
	for _, sig := range inputs.ReflectionSignals {
		if sig.Strength < MinSignalStrength {
			continue
		}

		switch sig.SignalType {
		case "low_efficiency":
			proposals = append(proposals,
				proposedAction{
					Type:       ActIncreaseDiscovery,
					Reason:     "low efficiency detected: increase pipeline discovery",
					Source:     sig.SignalType,
					Confidence: sig.Strength,
					Priority:   sig.Strength * 0.8,
				},
				proposedAction{
					Type:       ActTriggerAutomation,
					Reason:     "low efficiency detected: explore automation opportunities",
					Source:     sig.SignalType,
					Confidence: sig.Strength * 0.8,
					Priority:   sig.Strength * 0.6,
				},
			)

		case "pricing_misalignment":
			proposals = append(proposals, proposedAction{
				Type:       ActAdjustPricing,
				Reason:     "pricing misalignment detected: recalibrate price bands",
				Source:     sig.SignalType,
				Confidence: sig.Strength,
				Priority:   sig.Strength * 0.9,
			})

		case "overload_risk":
			proposals = append(proposals,
				proposedAction{
					Type:       ActReduceLoad,
					Reason:     "overload risk detected: defer lower-priority work",
					Source:     sig.SignalType,
					Confidence: sig.Strength,
					Priority:   sig.Strength * 0.9,
				},
				proposedAction{
					Type:       ActShiftScheduling,
					Reason:     "overload risk detected: redistribute scheduled items",
					Source:     sig.SignalType,
					Confidence: sig.Strength * 0.8,
					Priority:   sig.Strength * 0.7,
				},
			)

		case "income_instability":
			proposals = append(proposals, proposedAction{
				Type:       ActStabilizeIncome,
				Reason:     "income instability detected: stabilize revenue streams",
				Source:     sig.SignalType,
				Confidence: sig.Strength,
				Priority:   sig.Strength * 0.9,
			})

		case "automation_opportunity":
			proposals = append(proposals, proposedAction{
				Type:       ActTriggerAutomation,
				Reason:     "automation opportunity identified: evaluate automation candidates",
				Source:     sig.SignalType,
				Confidence: sig.Strength,
				Priority:   sig.Strength * 0.7,
			})
		}
	}

	// --- Objective-driven escalation rules ---

	// Low net utility escalates ALL action priorities.
	if inputs.NetUtility < LowUtilityThreshold {
		for i := range proposals {
			proposals[i].Priority = clamp01(proposals[i].Priority + PriorityEscalationBoost)
		}
	}

	// High financial risk prioritizes income stabilization.
	if inputs.FinancialRisk > HighFinancialRiskThreshold {
		proposals = append(proposals, proposedAction{
			Type:       ActStabilizeIncome,
			Reason:     "high financial risk: urgent income stabilization needed",
			Source:     "objective.financial_risk",
			Confidence: inputs.FinancialRisk,
			Priority:   clamp01(inputs.FinancialRisk),
		})
	}

	// High overload risk prioritizes load reduction.
	if inputs.OverloadRisk > HighOverloadRiskThreshold {
		proposals = append(proposals, proposedAction{
			Type:       ActReduceLoad,
			Reason:     "high overload risk: urgent load reduction needed",
			Source:     "objective.overload_risk",
			Confidence: inputs.OverloadRisk,
			Priority:   clamp01(inputs.OverloadRisk),
		})
	}

	// Deduplicate: keep the highest-priority proposal per action type.
	return deduplicateProposals(proposals)
}

// proposedAction is the intermediate result of rule evaluation before becoming an ActuationDecision.
type proposedAction struct {
	Type       ActuationType
	Reason     string
	Source     string
	Confidence float64
	Priority   float64
}

// deduplicateProposals keeps only the highest-priority proposal per action type.
// Output is sorted by type name for deterministic ordering.
func deduplicateProposals(proposals []proposedAction) []proposedAction {
	best := make(map[ActuationType]proposedAction)
	for _, p := range proposals {
		existing, ok := best[p.Type]
		if !ok || p.Priority > existing.Priority {
			best[p.Type] = p
		}
	}

	result := make([]proposedAction, 0, len(best))
	for _, p := range best {
		result = append(result, p)
	}

	// Sort by type name for deterministic output.
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Type) < string(result[j].Type)
	})

	// Cap at MaxDecisionsPerRun.
	if len(result) > MaxDecisionsPerRun {
		result = result[:MaxDecisionsPerRun]
	}
	return result
}

// clamp01 clamps a value to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ValidateTransition checks whether a state transition is allowed.
func ValidateTransition(from, to DecisionStatus) bool {
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
