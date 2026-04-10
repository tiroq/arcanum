package income

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter implements decision_graph.IncomeSignalProvider using the income
// engine. Fail-open: returns zero signal if engine is nil or DB unavailable.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter backed by the given Engine.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// GetIncomeSignal returns the income signal for graph scoring.
// Returns bestOpenScore ∈ [0,1] and openOpportunities count.
// Fail-open: returns (0, 0) if engine is nil or query fails.
func (a *GraphAdapter) GetIncomeSignal(ctx context.Context) (bestOpenScore float64, openOpportunities int) {
	if a == nil || a.engine == nil {
		return 0, 0
	}
	sig := a.engine.GetSignal(ctx)
	return sig.BestOpenScore, sig.OpenOpportunities
}

// IsIncomeRelated reports whether the given action type is income-oriented.
// Used by the planner adapter to decide whether to apply the income boost.
func (a *GraphAdapter) IsIncomeRelated(actionType string) bool {
	return IsIncomeAction(actionType)
}
