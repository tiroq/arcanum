package goal_planning

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter provides a nil-safe, fail-open API adapter for the goal planning engine.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a new adapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// GetSubgoal returns a subgoal by ID. Returns zero value on error.
func (a *GraphAdapter) GetSubgoal(ctx context.Context, id string) Subgoal {
	if a == nil || a.engine == nil {
		return Subgoal{}
	}
	sg, err := a.engine.GetSubgoal(ctx, id)
	if err != nil {
		return Subgoal{}
	}
	return sg
}

// ListSubgoals returns subgoals for a goal. Returns nil on error.
func (a *GraphAdapter) ListSubgoals(ctx context.Context, goalID string) []Subgoal {
	if a == nil || a.engine == nil {
		return nil
	}
	sgs, err := a.engine.ListSubgoals(ctx, goalID)
	if err != nil {
		return nil
	}
	return sgs
}

// ListAllSubgoals returns all subgoals. Returns nil on error.
func (a *GraphAdapter) ListAllSubgoals(ctx context.Context) []Subgoal {
	if a == nil || a.engine == nil {
		return nil
	}
	sgs, err := a.engine.ListAllSubgoals(ctx)
	if err != nil {
		return nil
	}
	return sgs
}

// GetOverallProgress returns weighted progress for a goal. Returns 0 on error.
func (a *GraphAdapter) GetOverallProgress(ctx context.Context, goalID string) float64 {
	if a == nil || a.engine == nil {
		return 0
	}
	sgs, err := a.engine.ListSubgoals(ctx, goalID)
	if err != nil {
		return 0
	}
	return ComputeOverallProgress(sgs)
}
