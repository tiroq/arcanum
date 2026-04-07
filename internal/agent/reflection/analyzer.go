package reflection

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	"github.com/tiroq/arcanum/internal/agent/planning"
)

// AnalysisInput bundles all data needed for reflection analysis.
// Collected once before running rules — rules never query the database.
type AnalysisInput struct {
	RecentDecisions []planning.StoredDecision
	RecentOutcomes  []outcome.ActionOutcome
	ActionMemory    []actionmemory.ActionMemoryRecord
	CycleID         string
	Timestamp       time.Time
}

// analysisContext is a pre-computed lookup structure built from AnalysisInput.
type analysisContext struct {
	input AnalysisInput

	// outcomesByAction maps action_type to its outcomes.
	outcomesByAction map[string][]outcome.ActionOutcome
	// memoryByAction maps action_type to its memory record.
	memoryByAction map[string]*actionmemory.ActionMemoryRecord
	// feedbackByAction maps action_type to its computed feedback.
	feedbackByAction map[string]actionmemory.ActionFeedback
	// decisionsByAction maps selected_action to decisions.
	decisionsByAction map[string][]planning.StoredDecision
}

func buildContext(input AnalysisInput) *analysisContext {
	ac := &analysisContext{
		input:             input,
		outcomesByAction:  make(map[string][]outcome.ActionOutcome),
		memoryByAction:    make(map[string]*actionmemory.ActionMemoryRecord),
		feedbackByAction:  make(map[string]actionmemory.ActionFeedback),
		decisionsByAction: make(map[string][]planning.StoredDecision),
	}

	for i := range input.RecentOutcomes {
		o := input.RecentOutcomes[i]
		ac.outcomesByAction[o.ActionType] = append(ac.outcomesByAction[o.ActionType], o)
	}
	for i := range input.ActionMemory {
		m := input.ActionMemory[i]
		ac.memoryByAction[m.ActionType] = &m
		ac.feedbackByAction[m.ActionType] = actionmemory.GenerateFeedback(&m)
	}
	for i := range input.RecentDecisions {
		d := input.RecentDecisions[i]
		ac.decisionsByAction[d.SelectedAction] = append(ac.decisionsByAction[d.SelectedAction], d)
	}

	return ac
}

// Analyze runs all 5 deterministic reflection rules and returns findings.
func Analyze(input AnalysisInput) []Finding {
	ac := buildContext(input)

	var findings []Finding
	findings = append(findings, ruleRepeatedLowValue(ac)...)
	findings = append(findings, rulePlannerIgnoresFeedback(ac)...)
	findings = append(findings, rulePlannerStalling(ac)...)
	findings = append(findings, ruleUnstableEffectiveness(ac)...)
	findings = append(findings, ruleEffectivePattern(ac)...)
	return findings
}

// Thresholds — all tunable but deterministic.
const (
	// Rule A: repeated_low_value_action
	repeatedLowValueMinSelections = 3
	repeatedLowValueMaxSuccess    = 0.3

	// Rule B: planner_ignores_feedback
	// (triggered when planner selects an action that memory says to avoid)

	// Rule C: planner_stalling
	stallingNoopMinRatio = 0.6
	stallingMinDecisions = 5

	// Rule D: unstable_action_effectiveness
	unstableMinSamples   = 5
	unstableVarianceHigh = 0.20

	// Rule E: effective_action_pattern
	effectiveMinSelections = 3
	effectiveMinSuccess    = 0.7
)

// Rule A: repeated_low_value_action
// Same action selected repeatedly but outcomes are poor (high failure or neutral).
func ruleRepeatedLowValue(ac *analysisContext) []Finding {
	var findings []Finding

	for actionType, decisions := range ac.decisionsByAction {
		if actionType == "noop" || actionType == "log_recommendation" {
			continue
		}
		if len(decisions) < repeatedLowValueMinSelections {
			continue
		}

		mem, ok := ac.memoryByAction[actionType]
		if !ok || mem.TotalRuns < repeatedLowValueMinSelections {
			continue
		}
		if mem.SuccessRate > repeatedLowValueMaxSuccess {
			continue
		}

		findings = append(findings, Finding{
			ID:         uuid.New(),
			CycleID:    ac.input.CycleID,
			Rule:       RuleRepeatedLowValue,
			Severity:   SeverityWarning,
			ActionType: actionType,
			Summary: fmt.Sprintf(
				"Action %q selected %d times but success rate is only %.0f%%",
				actionType, len(decisions), mem.SuccessRate*100,
			),
			Detail: map[string]any{
				"selections":   len(decisions),
				"total_runs":   mem.TotalRuns,
				"success_rate": mem.SuccessRate,
				"failure_rate": mem.FailureRate,
			},
			CreatedAt: ac.input.Timestamp,
		})
	}

	return findings
}

