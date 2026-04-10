package reflection

import (
	"context"

	"go.uber.org/zap"
)

// ReflectionProvider is the interface for the decision graph to consume reflection signals.
type ReflectionProvider interface {
	GetReflectionSignals(ctx context.Context) ([]ReflectionSignal, error)
}

// MetaGraphAdapter wraps MetaEngine to provide reflection signals to the decision graph.
// All methods are nil-safe and fail-open.
type MetaGraphAdapter struct {
	engine *MetaEngine
	logger *zap.Logger
}

// NewMetaGraphAdapter creates a MetaGraphAdapter.
func NewMetaGraphAdapter(engine *MetaEngine, logger *zap.Logger) *MetaGraphAdapter {
	return &MetaGraphAdapter{engine: engine, logger: logger}
}

// GetReflectionSignals returns the latest reflection signals.
// Fail-open: nil engine or empty signals → no error, empty slice.
func (a *MetaGraphAdapter) GetReflectionSignals(ctx context.Context) ([]ReflectionSignal, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	signals := a.engine.GetLatestSignals()
	return signals, nil
}

// GetReflectionBoost computes a bounded scoring boost from active reflection signals.
// The boost is capped at ReflectionSignalBoostMax (0.10).
// Only signals matching the given context tags contribute.
func (a *MetaGraphAdapter) GetReflectionBoost(ctx context.Context, contextTags []string) float64 {
	if a == nil || a.engine == nil {
		return 0
	}

	signals := a.engine.GetLatestSignals()
	if len(signals) == 0 {
		return 0
	}

	tagSet := make(map[string]bool, len(contextTags))
	for _, t := range contextTags {
		tagSet[t] = true
	}

	var totalBoost float64
	for _, sig := range signals {
		if matchesTags(sig.ContextTags, tagSet) {
			boost := sig.Strength * ReflectionSignalBoostScale
			totalBoost += boost
		}
	}

	if totalBoost > ReflectionSignalBoostMax {
		totalBoost = ReflectionSignalBoostMax
	}
	return totalBoost
}

// RunReflection triggers a meta-reflection run via the adapter.
// Fail-open: nil engine → nil report, no error.
func (a *MetaGraphAdapter) RunReflection(ctx context.Context, force bool) (*MetaReflectionReport, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.RunReflection(ctx, force)
}

// matchesTags returns true if any signal tag matches any of the target tags.
func matchesTags(signalTags []string, targetTags map[string]bool) bool {
	if len(targetTags) == 0 {
		return true // no filter = match all
	}
	for _, t := range signalTags {
		if targetTags[t] {
			return true
		}
	}
	return false
}
