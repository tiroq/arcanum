package stability

import (
	"fmt"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/agent/reflection"
)

// DetectionInput bundles all data needed for stability analysis.
// Collected once before running rules — rules never query the database.
type DetectionInput struct {
	RecentDecisions   []planning.StoredDecision
	RecentOutcomes    []outcome.ActionOutcome
	ActionMemory      []actionmemory.ActionMemoryRecord
	RecentReflections []reflection.Finding
	RecentCycleErrors int
	RecentCycleTotal  int
	CurrentState      *State
	Timestamp         time.Time
}

// Thresholds — deterministic, tunable.
const (
	// Rule A: noop loop
	noopLoopMinDecisions = 5
	noopLoopMinRatio     = 0.60

	// Rule B: low-value loop
	lowValueMinSelections = 3
	lowValueMaxSuccess    = 0.30

	// Rule C: cycle instability
	cycleInstabilityMinTotal  = 3
	cycleInstabilityMinErrors = 2

	// Rule D: retry amplification
	retryAmpMinSelections = 3
	retryAmpMaxSuccess    = 0.30

	// Rule E: recovery
	recoveryMinDecisions    = 5
	recoveryMaxNoopRatio    = 0.30
	recoveryMinCyclesNeeded = 3
)

// Detect runs all 5 deterministic stability rules and returns findings.
func Detect(input DetectionInput) DetectionResult {
	var findings []DetectionFinding
	findings = append(findings, ruleNoopLoop(input)...)
	findings = append(findings, ruleLowValueLoop(input)...)
	findings = append(findings, ruleCycleInstability(input)...)
	findings = append(findings, ruleRetryAmplification(input)...)
	findings = append(findings, ruleRecovery(input)...)

	return DetectionResult{
		Findings:  findings,
		Timestamp: input.Timestamp,
	}
}

// Rule A: noop_loop_detected
// If noop is selected in >= 60% of recent decisions over at least 5 decisions.
func ruleNoopLoop(input DetectionInput) []DetectionFinding {
	total := len(input.RecentDecisions)
	if total < noopLoopMinDecisions {
		return nil
	}

	noopCount := 0
	for _, d := range input.RecentDecisions {
		if d.SelectedAction == "noop" {
			noopCount++
		}
	}

	ratio := float64(noopCount) / float64(total)
	if ratio < noopLoopMinRatio {
		return nil
	}

	return []DetectionFinding{{
		Finding: FindingNoopLoop,
		Detail: map[string]any{
			"noop_count":      noopCount,
			"total_decisions": total,
			"noop_ratio":      ratio,
		},
	}}
}

// Rule B: low_value_loop_detected
// If the same action type appears >= 3 times recently and most outcomes are neutral/failure.
func ruleLowValueLoop(input DetectionInput) []DetectionFinding {
	// Count selections per action type (excluding noop/log_recommendation).
	selectionCount := make(map[string]int)
	for _, d := range input.RecentDecisions {
		if d.SelectedAction == "noop" || d.SelectedAction == "log_recommendation" {
			continue
		}
		selectionCount[d.SelectedAction]++
	}

	// Build memory lookup.
	memByAction := make(map[string]*actionmemory.ActionMemoryRecord)
	for i := range input.ActionMemory {
		m := input.ActionMemory[i]
		memByAction[m.ActionType] = &m
	}

	var findings []DetectionFinding
	for actionType, count := range selectionCount {
		if count < lowValueMinSelections {
			continue
		}

		mem, ok := memByAction[actionType]
		if !ok || mem.TotalRuns < lowValueMinSelections {
			continue
		}
		if mem.SuccessRate > lowValueMaxSuccess {
			continue
		}

		findings = append(findings, DetectionFinding{
			Finding:    FindingLowValueLoop,
			ActionType: actionType,
			Detail: map[string]any{
				"selections":   count,
				"total_runs":   mem.TotalRuns,
				"success_rate": mem.SuccessRate,
				"failure_rate": mem.FailureRate,
			},
		})
	}
	return findings
}

// Rule C: cycle_instability_detected
// If multiple recent scheduler cycles fail or timeout.
func ruleCycleInstability(input DetectionInput) []DetectionFinding {
	if input.RecentCycleTotal < cycleInstabilityMinTotal {
		return nil
	}
	if input.RecentCycleErrors < cycleInstabilityMinErrors {
		return nil
	}

	ratio := float64(input.RecentCycleErrors) / float64(input.RecentCycleTotal)
	return []DetectionFinding{{
		Finding: FindingCycleInstability,
		Detail: map[string]any{
			"cycle_errors": input.RecentCycleErrors,
			"cycle_total":  input.RecentCycleTotal,
			"error_ratio":  ratio,
		},
	}}
}

// Rule D: retry_amplification_detected
// If retry_job dominates recent actions and outcomes remain poor.
func ruleRetryAmplification(input DetectionInput) []DetectionFinding {
	retryCount := 0
	for _, d := range input.RecentDecisions {
		if d.SelectedAction == "retry_job" {
			retryCount++
		}
	}
	if retryCount < retryAmpMinSelections {
		return nil
	}

	// Check outcome memory for retry_job.
	memByAction := make(map[string]*actionmemory.ActionMemoryRecord)
	for i := range input.ActionMemory {
		m := input.ActionMemory[i]
		memByAction[m.ActionType] = &m
	}

	mem, ok := memByAction["retry_job"]
	if !ok || mem.TotalRuns < retryAmpMinSelections {
		return nil
	}
	if mem.SuccessRate > retryAmpMaxSuccess {
		return nil
	}

	return []DetectionFinding{{
		Finding:    FindingRetryAmplification,
		ActionType: "retry_job",
		Detail: map[string]any{
			"retry_selections": retryCount,
			"total_runs":       mem.TotalRuns,
			"success_rate":     mem.SuccessRate,
			"failure_rate":     mem.FailureRate,
		},
	}}
}

// Rule E: stability_recovered
// If recent cycles are healthy again: low noop ratio, no cycle errors,
// and system was previously in a degraded mode.
func ruleRecovery(input DetectionInput) []DetectionFinding {
	// Only relevant if system is currently degraded.
	if input.CurrentState == nil || input.CurrentState.Mode == ModeNormal {
		return nil
	}

	total := len(input.RecentDecisions)
	if total < recoveryMinDecisions {
		return nil
	}

	// Check noop ratio is low.
	noopCount := 0
	for _, d := range input.RecentDecisions {
		if d.SelectedAction == "noop" {
			noopCount++
		}
	}
	noopRatio := float64(noopCount) / float64(total)
	if noopRatio > recoveryMaxNoopRatio {
		return nil
	}

	// Check no recent cycle errors.
	if input.RecentCycleTotal >= recoveryMinCyclesNeeded && input.RecentCycleErrors > 0 {
		return nil
	}

	return []DetectionFinding{{
		Finding: FindingStabilityRecovered,
		Detail: map[string]any{
			"noop_ratio":          noopRatio,
			"recent_cycle_errors": input.RecentCycleErrors,
			"previous_mode":       string(input.CurrentState.Mode),
			"reason": fmt.Sprintf(
				"noop_ratio=%.2f, cycle_errors=%d — system appears healthy",
				noopRatio, input.RecentCycleErrors,
			),
		},
	}}
}
