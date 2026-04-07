package stability

import (
	"context"

	"github.com/tiroq/arcanum/internal/agent/strategy"
)

// StrategyStabilityAdapter implements strategy.StabilityProvider
// so the strategy engine can check the current stability mode
// and blocked actions without importing the stability package directly.
type StrategyStabilityAdapter struct {
	engine *Engine
}

// NewStrategyStabilityAdapter creates an adapter for the strategy engine.
func NewStrategyStabilityAdapter(engine *Engine) *StrategyStabilityAdapter {
	return &StrategyStabilityAdapter{engine: engine}
}

// Ensure compile-time interface compliance.
var _ strategy.StabilityProvider = (*StrategyStabilityAdapter)(nil)

// GetMode returns the current stability mode as a string.
func (a *StrategyStabilityAdapter) GetMode(ctx context.Context) string {
	st, err := a.engine.GetState(ctx)
	if err != nil {
		return "normal" // fail-open
	}
	return string(st.Mode)
}

// GetBlockedActions returns the currently blocked action types.
func (a *StrategyStabilityAdapter) GetBlockedActions(ctx context.Context) []string {
	st, err := a.engine.GetState(ctx)
	if err != nil {
		return nil // fail-open
	}
	return st.BlockedActionTypes
}
