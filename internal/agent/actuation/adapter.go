package actuation

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter wraps the actuation Engine to provide a nil-safe, fail-open
// API surface for handlers and other subsystems.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a new GraphAdapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{
		engine: engine,
		logger: logger,
	}
}

// Run triggers a full actuation pipeline run.
// Fail-open: nil engine → empty result, no error.
func (a *GraphAdapter) Run(ctx context.Context) (ActuationRunResult, error) {
	if a == nil || a.engine == nil {
		return ActuationRunResult{}, nil
	}
	return a.engine.Run(ctx)
}

// ListDecisions returns recent actuation decisions.
// Fail-open: nil engine → empty slice, no error.
func (a *GraphAdapter) ListDecisions(ctx context.Context, limit int) ([]ActuationDecision, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListDecisions(ctx, limit)
}

// ApproveDecision transitions a decision from proposed → approved.
func (a *GraphAdapter) ApproveDecision(ctx context.Context, id string) (ActuationDecision, error) {
	if a == nil || a.engine == nil {
		return ActuationDecision{}, nil
	}
	return a.engine.ApproveDecision(ctx, id)
}

// RejectDecision transitions a decision from proposed → rejected.
func (a *GraphAdapter) RejectDecision(ctx context.Context, id string) (ActuationDecision, error) {
	if a == nil || a.engine == nil {
		return ActuationDecision{}, nil
	}
	return a.engine.RejectDecision(ctx, id)
}

// ExecuteDecision transitions a decision from approved → executed.
func (a *GraphAdapter) ExecuteDecision(ctx context.Context, id string) (ActuationDecision, error) {
	if a == nil || a.engine == nil {
		return ActuationDecision{}, nil
	}
	return a.engine.ExecuteDecision(ctx, id)
}
