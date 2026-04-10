package capacity

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter implements the decision_graph.CapacityProvider interface.
// Provides capacity-aware penalty/boost for the scoring pipeline.
// Nil-safe and fail-open: returns 0 if engine is not available.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a new capacity graph adapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// GetCapacityPenalty returns a bounded penalty [0, CapacityPenaltyMax] based on
// current capacity constraints. Applied to all scored paths in the pipeline.
// Fail-open: returns 0 if capacity data is missing.
func (a *GraphAdapter) GetCapacityPenalty(ctx context.Context) float64 {
	if a == nil || a.engine == nil {
		return 0
	}
	state, err := a.engine.GetState(ctx)
	if err != nil {
		return 0
	}
	if state.MaxDailyWorkHours == 0 {
		return 0 // no capacity data
	}
	return ComputeCapacityPenalty(state.AvailableHoursToday, state.MaxDailyWorkHours, state.OwnerLoadScore)
}

// GetCapacityBoost returns a bounded boost [0, CapacityBoostMax] for
// small high-value actions when capacity is constrained.
// actionEffortHours is the estimated effort of the first action (default 1.0 if unknown).
// actionValuePerHour is the value per hour of the action (default 0 if unknown).
// Fail-open: returns 0 if capacity data or action metadata is missing.
func (a *GraphAdapter) GetCapacityBoost(ctx context.Context, actionEffortHours, actionValuePerHour float64) float64 {
	if a == nil || a.engine == nil {
		return 0
	}
	state, err := a.engine.GetState(ctx)
	if err != nil {
		return 0
	}
	if state.MaxDailyWorkHours == 0 {
		return 0
	}
	// Compute a quick fit score for boost eligibility.
	fitScore := ComputeCapacityFitScore(
		actionValuePerHour, 0.5, actionEffortHours,
		state.AvailableHoursToday, state.OwnerLoadScore,
	)
	return ComputeCapacityBoost(fitScore, actionEffortHours, actionValuePerHour)
}

// GetCapacityState returns the current capacity state for API visibility.
func (a *GraphAdapter) GetCapacityState(ctx context.Context) (CapacityState, error) {
	if a == nil || a.engine == nil {
		return CapacityState{}, nil
	}
	return a.engine.GetState(ctx)
}

// RecomputeState triggers a capacity state recomputation.
func (a *GraphAdapter) RecomputeState(ctx context.Context) (CapacityState, error) {
	if a == nil || a.engine == nil {
		return CapacityState{}, nil
	}
	return a.engine.RecomputeState(ctx)
}

// EvaluateItems evaluates items against current capacity.
func (a *GraphAdapter) EvaluateItems(ctx context.Context, items []CapacityItem) ([]CapacityDecision, CapacitySummary, error) {
	if a == nil || a.engine == nil {
		return nil, CapacitySummary{}, nil
	}
	return a.engine.EvaluateItems(ctx, items)
}

// GetRecommendations returns recent capacity decisions.
func (a *GraphAdapter) GetRecommendations(ctx context.Context, limit int) ([]CapacityDecision, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.GetRecommendations(ctx, limit)
}
