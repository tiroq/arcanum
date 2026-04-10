package financialtruth

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter implements the decision_graph.FinancialTruthProvider interface.
// Provides verified financial truth signals for upstream consumers
// (financial pressure, income engine).
// Nil-safe and fail-open: returns zero values when engine is not available.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a new financial truth graph adapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// GetTruthSignal returns the verified financial truth signal.
// Fail-open: returns empty signal if engine is nil or data unavailable.
func (a *GraphAdapter) GetTruthSignal(ctx context.Context) FinancialTruthSignal {
	if a == nil || a.engine == nil {
		return FinancialTruthSignal{}
	}
	return a.engine.GetTruthSignal(ctx)
}

// GetVerifiedIncomeForOpportunity returns the verified income amount for a
// specific opportunity. Fail-open: returns 0 if unavailable.
func (a *GraphAdapter) GetVerifiedIncomeForOpportunity(ctx context.Context, oppID string) float64 {
	if a == nil || a.engine == nil {
		return 0
	}
	return a.engine.GetVerifiedValueForOpportunity(ctx, oppID)
}

// GetSummary returns the current month's financial truth summary.
func (a *GraphAdapter) GetSummary(ctx context.Context) FinancialSummary {
	if a == nil || a.engine == nil {
		return FinancialSummary{}
	}
	return a.engine.GetSummary(ctx)
}
