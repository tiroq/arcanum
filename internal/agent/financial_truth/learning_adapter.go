package financialtruth

import "context"

// LearningTruthAdapter provides verified financial values for income engine
// learning. Implements a simple interface that the income engine can consume
// without import cycles.
// Nil-safe and fail-open: returns 0 when unavailable.
type LearningTruthAdapter struct {
	engine *Engine
}

// NewLearningTruthAdapter creates a LearningTruthAdapter backed by the engine.
func NewLearningTruthAdapter(engine *Engine) *LearningTruthAdapter {
	return &LearningTruthAdapter{engine: engine}
}

// GetVerifiedValueForOpportunity returns verified income linked to an opportunity.
// Returns 0 if no verified facts exist.
func (a *LearningTruthAdapter) GetVerifiedValueForOpportunity(ctx context.Context, oppID string) float64 {
	if a == nil || a.engine == nil {
		return 0
	}
	return a.engine.GetVerifiedValueForOpportunity(ctx, oppID)
}
