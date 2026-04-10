package externalactions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates the external action lifecycle:
// create → policy check → review (if needed) → dry-run → execute → result capture.
type Engine struct {
	store   *Store
	router  *ConnectorRouter
	policy  *PolicyEngine
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewEngine creates a new external actions engine.
func NewEngine(
	store *Store,
	router *ConnectorRouter,
	policy *PolicyEngine,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		store:   store,
		router:  router,
		policy:  policy,
		auditor: auditor,
		logger:  logger,
	}
}

// CreateAction creates a new external action.
// Applies policy check and routes to a connector.
// If the action requires review, it is set to review_required status.
func (e *Engine) CreateAction(ctx context.Context, a ExternalAction) (ExternalAction, error) {
	if !IsValidActionType(a.ActionType) {
		return ExternalAction{}, fmt.Errorf("invalid action type: %s", a.ActionType)
	}
	if err := ValidatePayload(a.Payload); err != nil {
		return ExternalAction{}, err
	}

	a.ID = uuid.New().String()
	if a.IdempotencyKey == "" {
		a.IdempotencyKey = uuid.New().String()
	}
	if a.MaxRetries == 0 {
		a.MaxRetries = MaxRetries
	}

	// Select connector.
	var connectorName string
	if a.ConnectorName != "" {
		// Explicit connector requested.
		c, ok := e.router.RouteByName(a.ConnectorName)
		if !ok {
			return ExternalAction{}, ErrConnectorNotFound
		}
		if !c.Supports(a.ActionType) {
			return ExternalAction{}, fmt.Errorf("connector %s does not support action type %s", a.ConnectorName, a.ActionType)
		}
		connectorName = a.ConnectorName
	} else {
		// Auto-route.
		c, ok := e.router.Route(a.ActionType)
		if !ok {
			return ExternalAction{}, ErrConnectorNotFound
		}
		connectorName = c.Name()
	}
	a.ConnectorName = connectorName

	// Evaluate policy.
	pol := e.policy.Evaluate(a)
	a.RiskLevel = pol.RiskLevel

	if pol.RequiresReview {
		a.Status = StatusReviewRequired
		a.ReviewReason = pol.Reason
	} else {
		a.Status = StatusReady
	}

	saved, err := e.store.CreateAction(ctx, a)
	if err != nil {
		return ExternalAction{}, fmt.Errorf("store action: %w", err)
	}

	e.emitAudit(ctx, "external.action_created", saved.ID, map[string]interface{}{
		"action_type":    saved.ActionType,
		"connector":      saved.ConnectorName,
		"status":         saved.Status,
		"risk_level":     saved.RiskLevel,
		"review_reason":  saved.ReviewReason,
		"opportunity_id": saved.OpportunityID,
	})

	return saved, nil
}

// ApproveAction marks a review_required action as ready for execution.
func (e *Engine) ApproveAction(ctx context.Context, actionID, approvedBy string) (ExternalAction, error) {
	action, err := e.store.GetAction(ctx, actionID)
	if err != nil {
		return ExternalAction{}, ErrActionNotFound
	}

	if !IsValidTransition(action.Status, StatusReady) {
		return ExternalAction{}, fmt.Errorf("%w: cannot approve from status %s", ErrInvalidTransition, action.Status)
	}

	if err := e.store.UpdateActionStatus(ctx, actionID, StatusReady); err != nil {
		return ExternalAction{}, fmt.Errorf("update status: %w", err)
	}
	action.Status = StatusReady

	e.emitAudit(ctx, "external.action_approved", actionID, map[string]interface{}{
		"approved_by": approvedBy,
		"action_type": action.ActionType,
	})

	return action, nil
}

// DryRun executes a dry-run of the action through its connector.
func (e *Engine) DryRun(ctx context.Context, actionID string) (ExecutionResult, error) {
	action, err := e.store.GetAction(ctx, actionID)
	if err != nil {
		return ExecutionResult{}, ErrActionNotFound
	}

	c, ok := e.router.RouteByName(action.ConnectorName)
	if !ok {
		return ExecutionResult{}, ErrConnectorNotFound
	}
	if !c.Enabled() {
		return ExecutionResult{}, ErrConnectorDisabled
	}

	start := time.Now()
	result, err := c.DryRun(action.Payload)
	result.DurationMs = time.Since(start).Milliseconds()
	result.ID = uuid.New().String()
	result.ActionID = actionID
	result.Mode = ModeDryRun

	if err != nil {
		e.emitAudit(ctx, "external.action_failed", actionID, map[string]interface{}{
			"mode":  ModeDryRun,
			"error": err.Error(),
		})
		return result, err
	}

	// Persist result.
	saved, saveErr := e.store.CreateResult(ctx, result)
	if saveErr != nil {
		e.logger.Warn("failed to persist dry-run result", zap.Error(saveErr))
	} else {
		result = saved
	}

	// Mark dry-run completed on the action.
	if markErr := e.store.UpdateActionDryRun(ctx, actionID); markErr != nil {
		e.logger.Warn("failed to mark dry-run completed", zap.Error(markErr))
	}

	e.emitAudit(ctx, "external.action_dry_run", actionID, map[string]interface{}{
		"success":   result.Success,
		"connector": action.ConnectorName,
	})

	return result, nil
}

