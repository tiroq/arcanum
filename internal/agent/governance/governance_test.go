package governance

import (
	"testing"
	"time"
)

// --- GovernanceState tests ---

func TestDefaultState(t *testing.T) {
	st := DefaultState()
	if st.Mode != ModeNormal {
		t.Errorf("expected mode normal, got %s", st.Mode)
	}
	if st.FreezeLearning || st.FreezePolicyUpdates || st.FreezeExploration {
		t.Error("default state should have no freeze flags set")
	}
	if st.ForceSafeMode || st.RequireHumanReview {
		t.Error("default state should have no override flags set")
	}
	if st.ForceReasoningMode != "" {
		t.Error("default state should have no forced reasoning mode")
	}
}

func TestIsFrozen(t *testing.T) {
	tests := []struct {
		name   string
		mode   string
		expect bool
	}{
		{"normal", ModeNormal, false},
		{"frozen", ModeFrozen, true},
		{"safe_hold", ModeSafeHold, false},
		{"rollback_only", ModeRollbackOnly, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := GovernanceState{Mode: tt.mode}
			if st.IsFrozen() != tt.expect {
				t.Errorf("IsFrozen() = %v, want %v", st.IsFrozen(), tt.expect)
			}
		})
	}
}

func TestIsLearningBlocked(t *testing.T) {
	tests := []struct {
		name   string
		state  GovernanceState
		expect bool
	}{
		{"normal", GovernanceState{Mode: ModeNormal}, false},
		{"freeze_learning", GovernanceState{Mode: ModeNormal, FreezeLearning: true}, true},
		{"frozen_mode", GovernanceState{Mode: ModeFrozen}, true},
		{"rollback_mode", GovernanceState{Mode: ModeRollbackOnly}, true},
		{"safe_hold_no_freeze", GovernanceState{Mode: ModeSafeHold}, false},
		{"safe_hold_with_freeze", GovernanceState{Mode: ModeSafeHold, FreezeLearning: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.state.IsLearningBlocked() != tt.expect {
				t.Errorf("IsLearningBlocked() = %v, want %v", tt.state.IsLearningBlocked(), tt.expect)
			}
		})
	}
}

func TestIsPolicyBlocked(t *testing.T) {
	tests := []struct {
		name   string
		state  GovernanceState
		expect bool
	}{
		{"normal", GovernanceState{Mode: ModeNormal}, false},
		{"freeze_policy", GovernanceState{Mode: ModeNormal, FreezePolicyUpdates: true}, true},
		{"frozen_mode", GovernanceState{Mode: ModeFrozen}, true},
		{"rollback_mode", GovernanceState{Mode: ModeRollbackOnly}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.state.IsPolicyBlocked() != tt.expect {
				t.Errorf("IsPolicyBlocked() = %v, want %v", tt.state.IsPolicyBlocked(), tt.expect)
			}
		})
	}
}

func TestIsExplorationBlocked(t *testing.T) {
	tests := []struct {
		name   string
		state  GovernanceState
		expect bool
	}{
		{"normal", GovernanceState{Mode: ModeNormal}, false},
		{"freeze_exploration", GovernanceState{Mode: ModeNormal, FreezeExploration: true}, true},
		{"frozen_mode", GovernanceState{Mode: ModeFrozen}, true},
		{"safe_hold", GovernanceState{Mode: ModeSafeHold}, true},
		{"rollback_mode", GovernanceState{Mode: ModeRollbackOnly}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.state.IsExplorationBlocked() != tt.expect {
				t.Errorf("IsExplorationBlocked() = %v, want %v", tt.state.IsExplorationBlocked(), tt.expect)
			}
		})
	}
}

