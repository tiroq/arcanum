package externalactions

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter exposes external action data for the API layer.
// Nil-safe and fail-open: returns empty values if engine is not available.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a new external actions graph adapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// CreateAction creates a new external action.
func (a *GraphAdapter) CreateAction(ctx context.Context, action ExternalAction) (ExternalAction, error) {
	if a == nil || a.engine == nil {
		return ExternalAction{}, nil
	}
	return a.engine.CreateAction(ctx, action)
}

// ListActions returns recent actions.
func (a *GraphAdapter) ListActions(ctx context.Context, limit int) ([]ExternalAction, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListActions(ctx, limit)
}

// GetAction retrieves an action by ID.
func (a *GraphAdapter) GetAction(ctx context.Context, id string) (ExternalAction, error) {
	if a == nil || a.engine == nil {
		return ExternalAction{}, nil
	}
	return a.engine.GetAction(ctx, id)
}

// Execute runs the action through its connector.
func (a *GraphAdapter) Execute(ctx context.Context, actionID string) (ExecutionResult, error) {
	if a == nil || a.engine == nil {
		return ExecutionResult{}, nil
	}
	return a.engine.Execute(ctx, actionID)
}

// DryRun executes a dry-run of the action.
func (a *GraphAdapter) DryRun(ctx context.Context, actionID string) (ExecutionResult, error) {
	if a == nil || a.engine == nil {
		return ExecutionResult{}, nil
	}
	return a.engine.DryRun(ctx, actionID)
}

// ApproveAction marks a review_required action as ready for execution.
func (a *GraphAdapter) ApproveAction(ctx context.Context, actionID, approvedBy string) (ExternalAction, error) {
	if a == nil || a.engine == nil {
		return ExternalAction{}, nil
	}
	return a.engine.ApproveAction(ctx, actionID, approvedBy)
}

// GetResults returns execution results for an action.
func (a *GraphAdapter) GetResults(ctx context.Context, actionID string) ([]ExecutionResult, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.GetResults(ctx, actionID)
}