// Execute runs the action through its connector for real.
// Fail-safe: will NOT execute actions that require review and haven't been approved.
func (e *Engine) Execute(ctx context.Context, actionID string) (ExecutionResult, error) {
	action, err := e.store.GetAction(ctx, actionID)
	if err != nil {
		return ExecutionResult{}, ErrActionNotFound
	}

	// Fail-safe: block execution of unapproved actions.
	if action.Status == StatusReviewRequired {
		return ExecutionResult{}, ErrReviewRequired
	}
	if action.Status == StatusExecuted {
		return ExecutionResult{}, ErrAlreadyExecuted
	}
	if action.Status != StatusReady {
		return ExecutionResult{}, fmt.Errorf("%w: cannot execute from status %s", ErrInvalidTransition, action.Status)
	}

	c, ok := e.router.RouteByName(action.ConnectorName)
	if !ok {
		return ExecutionResult{}, ErrConnectorNotFound
	}
	if !c.Enabled() {
		return ExecutionResult{}, ErrConnectorDisabled
	}

	// Check retry limit.
	if action.RetryCount >= action.MaxRetries {
		if updateErr := e.store.UpdateActionStatus(ctx, actionID, StatusFailed); updateErr != nil {
			e.logger.Warn("failed to update action status to failed", zap.Error(updateErr))
		}
		return ExecutionResult{}, ErrMaxRetriesExceeded
	}

	start := time.Now()
	result, execErr := c.Execute(action.Payload)
	result.DurationMs = time.Since(start).Milliseconds()
	result.ID = uuid.New().String()
	result.ActionID = actionID
	result.Mode = ModeExecute

	if execErr != nil {
		result.Success = false
		result.ErrorMessage = execErr.Error()

		// Increment retry count.
		if retryErr := e.store.IncrementRetryCount(ctx, actionID); retryErr != nil {
			e.logger.Warn("failed to increment retry count", zap.Error(retryErr))
		}

		// Persist failed result.
		if _, saveErr := e.store.CreateResult(ctx, result); saveErr != nil {
			e.logger.Warn("failed to persist execution result", zap.Error(saveErr))
		}

		e.emitAudit(ctx, "external.action_failed", actionID, map[string]interface{}{
			"mode":        ModeExecute,
			"error":       execErr.Error(),
			"retry_count": action.RetryCount + 1,
		})

		return result, execErr
	}

	// Success — update action status.
	newStatus := StatusExecuted
	if !result.Success {
		newStatus = StatusFailed
	}
	if updateErr := e.store.UpdateActionStatus(ctx, actionID, newStatus); updateErr != nil {
		e.logger.Warn("failed to update action status", zap.Error(updateErr))
	}

	// Persist result.
	saved, saveErr := e.store.CreateResult(ctx, result)
	if saveErr != nil {
		e.logger.Warn("failed to persist execution result", zap.Error(saveErr))
	} else {
		result = saved
	}

	e.emitAudit(ctx, "external.action_executed", actionID, map[string]interface{}{
		"success":     result.Success,
		"connector":   action.ConnectorName,
		"external_id": result.ExternalID,
		"duration_ms": result.DurationMs,
	})

	return result, nil
}

// ListActions returns recent actions.
func (e *Engine) ListActions(ctx context.Context, limit int) ([]ExternalAction, error) {
	return e.store.ListActions(ctx, limit)
}

// GetAction returns a single action by ID.
func (e *Engine) GetAction(ctx context.Context, id string) (ExternalAction, error) {
	return e.store.GetAction(ctx, id)
}

// GetResults returns execution results for an action.
func (e *Engine) GetResults(ctx context.Context, actionID string) ([]ExecutionResult, error) {
	return e.store.ListResultsByAction(ctx, actionID)
}

// --- Internal Helpers ---

func (e *Engine) emitAudit(ctx context.Context, eventType, actionID string, payload map[string]interface{}) {
	if e.auditor == nil {
		return
	}
	if err := e.auditor.RecordEvent(ctx, "external_action", uuid.Nil, eventType, "system", actionID, payload); err != nil {
		e.logger.Warn("failed to emit audit event",
			zap.String("event_type", eventType),
			zap.Error(err),
		)
	}
}
