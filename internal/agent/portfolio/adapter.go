package portfolio

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter implements the decision_graph.PortfolioProvider interface.
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

// GetStrategyBoost returns a bounded score adjustment for the given opportunity type
// based on its strategy's portfolio performance. Range: [-StrategyPenaltyMax, +StrategyPriorityBoostMax].
// Fail-open: returns 0 when engine is nil or data is unavailable.
func (a *GraphAdapter) GetStrategyBoost(ctx context.Context, opportunityType string) float64 {
	if a == nil || a.engine == nil {
		return 0
	}

	strategyType := MapOpportunityToStrategy(opportunityType)

	strategies, perfMap, err := a.engine.GetStrategiesAndPerformance(ctx)
	if err != nil {
		a.logger.Warn("portfolio: failed to load strategies for boost",
			zap.Error(err),
		)
		return 0
	}

	return ComputeStrategyBoost(strategyType, strategies, perfMap)
}

// IsStrategyRelated reports whether the given action type is associated with
// a strategy (i.e. is an income-oriented action).
func (a *GraphAdapter) IsStrategyRelated(actionType string) bool {
	if a == nil {
		return false
	}
	// All income actions participate in strategy-level scoring.
	switch actionType {
	case "propose_income_action", "analyze_opportunity", "schedule_work":
		return true
	}
	return false
}

// GetPortfolio returns the current portfolio view.
// Fail-open: returns empty Portfolio when engine is nil.
func (a *GraphAdapter) GetPortfolio(ctx context.Context) Portfolio {
	if a == nil || a.engine == nil {
		return Portfolio{}
	}
	p, err := a.engine.GetPortfolio(ctx)
	if err != nil {
		a.logger.Warn("portfolio: failed to get portfolio", zap.Error(err))
		return Portfolio{}
	}
	return p
}

// GetStrategies returns all strategies (for API).
func (a *GraphAdapter) GetStrategies(ctx context.Context) ([]Strategy, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListStrategies(ctx)
}

// GetPerformance returns all performance records (for API).
func (a *GraphAdapter) GetPerformance(ctx context.Context) ([]StrategyPerformance, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.GetPerformance(ctx)
}

// CreateStrategy creates a new strategy via the engine (for API).
func (a *GraphAdapter) CreateStrategy(ctx context.Context, st Strategy) (Strategy, error) {
	if a == nil || a.engine == nil {
		return Strategy{}, nil
	}
	return a.engine.CreateStrategy(ctx, st)
}

// Rebalance triggers portfolio rebalancing (for API).
func (a *GraphAdapter) Rebalance(ctx context.Context) (RebalanceResult, error) {
	if a == nil || a.engine == nil {
		return RebalanceResult{Reason: "portfolio engine not available"}, nil
	}
	return a.engine.Rebalance(ctx)
}

// GetCapacityAvailableHoursWeek exposes weekly capacity for use by other modules.
// Fail-open: returns 0 when engine or capacity provider is nil.
func (a *GraphAdapter) GetCapacityAvailableHoursWeek(ctx context.Context) float64 {
	if a == nil || a.engine == nil || a.engine.capacity == nil {
		return 0
	}
	return a.engine.capacity.GetAvailableHoursWeek(ctx)
}