// Rule B: planner_ignores_feedback
// Planner selected an action that action memory recommends avoiding.
func rulePlannerIgnoresFeedback(ac *analysisContext) []Finding {
	var findings []Finding

	for actionType, decisions := range ac.decisionsByAction {
		if actionType == "noop" || actionType == "log_recommendation" {
			continue
		}

		fb, ok := ac.feedbackByAction[actionType]
		if !ok {
			continue
		}
		if fb.Recommendation != actionmemory.RecommendAvoidAction {
			continue
		}

		findings = append(findings, Finding{
			ID:         uuid.New(),
			CycleID:    ac.input.CycleID,
			Rule:       RulePlannerIgnoresFeedback,
			Severity:   SeverityWarning,
			ActionType: actionType,
			Summary: fmt.Sprintf(
				"Action %q selected %d times despite avoid_action feedback (failure_rate=%.0f%%)",
				actionType, len(decisions), fb.FailureRate*100,
			),
			Detail: map[string]any{
				"selections":     len(decisions),
				"recommendation": string(fb.Recommendation),
				"failure_rate":   fb.FailureRate,
				"sample_size":    fb.SampleSize,
			},
			CreatedAt: ac.input.Timestamp,
		})
	}

	return findings
}

// Rule C: planner_stalling
// Majority of recent decisions select noop — the planner is stuck.
func rulePlannerStalling(ac *analysisContext) []Finding {
	total := len(ac.input.RecentDecisions)
	if total < stallingMinDecisions {
		return nil
	}

	noopCount := 0
	for _, d := range ac.input.RecentDecisions {
		if d.SelectedAction == "noop" {
			noopCount++
		}
	}

	ratio := float64(noopCount) / float64(total)
	if ratio < stallingNoopMinRatio {
		return nil
	}

	return []Finding{{
		ID:         uuid.New(),
		CycleID:    ac.input.CycleID,
		Rule:       RulePlannerStalling,
		Severity:   SeverityWarning,
		ActionType: "noop",
		Summary: fmt.Sprintf(
			"Planner selected noop in %d of %d recent decisions (%.0f%%)",
			noopCount, total, ratio*100,
		),
		Detail: map[string]any{
			"noop_count":      noopCount,
			"total_decisions": total,
			"noop_ratio":      ratio,
		},
		CreatedAt: ac.input.Timestamp,
	}}
}

// Rule D: unstable_action_effectiveness
// An action's outcome status varies widely — high variance suggests unreliable behavior.
func ruleUnstableEffectiveness(ac *analysisContext) []Finding {
	var findings []Finding

	for actionType, outcomes := range ac.outcomesByAction {
		if len(outcomes) < unstableMinSamples {
			continue
		}

		// Compute success ratio variance using a sliding window approach.
		// We score each outcome: success=1, neutral=0.5, failure=0.
		scores := make([]float64, len(outcomes))
		var sum float64
		for i, o := range outcomes {
			switch o.OutcomeStatus {
			case outcome.OutcomeSuccess:
				scores[i] = 1.0
			case outcome.OutcomeNeutral:
				scores[i] = 0.5
			case outcome.OutcomeFailure:
				scores[i] = 0.0
			}
			sum += scores[i]
		}
		mean := sum / float64(len(scores))

		var varianceSum float64
		for _, s := range scores {
			varianceSum += (s - mean) * (s - mean)
		}
		variance := varianceSum / float64(len(scores))
		stddev := math.Sqrt(variance)

		if stddev < unstableVarianceHigh {
			continue
		}

		findings = append(findings, Finding{
			ID:         uuid.New(),
			CycleID:    ac.input.CycleID,
			Rule:       RuleUnstableEffectiveness,
			Severity:   SeverityWarning,
			ActionType: actionType,
			Summary: fmt.Sprintf(
				"Action %q has unstable effectiveness (stddev=%.2f across %d outcomes)",
				actionType, stddev, len(outcomes),
			),
			Detail: map[string]any{
				"outcome_count": len(outcomes),
				"mean_score":    mean,
				"stddev":        stddev,
				"variance":      variance,
			},
			CreatedAt: ac.input.Timestamp,
		})
	}

	return findings
}

// Rule E: effective_action_pattern
// An action is consistently successful — worth highlighting as a positive signal.
func ruleEffectivePattern(ac *analysisContext) []Finding {
	var findings []Finding

	for actionType, decisions := range ac.decisionsByAction {
		if actionType == "noop" || actionType == "log_recommendation" {
			continue
		}
		if len(decisions) < effectiveMinSelections {
			continue
		}

		mem, ok := ac.memoryByAction[actionType]
		if !ok || mem.TotalRuns < effectiveMinSelections {
			continue
		}
		if mem.SuccessRate < effectiveMinSuccess {
			continue
		}

		findings = append(findings, Finding{
			ID:         uuid.New(),
			CycleID:    ac.input.CycleID,
			Rule:       RuleEffectivePattern,
			Severity:   SeverityInfo,
			ActionType: actionType,
			Summary: fmt.Sprintf(
				"Action %q is effective: selected %d times with %.0f%% success rate",
				actionType, len(decisions), mem.SuccessRate*100,
			),
			Detail: map[string]any{
				"selections":   len(decisions),
				"total_runs":   mem.TotalRuns,
				"success_rate": mem.SuccessRate,
			},
			CreatedAt: ac.input.Timestamp,
		})
	}

	return findings
}
