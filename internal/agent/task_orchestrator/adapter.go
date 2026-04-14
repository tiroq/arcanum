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

// FailTaskWithReason marks a task as failed with a reason.
func (a *GraphAdapter) FailTaskWithReason(ctx context.Context, id, reason string) (OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return OrchestratedTask{}, nil
	}
	return a.engine.FailTaskWithReason(ctx, id, reason)
}

// PauseTask marks a task as paused.
func (a *GraphAdapter) PauseTask(ctx context.Context, id string) (OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return OrchestratedTask{}, nil
	}
	return a.engine.PauseTask(ctx, id)
}

// ListRunningTasks returns currently running tasks.
func (a *GraphAdapter) ListRunningTasks(ctx context.Context, limit int) ([]OrchestratedTask, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListRunningTasks(ctx, limit)
}

// FindByActuationDecision returns the task ID linked to the given actuation decision.
func (a *GraphAdapter) FindByActuationDecision(ctx context.Context, decisionID string) (string, error) {
	if a == nil || a.engine == nil {
		return "", nil
	}
	return a.engine.FindByActuationDecision(ctx, decisionID)
}

// SetActuationDecisionID links an actuation decision to a task.
func (a *GraphAdapter) SetActuationDecisionID(ctx context.Context, taskID, decisionID string) error {
	if a == nil || a.engine == nil {
		return nil
	}
	return a.engine.SetActuationDecisionID(ctx, taskID, decisionID)
}

// SetExecutionTaskID links an execution task to an orchestrated task.
func (a *GraphAdapter) SetExecutionTaskID(ctx context.Context, taskID, execTaskID string) error {
	if a == nil || a.engine == nil {
		return nil
	}
	return a.engine.SetExecutionTaskID(ctx, taskID, execTaskID)
}

// SetOutcome records execution outcome on a task.
func (a *GraphAdapter) SetOutcome(ctx context.Context, taskID, outcomeType, lastError string, attemptCount int) error {
	if a == nil || a.engine == nil {
		return nil
	}
	return a.engine.SetOutcome(ctx, taskID, outcomeType, lastError, attemptCount)
}
