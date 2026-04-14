package taskorchestrator

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter exposes the task orchestrator engine to the API and other subsystems.
// All methods are nil-safe and fail-open.
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

// CreateTask creates a new orchestrated task.
func (a *GraphAdapter) CreateTask(ctx context.Context, source, goal string, urgency, expectedValue, riskLevel float64, strategyType string) (OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return OrchestratedTask{}, nil
	}
	return a.engine.CreateTask(ctx, source, goal, urgency, expectedValue, riskLevel, strategyType)
}

// GetTask retrieves a task by ID.
func (a *GraphAdapter) GetTask(ctx context.Context, id string) (OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return OrchestratedTask{}, nil
	}
	return a.engine.GetTask(ctx, id)
}

// ListTasks returns tasks with a limit.
func (a *GraphAdapter) ListTasks(ctx context.Context, limit int) ([]OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListTasks(ctx, limit)
}

// RecomputePriorities re-scores all scoreable tasks.
func (a *GraphAdapter) RecomputePriorities(ctx context.Context) error {
	if a == nil || a.engine == nil {
		return nil
	}
	return a.engine.RecomputePriorities(ctx)
}

// Dispatch runs a single orchestration cycle.
func (a *GraphAdapter) Dispatch(ctx context.Context) (DispatchResult, error) {
	if a == nil || a.engine == nil {
		return DispatchResult{}, nil
	}
	return a.engine.Dispatch(ctx)
}

// GetQueue returns the current queue state.
func (a *GraphAdapter) GetQueue(ctx context.Context, limit int) ([]TaskQueueEntry, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.GetQueue(ctx, limit)
}

// CompleteTask marks a task as completed.
func (a *GraphAdapter) CompleteTask(ctx context.Context, id string) (OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return OrchestratedTask{}, nil
	}
	return a.engine.CompleteTask(ctx, id)
}

// FailTask marks a task as failed.
func (a *GraphAdapter) FailTask(ctx context.Context, id string) (OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return OrchestratedTask{}, nil
	}
	return a.engine.FailTask(ctx, id)
}
