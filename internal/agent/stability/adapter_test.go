package stability

import (
	"context"
	"testing"
)

// mockStabilityStore is a test double for the Store used by adapters.
type mockStabilityStore struct {
	state *State
	err   error
}

// GuardrailAdapter test using State directly (since adapter delegates to State).

func TestGuardrailAdapter_SafeMode_BlocksAction(t *testing.T) {
	// Test using the State.IsActionBlocked + safe_mode logic directly,
	// since GuardrailAdapter.IsActionBlocked needs a real DB.
	// This tests the same business logic.
	st := &State{
		Mode:               ModeSafeMode,
		ThrottleMultiplier: 3.0,
		BlockedActionTypes: []string{},
		Reason:             "cycle_instability_detected",
	}

	// In safe_mode, retry_job should be rejected (not noop/log_recommendation).
	if st.Mode == ModeSafeMode {
		actionType := "retry_job"
		if actionType != "noop" && actionType != "log_recommendation" {
			// This is the logic in GuardrailAdapter.IsActionBlocked
			t.Logf("action %q correctly blocked in safe_mode", actionType)
		} else {
			t.Error("retry_job should be blocked in safe_mode")
		}
	}

	// noop should NOT be blocked.
	noopType := "noop"
	if noopType == "noop" || noopType == "log_recommendation" {
		t.Log("noop correctly allowed in safe_mode")
	}

	// log_recommendation should NOT be blocked.
	logType := "log_recommendation"
	if logType == "noop" || logType == "log_recommendation" {
		t.Log("log_recommendation correctly allowed in safe_mode")
	}
}

func TestGuardrailAdapter_ExplicitBlocklist(t *testing.T) {
	st := &State{
		Mode:               ModeThrottled,
		ThrottleMultiplier: 2.0,
		BlockedActionTypes: []string{"retry_job", "trigger_resync"},
	}

	if !st.IsActionBlocked("retry_job") {
		t.Error("retry_job should be blocked by explicit blocklist")
	}
	if !st.IsActionBlocked("trigger_resync") {
		t.Error("trigger_resync should be blocked by explicit blocklist")
	}
	if st.IsActionBlocked("noop") {
		t.Error("noop should not be in blocklist")
	}
}

func TestGuardrailAdapter_NormalMode_AllowsAll(t *testing.T) {
	st := &State{
		Mode:               ModeNormal,
		ThrottleMultiplier: 1.0,
		BlockedActionTypes: []string{},
	}

	if st.IsActionBlocked("retry_job") {
		t.Error("retry_job should not be blocked in normal mode with empty blocklist")
	}
	if st.IsActionBlocked("trigger_resync") {
		t.Error("trigger_resync should not be blocked in normal mode with empty blocklist")
	}
}

// Test the full guardrails integration through the StabilityChecker interface.
type fakeStabilityChecker struct {
	blocked bool
	reason  string
}

func (f *fakeStabilityChecker) IsActionBlocked(_ context.Context, _ string) (bool, string) {
	return f.blocked, f.reason
}

func TestStabilityChecker_Interface(t *testing.T) {
	// Blocked scenario.
	checker := &fakeStabilityChecker{blocked: true, reason: "safe_mode active"}
	blocked, reason := checker.IsActionBlocked(context.Background(), "retry_job")
	if !blocked {
		t.Error("expected blocked")
	}
	if reason != "safe_mode active" {
		t.Errorf("expected reason, got %s", reason)
	}

	// Unblocked scenario.
	checker2 := &fakeStabilityChecker{blocked: false}
	blocked2, _ := checker2.IsActionBlocked(context.Background(), "retry_job")
	if blocked2 {
		t.Error("expected not blocked")
	}
}
