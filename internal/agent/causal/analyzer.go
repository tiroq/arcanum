package causal

import (
	"fmt"

	"github.com/google/uuid"
)

// Analyze runs all deterministic causal rules against the input and returns
// causal attributions. Pure function — no side effects.
func Analyze(input AnalysisInput) []CausalAttribution {
	var attributions []CausalAttribution
	attributions = append(attributions, analyzePolicyChanges(input)...)
	attributions = append(attributions, analyzeStabilityIntervention(input)...)
	attributions = append(attributions, analyzePlannerConditions(input)...)
	return attributions
}

// --- Rule 1 + 2 + 3 + 5: Policy change attribution ---

func analyzePolicyChanges(input AnalysisInput) []CausalAttribution {
	var out []CausalAttribution

	for _, pc := range input.RecentPolicyChanges {
		if !pc.Applied {
			continue
		}

		a := CausalAttribution{
			SubjectType: SubjectPolicyChange,
			SubjectID:   pc.ID,
			Evidence: map[string]any{
				"parameter": pc.Parameter,
				"old_value": pc.OldValue,
				"new_value": pc.NewValue,
				"applied":   pc.Applied,
			},
			CreatedAt: input.Timestamp,
		}

		// Rule 5: no causal support — improvement not detected or unknown.
		if pc.ImprovementDetected == nil {
			a.Hypothesis = fmt.Sprintf("policy change to %s has not yet been evaluated", pc.Parameter)
			a.Attribution = AttributionAmbiguous
			a.Confidence = 0.20
			a.Evidence["reason"] = "change not yet evaluated"
			a.CompetingExplanations = []string{
				"insufficient time elapsed for evaluation",
				"target metric may not have moved yet",
			}
			out = append(out, a)
			continue
		}

		improved := *pc.ImprovementDetected

		// Rule 3: ambiguous — too many simultaneous changes.
		if input.SimultaneousChanges > 2 {
			a.Hypothesis = fmt.Sprintf("policy change to %s occurred alongside %d other changes — ambiguous", pc.Parameter, input.SimultaneousChanges-1)
			a.Attribution = AttributionAmbiguous
			a.Confidence = 0.30
			a.Evidence["simultaneous_changes"] = input.SimultaneousChanges
			a.Evidence["improvement_detected"] = improved
			a.CompetingExplanations = []string{
				"multiple simultaneous policy changes make isolation impossible",
				"improvement may be caused by a different change",
				"natural system recovery unrelated to any policy change",
			}
			out = append(out, a)
			continue
		}

		// Check for external instability.
		hasExternalInstability := input.ProviderInstability || input.CycleInstability || input.HighSystemFailure

		if improved && !hasExternalInstability {
			// Rule 1: likely internal cause.
			a.Hypothesis = fmt.Sprintf("policy change to %s (%.3f → %.3f) likely caused observed improvement", pc.Parameter, pc.OldValue, pc.NewValue)
			a.Attribution = AttributionInternal
			a.Confidence = 0.75
			a.Evidence["improvement_detected"] = true
			a.Evidence["external_instability"] = false
			a.CompetingExplanations = []string{
				"temporary recovery unrelated to policy change",
				"workload pattern shift coinciding with change",
			}
		} else if improved && hasExternalInstability {
			// Rule 2: mixed — improvement detected but external factors present.
			a.Hypothesis = fmt.Sprintf("policy change to %s improved metrics but external instability is also present", pc.Parameter)
			a.Attribution = AttributionMixed
			a.Confidence = 0.50
			a.Evidence["improvement_detected"] = true
			a.Evidence["external_instability"] = true
			a.Evidence["provider_instability"] = input.ProviderInstability
			a.Evidence["cycle_instability"] = input.CycleInstability
			a.Evidence["high_system_failure"] = input.HighSystemFailure
			a.CompetingExplanations = buildExternalExplanations(input)
		} else if !improved && hasExternalInstability {
			// Rule 2: likely external — metric didn't improve and external issues exist.
			a.Hypothesis = fmt.Sprintf("policy change to %s did not improve metrics; external instability likely the dominant factor", pc.Parameter)
			a.Attribution = AttributionExternal
			a.Confidence = 0.60
			a.Evidence["improvement_detected"] = false
			a.Evidence["external_instability"] = true
			a.CompetingExplanations = append(
				[]string{"policy change may have been counterproductive"},
				buildExternalExplanations(input)...,
			)
		} else {
			// Rule 5: no causal support — not improved, but no external cause either.
			a.Hypothesis = fmt.Sprintf("policy change to %s did not produce expected improvement — cause unclear", pc.Parameter)
			a.Attribution = AttributionAmbiguous
			a.Confidence = 0.35
			a.Evidence["improvement_detected"] = false
			a.Evidence["external_instability"] = false
			a.CompetingExplanations = []string{
				"parameter change may be too small to have visible effect",
				"target metric may respond on a longer timescale",
				"policy change may have been counterproductive",
			}
		}

		out = append(out, a)
	}
	return out
}

