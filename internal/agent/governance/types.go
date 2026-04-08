package governance

import "time"

// Governance modes.
const (
	ModeNormal       = "normal"
	ModeFrozen       = "frozen"
	ModeSafeHold     = "safe_hold"
	ModeRollbackOnly = "rollback_only"
)

// Action types for operator overrides.
const (
	ActionFreeze        = "freeze"
	ActionUnfreeze      = "unfreeze"
	ActionSafeHold      = "safe_hold"
	ActionRollback      = "rollback"
	ActionForceMode     = "force_mode"
	ActionRequireReview = "require_review"
	ActionClearOverride = "clear_override"
)

// Valid governance modes for validation.
var validModes = map[string]bool{
	ModeNormal:       true,
	ModeFrozen:       true,
	ModeSafeHold:     true,
	ModeRollbackOnly: true,
}

// Valid reasoning modes that can be forced.
var validReasoningModes = map[string]bool{
	"":             true, // empty = no forced mode
	"graph":        true,
	"direct":       true,
	"conservative": true,
	"exploratory":  true,
}

// GovernanceState represents the current governance configuration for the system.
// Stored as a single row in agent_governance_state.
type GovernanceState struct {
	Mode                string    `json:"mode"`                  // normal | frozen | safe_hold | rollback_only
	FreezeLearning      bool      `json:"freeze_learning"`       // block learning writes
	FreezePolicyUpdates bool      `json:"freeze_policy_updates"` // block policy changes
	FreezeExploration   bool      `json:"freeze_exploration"`    // disable exploration
	ForceReasoningMode  string    `json:"force_reasoning_mode"`  // optional: graph/direct/conservative/exploratory
	ForceSafeMode       bool      `json:"force_safe_mode"`       // force conservative/safe path
	RequireHumanReview  bool      `json:"require_human_review"`  // suppress autonomous application
	LastUpdated         time.Time `json:"last_updated"`
	Reason              string    `json:"reason"`
}

// DefaultState returns the default (normal) governance state.
func DefaultState() GovernanceState {
	return GovernanceState{
		Mode:        ModeNormal,
		LastUpdated: time.Now().UTC(),
		Reason:      "default",
	}
}

// IsFrozen returns true if any freeze flag is active or mode is frozen/rollback_only.
func (s GovernanceState) IsFrozen() bool {
	return s.Mode == ModeFrozen || s.Mode == ModeRollbackOnly
}

// IsLearningBlocked returns true if learning writes should be suppressed.
func (s GovernanceState) IsLearningBlocked() bool {
	return s.FreezeLearning || s.Mode == ModeFrozen || s.Mode == ModeRollbackOnly
}

// IsPolicyBlocked returns true if policy updates should be suppressed.
func (s GovernanceState) IsPolicyBlocked() bool {
	return s.FreezePolicyUpdates || s.Mode == ModeFrozen || s.Mode == ModeRollbackOnly
}

// IsExplorationBlocked returns true if exploration should be disabled.
func (s GovernanceState) IsExplorationBlocked() bool {
	return s.FreezeExploration || s.Mode == ModeFrozen || s.Mode == ModeSafeHold || s.Mode == ModeRollbackOnly
}

// EffectiveReasoningMode returns the reasoning mode to use.
// If ForceReasoningMode is set, it takes precedence.
// If ForceSafeMode is true, returns "conservative".
// Empty string means no forced mode.
func (s GovernanceState) EffectiveReasoningMode() string {
	// Safer override wins: safe_mode overrides forced mode.
	if s.ForceSafeMode || s.Mode == ModeSafeHold {
		return "conservative"
	}
	if s.ForceReasoningMode != "" {
		return s.ForceReasoningMode
	}
	return ""
}

// GovernanceAction records an operator action in the governance action log.
type GovernanceAction struct {
	ID          string         `json:"id"`
	ActionType  string         `json:"action_type"`
	RequestedBy string         `json:"requested_by"`
	Reason      string         `json:"reason"`
	Payload     map[string]any `json:"payload"`
	CreatedAt   time.Time      `json:"created_at"`
}

// ReplayPack contains a decision explanation/replay pack.
type ReplayPack struct {
	ID                 string         `json:"id"`
	DecisionID         string         `json:"decision_id"`
	GoalType           string         `json:"goal_type"`
	SelectedMode       string         `json:"selected_mode"`
	SelectedPath       string         `json:"selected_path"`
	Confidence         float64        `json:"confidence"`
	Signals            map[string]any `json:"signals"`
	ArbitrationTrace   map[string]any `json:"arbitration_trace"`
	CalibrationInfo    map[string]any `json:"calibration_info"`
	ComparativeInfo    map[string]any `json:"comparative_info"`
	CounterfactualInfo map[string]any `json:"counterfactual_info"`
	CreatedAt          time.Time      `json:"created_at"`
}

// FreezeRequest is the API request body for freeze/unfreeze operations.
type FreezeRequest struct {
	RequestedBy       string `json:"requested_by"`
	Reason            string `json:"reason"`
	FreezeLearning    *bool  `json:"freeze_learning,omitempty"`
	FreezePolicy      *bool  `json:"freeze_policy,omitempty"`
	FreezeExploration *bool  `json:"freeze_exploration,omitempty"`
}

// ForceModeRequest is the API request body for force-mode operations.
type ForceModeRequest struct {
	RequestedBy   string `json:"requested_by"`
	Reason        string `json:"reason"`
	ReasoningMode string `json:"reasoning_mode"` // graph/direct/conservative/exploratory
	ForceSafeMode *bool  `json:"force_safe_mode,omitempty"`
}

// SafeHoldRequest is the API request body for safe-hold operations.
type SafeHoldRequest struct {
	RequestedBy string `json:"requested_by"`
	Reason      string `json:"reason"`
}

// RollbackRequest is the API request body for rollback operations.
type RollbackRequest struct {
	RequestedBy string `json:"requested_by"`
	Reason      string `json:"reason"`
}

// UnfreezeRequest is the API request body for unfreeze operations.
type UnfreezeRequest struct {
	RequestedBy string `json:"requested_by"`
	Reason      string `json:"reason"`
}

// ClearOverrideRequest is the API request body for clearing all overrides.
type ClearOverrideRequest struct {
	RequestedBy string `json:"requested_by"`
	Reason      string `json:"reason"`
}
