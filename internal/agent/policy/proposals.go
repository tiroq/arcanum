package policy

import (
	"fmt"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/reflection"
)

// ProposalInput contains all signals for deterministic proposal generation.
type ProposalInput struct {
	ReflectionFindings []reflection.Finding
	ActionMemory       []actionmemory.ActionMemoryRecord
	CurrentValues      map[PolicyParam]float64
	StabilityMode      string // "normal", "throttled", "safe_mode"
	Now                time.Time
}

// GenerateProposals produces deterministic policy change proposals from
// observed signals. All 5 rules are evaluated; the caller filters and limits.
func GenerateProposals(input ProposalInput) []PolicyChange {
	var proposals []PolicyChange
	proposals = append(proposals, ruleRepeatedLowValue(input)...)
	proposals = append(proposals, rulePlannerIgnoresFeedback(input)...)
	proposals = append(proposals, ruleEffectivePattern(input)...)
	proposals = append(proposals, ruleHighNoopRatio(input)...)
	proposals = append(proposals, ruleRetryAmplification(input)...)
	return proposals
}

// Rule 1 — Repeated Low Value Action
// IF reflection finding repeated_low_value_action occurs >= 2 times
// THEN increase feedbackAvoidPenalty by +0.05
func ruleRepeatedLowValue(input ProposalInput) []PolicyChange {
	count := 0
	for _, f := range input.ReflectionFindings {
		if f.Rule == reflection.RuleRepeatedLowValue {
			count++
		}
	}
	if count < 2 {
		return nil
	}

	current := currentOrDefault(input.CurrentValues, ParamFeedbackAvoidPenalty)
	delta := 0.05
	newVal := current + delta

	return []PolicyChange{{
		Parameter:  ParamFeedbackAvoidPenalty,
		OldValue:   current,
		NewValue:   newVal,
		Delta:      delta,
		Reason:     fmt.Sprintf("repeated_low_value_action found %d times — increasing avoid penalty", count),
		Evidence:   map[string]any{"reflection_rule": "repeated_low_value_action", "occurrences": count},
		Confidence: 0.80,
	}}
}

// Rule 2 — Planner Ignores Feedback
// IF planner_ignores_feedback finding occurs >= 2 times
// THEN increase feedbackAvoidPenalty by +0.05
func rulePlannerIgnoresFeedback(input ProposalInput) []PolicyChange {
	count := 0
	for _, f := range input.ReflectionFindings {
		if f.Rule == reflection.RulePlannerIgnoresFeedback {
			count++
		}
	}
	if count < 2 {
		return nil
	}

	current := currentOrDefault(input.CurrentValues, ParamFeedbackAvoidPenalty)
	delta := 0.05
	newVal := current + delta

	return []PolicyChange{{
		Parameter:  ParamFeedbackAvoidPenalty,
		OldValue:   current,
		NewValue:   newVal,
		Delta:      delta,
		Reason:     fmt.Sprintf("planner_ignores_feedback found %d times — increasing avoid penalty", count),
		Evidence:   map[string]any{"reflection_rule": "planner_ignores_feedback", "occurrences": count},
		Confidence: 0.75,
	}}
}

// Rule 3 — Effective Action Pattern
// IF any action has success_rate >= 70% AND >= 5 samples
// THEN slightly increase feedbackPreferBoost by +0.03
// Confidence is scaled by evidence quality (recency × sample weight).
func ruleEffectivePattern(input ProposalInput) []PolicyChange {
	effectiveCount := 0
	var bestConfidence float64
	for _, m := range input.ActionMemory {
		if m.TotalRuns >= 5 && m.SuccessRate >= 0.70 {
			effectiveCount++
			if !input.Now.IsZero() {
				ec := actionmemory.EvidenceConfidence(
					actionmemory.RecencyWeight(m.LastUpdated, input.Now),
					actionmemory.SampleWeight(m.TotalRuns),
				)
				if ec > bestConfidence {
					bestConfidence = ec
				}
			}
		}
	}
	if effectiveCount == 0 {
		return nil
	}

	current := currentOrDefault(input.CurrentValues, ParamFeedbackPreferBoost)
	delta := 0.03
	newVal := current + delta

	proposalConf := 0.75
	if bestConfidence > 0 {
		proposalConf *= bestConfidence
	}

	return []PolicyChange{{
		Parameter:  ParamFeedbackPreferBoost,
		OldValue:   current,
		NewValue:   newVal,
		Delta:      delta,
		Reason:     fmt.Sprintf("effective_action_pattern: %d actions with success>=70%% — increasing prefer boost", effectiveCount),
		Evidence:   map[string]any{"effective_actions": effectiveCount, "evidence_confidence": bestConfidence},
		Confidence: proposalConf,
	}}
}

// Rule 4 — High Noop Ratio
// IF planner_stalling finding present AND stability not in safe_mode
// THEN increase noopBasePenalty by +0.05
func ruleHighNoopRatio(input ProposalInput) []PolicyChange {
	if input.StabilityMode == "safe_mode" {
		return nil
	}

	stallingCount := 0
	for _, f := range input.ReflectionFindings {
		if f.Rule == reflection.RulePlannerStalling {
			stallingCount++
		}
	}
	if stallingCount == 0 {
		return nil
	}

	current := currentOrDefault(input.CurrentValues, ParamNoopBasePenalty)
	delta := 0.05
	newVal := current + delta

	return []PolicyChange{{
		Parameter:  ParamNoopBasePenalty,
		OldValue:   current,
		NewValue:   newVal,
		Delta:      delta,
		Reason:     fmt.Sprintf("planner_stalling detected — increasing noop penalty"),
		Evidence:   map[string]any{"reflection_rule": "planner_stalling", "occurrences": stallingCount},
		Confidence: 0.70,
	}}
}

// Rule 5 — Retry Amplification
// IF retry_job failure_rate >= 50%
// THEN reduce highRetryBoost by -0.05
// Confidence is scaled by evidence quality (recency × sample weight).
func ruleRetryAmplification(input ProposalInput) []PolicyChange {
	for _, m := range input.ActionMemory {
		if m.ActionType == "retry_job" && m.TotalRuns >= 5 && m.FailureRate >= 0.50 {
			current := currentOrDefault(input.CurrentValues, ParamHighRetryBoost)
			delta := -0.05
			newVal := current + delta

			proposalConf := 0.80
			var ec float64
			if !input.Now.IsZero() {
				ec = actionmemory.EvidenceConfidence(
					actionmemory.RecencyWeight(m.LastUpdated, input.Now),
					actionmemory.SampleWeight(m.TotalRuns),
				)
				proposalConf *= ec
			}

			return []PolicyChange{{
				Parameter: ParamHighRetryBoost,
				OldValue:  current,
				NewValue:  newVal,
				Delta:     delta,
				Reason:    fmt.Sprintf("retry_job failure_rate=%.2f >= 50%% — reducing retry boost", m.FailureRate),
				Evidence: map[string]any{
					"action_type":         "retry_job",
					"total_runs":          m.TotalRuns,
					"failure_rate":        m.FailureRate,
					"evidence_confidence": ec,
				},
				Confidence: proposalConf,
			}}
		}
	}
	return nil
}

func currentOrDefault(vals map[PolicyParam]float64, p PolicyParam) float64 {
	if v, ok := vals[p]; ok {
		return v
	}
	if d, ok := DefaultValues[p]; ok {
		return d
	}
	return 0
}
