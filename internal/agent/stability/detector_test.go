package stability

import (
	"testing"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/planning"
)

func makeDecisions(actions ...string) []planning.StoredDecision {
	var out []planning.StoredDecision
	for _, a := range actions {
		out = append(out, planning.StoredDecision{SelectedAction: a})
	}
	return out
}

func makeMemory(actionType string, totalRuns int, successRate, failureRate float64) actionmemory.ActionMemoryRecord {
	return actionmemory.ActionMemoryRecord{
		ActionType:  actionType,
		TotalRuns:   totalRuns,
		SuccessRate: successRate,
		FailureRate: failureRate,
	}
}

func TestDetect_NoopLoop(t *testing.T) {
	input := DetectionInput{
		RecentDecisions: makeDecisions("noop", "noop", "noop", "noop", "retry_job"),
		CurrentState:    &State{Mode: ModeNormal},
		Timestamp:       time.Now(),
	}

	result := Detect(input)

	found := false
	for _, f := range result.Findings {
		if f.Finding == FindingNoopLoop {
			found = true
		}
	}
	if !found {
		t.Errorf("expected FindingNoopLoop, got findings: %+v", result.Findings)
	}
}

func TestDetect_NoopLoop_BelowThreshold(t *testing.T) {
	// 2 noops out of 5 → 40%, below 60% threshold.
	input := DetectionInput{
		RecentDecisions: makeDecisions("noop", "noop", "retry_job", "retry_job", "trigger_resync"),
		CurrentState:    &State{Mode: ModeNormal},
		Timestamp:       time.Now(),
	}

	result := Detect(input)
	for _, f := range result.Findings {
		if f.Finding == FindingNoopLoop {
			t.Error("unexpected FindingNoopLoop for noop ratio below threshold")
		}
	}
}

func TestDetect_LowValueLoop(t *testing.T) {
	input := DetectionInput{
		RecentDecisions: makeDecisions("trigger_resync", "trigger_resync", "trigger_resync", "noop", "noop"),
		ActionMemory: []actionmemory.ActionMemoryRecord{
			makeMemory("trigger_resync", 5, 0.20, 0.80),
		},
		CurrentState: &State{Mode: ModeNormal},
		Timestamp:    time.Now(),
	}

	result := Detect(input)

	found := false
	for _, f := range result.Findings {
		if f.Finding == FindingLowValueLoop && f.ActionType == "trigger_resync" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected FindingLowValueLoop for trigger_resync, got findings: %+v", result.Findings)
	}
}

func TestDetect_CycleInstability(t *testing.T) {
	input := DetectionInput{
		RecentDecisions:   makeDecisions("noop"),
		RecentCycleErrors: 3,
		RecentCycleTotal:  5,
		CurrentState:      &State{Mode: ModeNormal},
		Timestamp:         time.Now(),
	}

	result := Detect(input)

	found := false
	for _, f := range result.Findings {
		if f.Finding == FindingCycleInstability {
			found = true
		}
	}
	if !found {
		t.Errorf("expected FindingCycleInstability, got findings: %+v", result.Findings)
	}
}

func TestDetect_RetryAmplification(t *testing.T) {
	input := DetectionInput{
		RecentDecisions: makeDecisions("retry_job", "retry_job", "retry_job", "noop"),
		ActionMemory: []actionmemory.ActionMemoryRecord{
			makeMemory("retry_job", 5, 0.10, 0.90),
		},
		CurrentState: &State{Mode: ModeNormal},
		Timestamp:    time.Now(),
	}

	result := Detect(input)

	found := false
	for _, f := range result.Findings {
		if f.Finding == FindingRetryAmplification {
			found = true
		}
	}
	if !found {
		t.Errorf("expected FindingRetryAmplification, got findings: %+v", result.Findings)
	}
}

func TestDetect_Recovery(t *testing.T) {
	// System in throttled mode but healthy recent pattern.
	input := DetectionInput{
		RecentDecisions: makeDecisions("retry_job", "trigger_resync", "retry_job", "trigger_resync", "retry_job"),
		ActionMemory: []actionmemory.ActionMemoryRecord{
			makeMemory("retry_job", 5, 0.80, 0.20),
		},
		RecentCycleErrors: 0,
		RecentCycleTotal:  5,
		CurrentState:      &State{Mode: ModeThrottled, ThrottleMultiplier: 2.0},
		Timestamp:         time.Now(),
	}

	result := Detect(input)

	found := false
	for _, f := range result.Findings {
		if f.Finding == FindingStabilityRecovered {
			found = true
		}
	}
	if !found {
		t.Errorf("expected FindingStabilityRecovered, got findings: %+v", result.Findings)
	}
}

func TestDetect_Recovery_NotTriggeredInNormalMode(t *testing.T) {
	input := DetectionInput{
		RecentDecisions:   makeDecisions("retry_job", "retry_job", "retry_job", "retry_job", "retry_job"),
		RecentCycleErrors: 0,
		RecentCycleTotal:  5,
		CurrentState:      &State{Mode: ModeNormal},
		Timestamp:         time.Now(),
	}

	result := Detect(input)
	for _, f := range result.Findings {
		if f.Finding == FindingStabilityRecovered {
			t.Error("should not trigger recovery when already in normal mode")
		}
	}
}
