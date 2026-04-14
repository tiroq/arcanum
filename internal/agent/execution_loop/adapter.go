package executionloop

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter exposes the execution loop engine to the API and other subsystems.
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

// CreateTask creates a new execution task.
func (a *GraphAdapter) CreateTask(ctx context.Context, opportunityID, goal string) (ExecutionTask, error) {
	if a == nil || a.engine == nil {
		return ExecutionTask{}, nil
	}
	return a.engine.CreateTask(ctx, opportunityID, goal)
}

// GetTask retrieves a task by ID.
func (a *GraphAdapter) GetTask(ctx context.Context, id string) (ExecutionTask, error) {
	if a == nil || a.engine == nil {
		return ExecutionTask{}, nil
	}
	return a.engine.GetTask(ctx, id)
}

// ListTasks returns tasks with a limit.
func (a *GraphAdapter) ListTasks(ctx context.Context, limit int) ([]ExecutionTask, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListTasks(ctx, limit)
}

// RunLoop runs the execution loop for a task.
func (a *GraphAdapter) RunLoop(ctx context.Context, taskID string) (ExecutionTask, error) {
	if a == nil || a.engine == nil {
		return ExecutionTask{}, nil
	}
	return a.engine.RunLoop(ctx, taskID)
}

// AbortTask aborts a task.
func (a *GraphAdapter) AbortTask(ctx context.Context, taskID string) (ExecutionTask, error) {
	if a == nil || a.engine == nil {
		return ExecutionTask{}, nil
	}
	return a.engine.AbortTask(ctx, taskID)
}