// --- Rule 4: Stability intervention effectiveness ---

func analyzeStabilityIntervention(input AnalysisInput) []CausalAttribution {
	if !input.StabilityChanged {
		return nil
	}

	a := CausalAttribution{
		SubjectType: SubjectStabilityEvent,
		SubjectID:   uuid.Nil,
		Evidence: map[string]any{
			"previous_mode": input.PreviousMode,
			"current_mode":  input.StabilityMode,
		},
		CreatedAt: input.Timestamp,
	}

	// Mode escalated (normal → throttled, normal → safe_mode, throttled → safe_mode).
	if isEscalation(input.PreviousMode, input.StabilityMode) {
		// Check if harmful patterns decreased after intervention.
		hasActiveHarmfulPatterns := input.HighSystemFailure || input.CycleInstability
		if !hasActiveHarmfulPatterns {
			// Rule 4: stability mechanism was effective.
			a.Hypothesis = fmt.Sprintf("stability escalation from %s to %s likely reduced harmful patterns", input.PreviousMode, input.StabilityMode)
			a.Attribution = AttributionInternal
			a.Confidence = 0.70
			a.Evidence["harmful_patterns_active"] = false
			a.CompetingExplanations = []string{
				"harmful patterns may have subsided naturally",
				"workload reduction coinciding with stability change",
				"scheduler throttling reduced action volume indirectly",
			}
		} else {
			// Harmful patterns still present — mixed.
			a.Hypothesis = fmt.Sprintf("stability escalation from %s to %s applied but harmful patterns persist", input.PreviousMode, input.StabilityMode)
			a.Attribution = AttributionMixed
			a.Confidence = 0.45
			a.Evidence["harmful_patterns_active"] = true
			a.CompetingExplanations = []string{
				"intervention too recent to take effect",
				"external factors overwhelming stability controls",
				"stability controls insufficient for current conditions",
			}
		}
		return []CausalAttribution{a}
	}

	// Mode de-escalated (safe_mode → throttled, throttled → normal, safe_mode → normal).
	if isDeescalation(input.PreviousMode, input.StabilityMode) {
		a.Hypothesis = fmt.Sprintf("stability de-escalation from %s to %s indicates recovery", input.PreviousMode, input.StabilityMode)
		a.Attribution = AttributionInternal
		a.Confidence = 0.65
		a.Evidence["recovery"] = true
		a.CompetingExplanations = []string{
			"recovery may be temporary",
			"external conditions improved independently",
		}
		return []CausalAttribution{a}
	}

	return nil
}

// --- Rule 2 / 3: Planner degradation from external conditions ---

func analyzePlannerConditions(input AnalysisInput) []CausalAttribution {
	// Only produce a planner attribution if there's clear external pressure
	// without corresponding internal policy changes.
	if !input.HighSystemFailure && !input.ProviderInstability {
		return nil
	}
	if len(input.RecentPolicyChanges) > 0 {
		// Policy changes present — covered by policy analysis above.
		return nil
	}

	a := CausalAttribution{
		SubjectType: SubjectPlannerShift,
		SubjectID:   uuid.Nil,
		Evidence: map[string]any{
			"provider_instability": input.ProviderInstability,
			"high_system_failure":  input.HighSystemFailure,
			"cycle_instability":    input.CycleInstability,
		},
		CreatedAt: input.Timestamp,
	}

	a.Hypothesis = "planner behavior degradation likely caused by external system conditions"
	a.Attribution = AttributionExternal
	a.Confidence = 0.65
	a.CompetingExplanations = []string{
		"accumulated internal parameter drift",
		"action memory becoming stale",
		"scheduler timing issues",
	}

	return []CausalAttribution{a}
}

// --- Helpers ---

func buildExternalExplanations(input AnalysisInput) []string {
	var out []string
	if input.ProviderInstability {
		out = append(out, "provider degradation affecting outcomes")
	}
	if input.CycleInstability {
		out = append(out, "cycle errors reducing action effectiveness")
	}
	if input.HighSystemFailure {
		out = append(out, "high system failure rate distorting metrics")
	}
	out = append(out, "temporary recovery unrelated to policy change")
	return out
}

var modeOrdinal = map[string]int{
	"normal":    0,
	"throttled": 1,
	"safe_mode": 2,
}

func isEscalation(prev, curr string) bool {
	return modeOrdinal[curr] > modeOrdinal[prev]
}

func isDeescalation(prev, curr string) bool {
	return modeOrdinal[curr] < modeOrdinal[prev]
}
