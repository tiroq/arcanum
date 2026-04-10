package objective

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter implements the decision_graph.ObjectiveFunctionProvider interface.
// Nil-safe and fail-open: returns zero values when engine is nil or errors occur.
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

// GetObjectiveSignal returns the current planner-facing objective signal.
// Fail-open: returns zero signal when engine is nil or data is unavailable.
func (a *GraphAdapter) GetObjectiveSignal(ctx context.Context) ObjectiveSignal {
	if a == nil || a.engine == nil {
		return ObjectiveSignal{}
	}
	sig, err := a.engine.GetObjectiveSignal(ctx)
	if err != nil {
		a.logger.Warn("objective: failed to get signal", zap.Error(err))
		return ObjectiveSignal{}
	}
	return sig
}

// GetSummary returns current objective summary (for API delegation).
func (a *GraphAdapter) GetSummary(ctx context.Context) (ObjectiveSummary, error) {
	if a == nil || a.engine == nil {
		return ObjectiveSummary{}, nil
	}
	return a.engine.GetSummary(ctx)
}

// GetObjectiveState returns current objective state (for API delegation).
func (a *GraphAdapter) GetObjectiveState(ctx context.Context) (ObjectiveState, error) {
	if a == nil || a.engine == nil {
		return ObjectiveState{}, nil
	}
	return a.engine.GetObjectiveState(ctx)
}

// GetRiskState returns current risk state (for API delegation).
func (a *GraphAdapter) GetRiskState(ctx context.Context) (RiskState, error) {
	if a == nil || a.engine == nil {
		return RiskState{}, nil
	}
	return a.engine.GetRiskState(ctx)
}

// Recompute triggers a full recomputation of objective and risk (for API delegation).
func (a *GraphAdapter) Recompute(ctx context.Context) (ObjectiveSummary, error) {
	if a == nil || a.engine == nil {
		return ObjectiveSummary{}, nil
	}
	return a.engine.Recompute(ctx)
}