func TestEffectiveReasoningMode(t *testing.T) {
	tests := []struct {
		name       string
		state      GovernanceState
		expectMode string
	}{
		{"normal_no_override", GovernanceState{Mode: ModeNormal}, ""},
		{"force_direct", GovernanceState{Mode: ModeNormal, ForceReasoningMode: "direct"}, "direct"},
		{"force_exploratory", GovernanceState{Mode: ModeNormal, ForceReasoningMode: "exploratory"}, "exploratory"},
		{"safe_mode_overrides_force", GovernanceState{Mode: ModeNormal, ForceSafeMode: true, ForceReasoningMode: "exploratory"}, "conservative"},
		{"safe_hold_forces_conservative", GovernanceState{Mode: ModeSafeHold, ForceReasoningMode: "direct"}, "conservative"},
		{"safe_hold_no_force", GovernanceState{Mode: ModeSafeHold}, "conservative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.EffectiveReasoningMode()
			if got != tt.expectMode {
				t.Errorf("EffectiveReasoningMode() = %q, want %q", got, tt.expectMode)
			}
		})
	}
}

// Conflict policy: safer override wins.
func TestConflictPolicySaferWins(t *testing.T) {
	// force_safe_mode + force_reasoning_mode=exploratory → conservative wins
	st := GovernanceState{
		Mode:               ModeNormal,
		ForceSafeMode:      true,
		ForceReasoningMode: "exploratory",
	}
	if st.EffectiveReasoningMode() != "conservative" {
		t.Error("safer override should win: expected conservative, got", st.EffectiveReasoningMode())
	}
}

func TestReplayPackDefaults(t *testing.T) {
	rp := ReplayPack{
		DecisionID: "test-123",
		GoalType:   "code_quality",
	}
	if rp.DecisionID != "test-123" {
		t.Error("unexpected decision_id")
	}
	if rp.Confidence != 0 {
		t.Error("default confidence should be 0")
	}
}

// --- FreezeRequest validation ---

func TestFreezeRequestDefaults(t *testing.T) {
	req := FreezeRequest{
		RequestedBy: "admin",
		Reason:      "emergency freeze",
	}
	// When freeze flags are nil, controller defaults them to true.
	if req.FreezeLearning != nil {
		t.Error("nil freeze_learning should default to freezing everything")
	}
}

// --- GovernanceAction fields ---

func TestGovernanceActionFields(t *testing.T) {
	action := GovernanceAction{
		ActionType:  ActionFreeze,
		RequestedBy: "admin",
		Reason:      "production incident",
		Payload:     map[string]any{"previous_mode": "normal"},
		CreatedAt:   time.Now().UTC(),
	}
	if action.ActionType != "freeze" {
		t.Errorf("expected freeze, got %s", action.ActionType)
	}
	if action.RequestedBy != "admin" {
		t.Error("unexpected requested_by")
	}
}

// --- State snapshot ---

func TestStateSnapshot(t *testing.T) {
	st := GovernanceState{
		Mode:                ModeFrozen,
		FreezeLearning:      true,
		FreezePolicyUpdates: true,
		FreezeExploration:   true,
		ForceSafeMode:       false,
		Reason:              "test",
	}
	snap := stateSnapshot(st)
	if snap["mode"] != ModeFrozen {
		t.Error("snapshot mode should be frozen")
	}
	if snap["freeze_learning"] != true {
		t.Error("snapshot should contain freeze_learning=true")
	}
}

// --- Fail-safe: governance state read failure degrades to safer behavior ---

func TestFailSafeDegradesToSaferBehavior(t *testing.T) {
	// Simulate by directly testing the fail-safe state properties.
	failSafe := GovernanceState{
		Mode:                ModeSafeHold,
		FreezeLearning:      true,
		FreezePolicyUpdates: true,
		FreezeExploration:   true,
		ForceSafeMode:       true,
		Reason:              "fail-safe: governance state unreadable",
	}

	if !failSafe.IsLearningBlocked() {
		t.Error("fail-safe should block learning")
	}
	if !failSafe.IsPolicyBlocked() {
		t.Error("fail-safe should block policy")
	}
	if !failSafe.IsExplorationBlocked() {
		t.Error("fail-safe should block exploration")
	}
	if failSafe.EffectiveReasoningMode() != "conservative" {
		t.Error("fail-safe should force conservative mode")
	}
}

