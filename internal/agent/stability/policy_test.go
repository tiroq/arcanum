package stability

import (
	"testing"
	"time"
)

func normalState() *State {
	return &State{
		Mode:               ModeNormal,
		ThrottleMultiplier: 1.0,
		BlockedActionTypes: []string{},
	}
}

func TestApplyPolicy_CycleInstability_EntersSafeMode(t *testing.T) {
	current := normalState()
	result := DetectionResult{
		Findings: []DetectionFinding{
			{Finding: FindingCycleInstability},
		},
		Timestamp: time.Now(),
	}

	next, reason := ApplyPolicy(current, result)

	if next.Mode != ModeSafeMode {
		t.Errorf("expected safe_mode, got %s", next.Mode)
	}
	if next.ThrottleMultiplier != 3.0 {
		t.Errorf("expected throttle 3.0, got %f", next.ThrottleMultiplier)
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestApplyPolicy_NoopLoop_EntersThrottled(t *testing.T) {
	current := normalState()
	result := DetectionResult{
		Findings: []DetectionFinding{
			{Finding: FindingNoopLoop},
		},
		Timestamp: time.Now(),
	}

	next, _ := ApplyPolicy(current, result)

	if next.Mode != ModeThrottled {
		t.Errorf("expected throttled, got %s", next.Mode)
	}
	if next.ThrottleMultiplier != 2.0 {
		t.Errorf("expected throttle 2.0, got %f", next.ThrottleMultiplier)
	}
}

func TestApplyPolicy_LowValueLoop_BlocksAction(t *testing.T) {
	current := normalState()
	result := DetectionResult{
		Findings: []DetectionFinding{
			{Finding: FindingLowValueLoop, ActionType: "trigger_resync"},
		},
		Timestamp: time.Now(),
	}

	next, _ := ApplyPolicy(current, result)

	if next.Mode != ModeThrottled {
		t.Errorf("expected throttled, got %s", next.Mode)
	}
	if !next.IsActionBlocked("trigger_resync") {
		t.Error("expected trigger_resync to be blocked")
	}
}

func TestApplyPolicy_RetryAmplification_BlocksRetryJob(t *testing.T) {
	current := normalState()
	result := DetectionResult{
		Findings: []DetectionFinding{
			{Finding: FindingRetryAmplification, ActionType: "retry_job"},
		},
		Timestamp: time.Now(),
	}

	next, _ := ApplyPolicy(current, result)

	if !next.IsActionBlocked("retry_job") {
		t.Error("expected retry_job to be blocked")
	}
}

func TestApplyPolicy_Recovery_ReturnsToNormal(t *testing.T) {
	current := &State{
		Mode:               ModeThrottled,
		ThrottleMultiplier: 2.0,
		BlockedActionTypes: []string{"retry_job"},
	}
	result := DetectionResult{
		Findings: []DetectionFinding{
			{Finding: FindingStabilityRecovered},
		},
		Timestamp: time.Now(),
	}

	next, reason := ApplyPolicy(current, result)

	if next.Mode != ModeNormal {
		t.Errorf("expected normal, got %s", next.Mode)
	}
	if next.ThrottleMultiplier != 1.0 {
		t.Errorf("expected throttle 1.0, got %f", next.ThrottleMultiplier)
	}
	if len(next.BlockedActionTypes) != 0 {
		t.Errorf("expected empty blocklist, got %v", next.BlockedActionTypes)
	}
	if reason != "stability_recovered" {
		t.Errorf("expected reason 'stability_recovered', got %s", reason)
	}
}

func TestApplyPolicy_RecoveryIgnoredWithInstability(t *testing.T) {
	current := &State{
		Mode:               ModeThrottled,
		ThrottleMultiplier: 2.0,
		BlockedActionTypes: []string{},
	}
	// Both recovery and instability present — instability wins.
	result := DetectionResult{
		Findings: []DetectionFinding{
			{Finding: FindingStabilityRecovered},
			{Finding: FindingCycleInstability},
		},
		Timestamp: time.Now(),
	}

	next, _ := ApplyPolicy(current, result)

	if next.Mode != ModeSafeMode {
		t.Errorf("expected safe_mode (instability overrides recovery), got %s", next.Mode)
	}
}

func TestApplyPolicy_NoFindings_NoChange(t *testing.T) {
	current := normalState()
	result := DetectionResult{
		Findings:  []DetectionFinding{},
		Timestamp: time.Now(),
	}

	next, _ := ApplyPolicy(current, result)

	if next.Mode != ModeNormal {
		t.Errorf("expected normal unchanged, got %s", next.Mode)
	}
	if next.ThrottleMultiplier != 1.0 {
		t.Errorf("expected throttle 1.0, got %f", next.ThrottleMultiplier)
	}
}
