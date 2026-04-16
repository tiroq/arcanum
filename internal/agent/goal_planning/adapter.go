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

// ListPlans returns all goal plans. Returns nil on error.
func (a *GraphAdapter) ListPlans(ctx context.Context) []GoalPlan {
	if a == nil || a.engine == nil {
		return nil
	}
	plans, err := a.engine.ListPlans(ctx)
	if err != nil {
		return nil
	}
	return plans
}

// CreatePlan creates a new goal plan. Returns zero plan on error.
func (a *GraphAdapter) CreatePlan(ctx context.Context, goalID string, horizon Horizon, strategy Strategy) GoalPlan {
	if a == nil || a.engine == nil {
		return GoalPlan{}
	}
	plan, err := a.engine.CreatePlan(ctx, goalID, horizon, strategy)
	if err != nil {
		return GoalPlan{}
	}
	return plan
}

// Replan triggers adaptive replanning. Returns count of subgoals replanned.
func (a *GraphAdapter) Replan(ctx context.Context, goalID string) int {
	if a == nil || a.engine == nil {
		return 0
	}
	count, err := a.engine.Replan(ctx, goalID)
	if err != nil {
		return 0
	}
	return count
}

// RunReplanCycle runs the full replanning cycle. Returns count of subgoals replanned.
func (a *GraphAdapter) RunReplanCycle(ctx context.Context) int {
	if a == nil || a.engine == nil {
		return 0
	}
	count, err := a.engine.RunReplanCycle(ctx)
	if err != nil {
		return 0
	}
	return count
}