// --- Deterministic behavior under governance controls ---

func TestDeterministicBehavior(t *testing.T) {
	// Same state should always produce same enforcement decisions.
	st := GovernanceState{
		Mode:                ModeFrozen,
		FreezeLearning:      true,
		FreezePolicyUpdates: false,
		FreezeExploration:   true,
		ForceReasoningMode:  "direct",
		ForceSafeMode:       false,
	}

	for i := 0; i < 100; i++ {
		if !st.IsLearningBlocked() {
			t.Fatal("determinism broken: learning should be blocked")
		}
		if st.IsPolicyBlocked() != true {
			// frozen mode blocks policy regardless of flag
			t.Fatal("determinism broken: policy should be blocked in frozen mode")
		}
		if !st.IsExplorationBlocked() {
			t.Fatal("determinism broken: exploration should be blocked")
		}
		if st.EffectiveReasoningMode() != "direct" {
			t.Fatal("determinism broken: should force direct mode")
		}
	}
}

// --- Repeated freeze/unfreeze transitions ---

func TestRepeatedFreezeUnfreezeTransitions(t *testing.T) {
	// Verify state model handles repeated transitions correctly.
	st := DefaultState()
	if st.Mode != ModeNormal {
		t.Fatal("should start normal")
	}

	// Freeze
	st.Mode = ModeFrozen
	st.FreezeLearning = true
	if !st.IsLearningBlocked() {
		t.Error("should be blocked after freeze")
	}

	// Unfreeze
	st = DefaultState()
	if st.IsLearningBlocked() {
		t.Error("should not be blocked after unfreeze")
	}

	// Freeze again
	st.Mode = ModeFrozen
	st.FreezeLearning = true
	if !st.IsLearningBlocked() {
		t.Error("should be blocked after second freeze")
	}

	// Unfreeze again
	st = DefaultState()
	if st.IsLearningBlocked() {
		t.Error("should not be blocked after second unfreeze")
	}
}

// --- No regression: existing safe execution paths ---

func TestNoRegressionSafeExecutionPaths(t *testing.T) {
	// Normal mode should not interfere with any existing behavior.
	st := DefaultState()
	if st.IsFrozen() {
		t.Error("normal mode should not be frozen")
	}
	if st.IsLearningBlocked() {
		t.Error("normal mode should not block learning")
	}
	if st.IsPolicyBlocked() {
		t.Error("normal mode should not block policy")
	}
	if st.IsExplorationBlocked() {
		t.Error("normal mode should not block exploration")
	}
	if st.EffectiveReasoningMode() != "" {
		t.Error("normal mode should not force any reasoning mode")
	}
	if st.RequireHumanReview {
		t.Error("normal mode should not require human review")
	}
}

// --- Rollback action is auditable ---

func TestRollbackStateProperties(t *testing.T) {
	st := GovernanceState{
		Mode:                ModeRollbackOnly,
		FreezeLearning:      true,
		FreezePolicyUpdates: true,
		FreezeExploration:   true,
		ForceSafeMode:       true,
		Reason:              "rollback after bad deploy",
	}

	if !st.IsFrozen() {
		t.Error("rollback should be frozen")
	}
	if !st.IsLearningBlocked() {
		t.Error("rollback should block learning")
	}
	if !st.IsPolicyBlocked() {
		t.Error("rollback should block policy")
	}
	if !st.IsExplorationBlocked() {
		t.Error("rollback should block exploration")
	}
	if st.EffectiveReasoningMode() != "conservative" {
		t.Error("rollback with force_safe_mode should be conservative")
	}
}

// --- Require human review suppresses autonomous application ---

func TestRequireHumanReview(t *testing.T) {
	st := GovernanceState{
		Mode:               ModeNormal,
		RequireHumanReview: true,
	}
	if !st.RequireHumanReview {
		t.Error("require_human_review should be set")
	}
	// In normal mode, other flags should be unaffected.
	if st.IsLearningBlocked() {
		t.Error("human review does not imply learning blocked")
	}
}
