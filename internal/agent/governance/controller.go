package governance

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Controller manages governance state transitions and operator overrides.
// All mutations are audited, bounded, and explicit.
type Controller struct {
	stateStore  *StateStore
	actionStore *ActionStore
	auditor     audit.AuditRecorder
	logger      *zap.Logger
}

// NewController creates a Controller.
func NewController(
	stateStore *StateStore,
	actionStore *ActionStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Controller {
	return &Controller{
		stateStore:  stateStore,
		actionStore: actionStore,
		auditor:     auditor,
		logger:      logger,
	}
}

// GetState returns the current governance state.
// On read failure, returns a safe default state (fail-safe).
func (c *Controller) GetState(ctx context.Context) GovernanceState {
	st, err := c.stateStore.GetState(ctx)
	if err != nil {
		c.logger.Warn("governance_state_read_failed_using_safe_default", zap.Error(err))
		// Fail-safe: degrade to safer behavior.
		return GovernanceState{
			Mode:                ModeSafeHold,
			FreezeLearning:      true,
			FreezePolicyUpdates: true,
			FreezeExploration:   true,
			ForceSafeMode:       true,
			LastUpdated:         time.Now().UTC(),
			Reason:              "fail-safe: governance state unreadable",
		}
	}
	return st
}

// Freeze applies a freeze to the system. Transitions mode to frozen
// and optionally sets individual freeze flags.
func (c *Controller) Freeze(ctx context.Context, req FreezeRequest) (GovernanceState, error) {
	if req.RequestedBy == "" {
		return GovernanceState{}, fmt.Errorf("requested_by is required")
	}

	prev := c.GetState(ctx)
	now := time.Now().UTC()

	st := prev
	st.Mode = ModeFrozen
	st.LastUpdated = now
	st.Reason = req.Reason
	if req.FreezeLearning != nil {
		st.FreezeLearning = *req.FreezeLearning
	} else {
		st.FreezeLearning = true
	}
	if req.FreezePolicy != nil {
		st.FreezePolicyUpdates = *req.FreezePolicy
	} else {
		st.FreezePolicyUpdates = true
	}
	if req.FreezeExploration != nil {
		st.FreezeExploration = *req.FreezeExploration
	} else {
		st.FreezeExploration = true
	}

	if err := c.stateStore.SaveState(ctx, st); err != nil {
		return GovernanceState{}, fmt.Errorf("save frozen state: %w", err)
	}

	c.recordAction(ctx, ActionFreeze, req.RequestedBy, req.Reason, map[string]any{
		"previous_mode": prev.Mode,
		"new_mode":      st.Mode,
	})
	c.emitStateChanged(ctx, prev, st, req.RequestedBy)

	return st, nil
}

// Unfreeze removes all freeze flags and returns to normal mode.
func (c *Controller) Unfreeze(ctx context.Context, req UnfreezeRequest) (GovernanceState, error) {
	if req.RequestedBy == "" {
		return GovernanceState{}, fmt.Errorf("requested_by is required")
	}

	prev := c.GetState(ctx)
	now := time.Now().UTC()

	st := GovernanceState{
		Mode:        ModeNormal,
		LastUpdated: now,
		Reason:      req.Reason,
	}

	if err := c.stateStore.SaveState(ctx, st); err != nil {
		return GovernanceState{}, fmt.Errorf("save unfrozen state: %w", err)
	}

	c.recordAction(ctx, ActionUnfreeze, req.RequestedBy, req.Reason, map[string]any{
		"previous_mode": prev.Mode,
		"new_mode":      st.Mode,
	})
	c.emitStateChanged(ctx, prev, st, req.RequestedBy)

	return st, nil
}

// ForceMode sets a forced reasoning mode and/or safe mode.
func (c *Controller) ForceMode(ctx context.Context, req ForceModeRequest) (GovernanceState, error) {
	if req.RequestedBy == "" {
		return GovernanceState{}, fmt.Errorf("requested_by is required")
	}
	if req.ReasoningMode != "" && !validReasoningModes[req.ReasoningMode] {
		return GovernanceState{}, fmt.Errorf("invalid reasoning mode: %s", req.ReasoningMode)
	}

	prev := c.GetState(ctx)
	now := time.Now().UTC()

	st := prev
	st.ForceReasoningMode = req.ReasoningMode
	if req.ForceSafeMode != nil {
		st.ForceSafeMode = *req.ForceSafeMode
	}
	st.LastUpdated = now
	st.Reason = req.Reason

	if err := c.stateStore.SaveState(ctx, st); err != nil {
		return GovernanceState{}, fmt.Errorf("save forced mode state: %w", err)
	}

	c.recordAction(ctx, ActionForceMode, req.RequestedBy, req.Reason, map[string]any{
		"previous_mode":        prev.Mode,
		"force_reasoning_mode": st.ForceReasoningMode,
		"force_safe_mode":      st.ForceSafeMode,
	})
	c.emitOverrideApplied(ctx, prev, st, req.RequestedBy, "force_mode")

	return st, nil
}

// SafeHold puts the system in safe_hold mode.
func (c *Controller) SafeHold(ctx context.Context, req SafeHoldRequest) (GovernanceState, error) {
	if req.RequestedBy == "" {
		return GovernanceState{}, fmt.Errorf("requested_by is required")
	}

	prev := c.GetState(ctx)
	now := time.Now().UTC()

	st := prev
	st.Mode = ModeSafeHold
	st.ForceSafeMode = true
	st.FreezeExploration = true
	st.LastUpdated = now
	st.Reason = req.Reason

	if err := c.stateStore.SaveState(ctx, st); err != nil {
		return GovernanceState{}, fmt.Errorf("save safe_hold state: %w", err)
	}

	c.recordAction(ctx, ActionSafeHold, req.RequestedBy, req.Reason, map[string]any{
		"previous_mode": prev.Mode,
		"new_mode":      st.Mode,
	})
	c.emitStateChanged(ctx, prev, st, req.RequestedBy)

	return st, nil
}

// Rollback puts the system in rollback_only mode.
// All adaptive changes are blocked; only safe/read-only execution paths are allowed.
func (c *Controller) Rollback(ctx context.Context, req RollbackRequest) (GovernanceState, error) {
	if req.RequestedBy == "" {
		return GovernanceState{}, fmt.Errorf("requested_by is required")
	}

	prev := c.GetState(ctx)
	now := time.Now().UTC()

	st := GovernanceState{
		Mode:                ModeRollbackOnly,
		FreezeLearning:      true,
		FreezePolicyUpdates: true,
		FreezeExploration:   true,
		ForceSafeMode:       true,
		LastUpdated:         now,
		Reason:              req.Reason,
	}

	if err := c.stateStore.SaveState(ctx, st); err != nil {
		return GovernanceState{}, fmt.Errorf("save rollback state: %w", err)
	}

	c.recordAction(ctx, ActionRollback, req.RequestedBy, req.Reason, map[string]any{
		"previous_mode": prev.Mode,
		"new_mode":      st.Mode,
	})
	c.emitRollbackApplied(ctx, prev, st, req.RequestedBy)

	return st, nil
}

// ClearOverride returns the system to normal mode, clearing all overrides.
func (c *Controller) ClearOverride(ctx context.Context, req ClearOverrideRequest) (GovernanceState, error) {
	if req.RequestedBy == "" {
		return GovernanceState{}, fmt.Errorf("requested_by is required")
	}

	prev := c.GetState(ctx)
	now := time.Now().UTC()

	st := GovernanceState{
		Mode:        ModeNormal,
		LastUpdated: now,
		Reason:      req.Reason,
	}

	if err := c.stateStore.SaveState(ctx, st); err != nil {
		return GovernanceState{}, fmt.Errorf("save cleared state: %w", err)
	}

	c.recordAction(ctx, ActionClearOverride, req.RequestedBy, req.Reason, map[string]any{
		"previous_mode": prev.Mode,
		"new_mode":      st.Mode,
	})
	c.emitStateChanged(ctx, prev, st, req.RequestedBy)

	return st, nil
}

// ListActions returns governance action history.
func (c *Controller) ListActions(ctx context.Context, limit, offset int) ([]GovernanceAction, error) {
	return c.actionStore.ListActions(ctx, limit, offset)
}

// --- Internal helpers ---

func (c *Controller) recordAction(ctx context.Context, actionType, requestedBy, reason string, payload map[string]any) {
	action := GovernanceAction{
		ActionType:  actionType,
		RequestedBy: requestedBy,
		Reason:      reason,
		Payload:     payload,
		CreatedAt:   time.Now().UTC(),
	}
	if err := c.actionStore.RecordAction(ctx, action); err != nil {
		c.logger.Warn("governance_action_record_failed",
			zap.String("action_type", actionType),
			zap.Error(err),
		)
	}
}

func (c *Controller) emitStateChanged(ctx context.Context, prev, next GovernanceState, requestedBy string) {
	c.audit(ctx, "governance.state_changed", map[string]any{
		"requested_by":   requestedBy,
		"previous_state": stateSnapshot(prev),
		"new_state":      stateSnapshot(next),
		"reason":         next.Reason,
	})
}

func (c *Controller) emitOverrideApplied(ctx context.Context, prev, next GovernanceState, requestedBy, overrideType string) {
	c.audit(ctx, "governance.override_applied", map[string]any{
		"requested_by":   requestedBy,
		"override_type":  overrideType,
		"previous_state": stateSnapshot(prev),
		"new_state":      stateSnapshot(next),
		"reason":         next.Reason,
	})
}

func (c *Controller) emitRollbackApplied(ctx context.Context, prev, next GovernanceState, requestedBy string) {
	c.audit(ctx, "governance.rollback_applied", map[string]any{
		"requested_by":   requestedBy,
		"previous_state": stateSnapshot(prev),
		"new_state":      stateSnapshot(next),
		"reason":         next.Reason,
	})
}

func (c *Controller) audit(ctx context.Context, eventType string, payload map[string]any) {
	if c.auditor == nil {
		return
	}
	_ = c.auditor.RecordEvent(ctx, "governance", uuid.New(), eventType,
		"operator", "governance_controller", payload)
}

func stateSnapshot(st GovernanceState) map[string]any {
	return map[string]any{
		"mode":                 st.Mode,
		"freeze_learning":      st.FreezeLearning,
		"freeze_policy":        st.FreezePolicyUpdates,
		"freeze_exploration":   st.FreezeExploration,
		"force_reasoning_mode": st.ForceReasoningMode,
		"force_safe_mode":      st.ForceSafeMode,
		"require_human_review": st.RequireHumanReview,
		"reason":               st.Reason,
	}
}
