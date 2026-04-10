package signals

import (
	"context"

	"go.uber.org/zap"

	decision_graph "github.com/tiroq/arcanum/internal/agent/decision_graph"
)

// GraphAdapter implements decision_graph.SignalIngestionProvider.
// Fail-open: returns empty ActiveSignals if engine is nil or query fails.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// GetActiveSignals returns currently active signals and derived state.
// Fail-open: returns empty slices/maps if unavailable.
func (a *GraphAdapter) GetActiveSignals(ctx context.Context) ([]decision_graph.SignalIngestionExport, map[string]float64) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	active := a.engine.GetActiveSignals(ctx)
	exports := make([]decision_graph.SignalIngestionExport, len(active.Signals))
	for i, s := range active.Signals {
		exports[i] = decision_graph.SignalIngestionExport{
			SignalType:  s.SignalType,
			Severity:    s.Severity,
			Confidence:  s.Confidence,
			Value:       s.Value,
			Source:      s.Source,
			ContextTags: s.ContextTags,
		}
	}
	return exports, active.Derived
}

// CountSignalsForGoal returns how many active signals match the given goal type.
// Fail-open: returns 0 if unavailable.
func (a *GraphAdapter) CountSignalsForGoal(ctx context.Context, goalType string) int {
	if a == nil || a.engine == nil {
		return 0
	}
	active := a.engine.GetActiveSignals(ctx)
	return CountMatchingSignals(active.Signals, goalType)
}
