package income

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter implements decision_graph.IncomeSignalProvider and
// decision_graph.OutcomeAttributionProvider using the income engine.
// Fail-open: returns zero signal if engine is nil or DB unavailable.
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

// GetOutcomeFeedback returns a bounded score adjustment for an income-related action type
// based on real outcome learning data (Iteration 39).
//
// Returns a value in [-OutcomeFeedbackMaxPenalty, +OutcomeFeedbackMaxBoost].
// Fail-open: returns 0 if engine is nil, learning store is nil, or insufficient data.
func (a *GraphAdapter) GetOutcomeFeedback(ctx context.Context, actionType string) float64 {
	if a == nil || a.engine == nil {
		return 0
	}
	// Map action type back to the opportunity types it serves.
	// Use the best learning record among matching types.
	var bestFeedback float64
	var found bool
	for oppType := range validOpportunityTypes {
		actions := MapOpportunityToActions(oppType)
		for _, at := range actions {
			if at == actionType {
				lr := a.engine.GetLearningForType(ctx, oppType)
				fb := ComputeOutcomeFeedback(lr)
				if !found || fb > bestFeedback {
					bestFeedback = fb
					found = true
				}
				break
			}
		}
	}
	return bestFeedback
}
