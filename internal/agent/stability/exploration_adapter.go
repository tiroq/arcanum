package stability

import (
	"context"

	"github.com/tiroq/arcanum/internal/agent/exploration"
)

// ExplorationStabilityAdapter implements exploration.StabilityProvider
// so the exploration engine can check the current stability mode
// without importing this package directly.
type ExplorationStabilityAdapter struct {
	engine *Engine
}

// NewExplorationStabilityAdapter creates an adapter for the exploration engine.
func NewExplorationStabilityAdapter(engine *Engine) *ExplorationStabilityAdapter {
	return &ExplorationStabilityAdapter{engine: engine}
}

// GetMode returns the current stability mode for exploration decisions.
func (a *ExplorationStabilityAdapter) GetMode(ctx context.Context) exploration.StabilityMode {
	st, err := a.engine.GetState(ctx)
	if err != nil {
		return exploration.StabilityNormal // fail-open: allow exploration on error
	}
	return exploration.StabilityMode(st.Mode)
}
