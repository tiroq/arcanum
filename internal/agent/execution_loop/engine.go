package executionloop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates the bounded execution loop.
// It is deterministic, observable, and governed by safety constraints.
type Engine struct {
	tasks    TaskStoreInterface
	obsStore ObservationStoreInterface
	planner  *Planner
	executor *Executor
	observer *Observer
	auditor  audit.AuditRecorder
	logger   *zap.Logger

	objective  ObjectiveProvider
	governance GovernanceProvider
}

// NewEngine creates a new execution loop engine.
func NewEngine(
	tasks TaskStoreInterface,
	obsStore ObservationStoreInterface,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	planner := NewPlanner([]string{"external_action"})
	observer := NewObserver(obsStore, logger)
	executor := NewExecutor(nil, logger)

	return &Engine{
		tasks:    tasks,
		obsStore: obsStore,
		planner:  planner,
		executor: executor,
		observer: observer,
		auditor:  auditor,
		logger:   logger,
	}
}

// WithObjective sets the objective provider for penalty-based abort.
func (e *Engine) WithObjective(o ObjectiveProvider) *Engine {
	e.objective = o
	return e
}

// WithGovernance sets the governance provider for execution gating.
func (e *Engine) WithGovernance(g GovernanceProvider) *Engine {
	e.governance = g
	e.executor.WithGovernance(g)
	return e
}

// WithExternalActions sets the external actions provider for step execution.
func (e *Engine) WithExternalActions(ea ExternalActionsProvider) *Engine {
	e.executor.extActions = ea
	return e
}

// CreateTask creates a new execution task in pending status.
func (e *Engine) CreateTask(ctx context.Context, opportunityID, goal string) (ExecutionTask, error) {
	now := nowUTC()
	task := ExecutionTask{
		ID:             uuid.New().String(),
		OpportunityID:  opportunityID,
		Goal:           goal,
		Status:         TaskStatusPending,
		Plan:           nil,
		CurrentStep:    0,
		IterationCount: 0,
		MaxIterations:  MaxIterations,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := e.tasks.Insert(ctx, task); err != nil {
		return ExecutionTask{}, fmt.Errorf("create task: %w", err)
	}

	e.auditEvent(ctx, "execution.task_created", map[string]any{
		"task_id":        task.ID,
		"opportunity_id": opportunityID,
		"goal":           goal,
	})

	return task, nil
}

// GetTask retrieves a task by ID.
func (e *Engine) GetTask(ctx context.Context, id string) (ExecutionTask, error) {
	return e.tasks.Get(ctx, id)
}

// ListTasks returns tasks with a limit.
func (e *Engine) ListTasks(ctx context.Context, limit int) ([]ExecutionTask, error) {
	return e.tasks.List(ctx, limit)
}

// AbortTask aborts a task if it is in a non-terminal state.
func (e *Engine) AbortTask(ctx context.Context, id string) (ExecutionTask, error) {
	task, err := e.tasks.Get(ctx, id)
	if err != nil {
		return ExecutionTask{}, err
	}

	if !ValidateTaskTransition(task.Status, TaskStatusAborted) {
		return ExecutionTask{}, fmt.Errorf("%w: cannot abort from %s", ErrInvalidTransition, task.Status)
	}

	task.Status = TaskStatusAborted
	task.AbortReason = "manual abort"
	task.UpdatedAt = nowUTC()

	if err := e.tasks.Update(ctx, task); err != nil {
		return ExecutionTask{}, fmt.Errorf("abort task: %w", err)
	}

	e.auditEvent(ctx, "execution.task_aborted", map[string]any{
		"task_id": id,
		"reason":  "manual abort",
	})

	return task, nil
}

// RunLoop executes one full iteration of the execution loop for a task.
// This is the core GOAL → PLAN → EXECUTE → OBSERVE → REFLECT → UPDATE cycle.
// It is bounded by MaxIterations and MaxExecutionTime.
func (e *Engine) RunLoop(ctx context.Context, taskID string) (ExecutionTask, error) {
	task, err := e.tasks.Get(ctx, taskID)
	if err != nil {
		return ExecutionTask{}, err
	}

	// Only pending or running tasks can be executed.
	if task.Status != TaskStatusPending && task.Status != TaskStatusRunning {
		return task, fmt.Errorf("%w: task is %s", ErrInvalidTransition, task.Status)
	}

	// Transition to running if pending.
	if task.Status == TaskStatusPending {
		if !ValidateTaskTransition(task.Status, TaskStatusRunning) {
			return task, ErrInvalidTransition
		}
		task.Status = TaskStatusRunning
		task.UpdatedAt = nowUTC()
		if err := e.tasks.Update(ctx, task); err != nil {
			return task, fmt.Errorf("transition to running: %w", err)
		}
	}

	// Check governance before any execution.
	if e.governance != nil {
		mode := e.governance.GetMode(ctx)
		if mode == "frozen" || mode == "rollback_only" {
			return e.abortTask(ctx, task, fmt.Sprintf("governance mode is %s", mode))
		}
	}

	// Main loop — bounded by MaxIterations.
	deadline := nowUTC().Add(time.Duration(MaxExecutionTimeSec) * time.Second)

	for task.IterationCount < task.MaxIterations {
		// Check time budget.
		if nowUTC().After(deadline) {
			return e.abortTask(ctx, task, "execution time exceeded")
		}

		// Check objective penalty signal.
		if e.objective != nil {
			sigType := e.objective.GetSignalType(ctx)
			sigStrength := e.objective.GetSignalStrength(ctx)
			if sigType == "penalty" && sigStrength > 0.05 {
				return e.abortTask(ctx, task, fmt.Sprintf("objective penalty signal: strength=%.2f", sigStrength))
			}
		}

		// Check consecutive failures via observer.
		if shouldAbort, reason := e.observer.ShouldAbort(ctx, task.ID); shouldAbort {
			return e.abortTask(ctx, task, reason)
		}

		task.IterationCount++

		// PLAN: generate plan if none exists or all steps are terminal.
		if len(task.Plan) == 0 || e.allStepsTerminal(task.Plan) {
			plan, planErr := e.planner.GeneratePlan(PlannerInput{
				Goal: task.Goal,
				Context: PlannerContext{
					OpportunityID:  task.OpportunityID,
					IterationCount: task.IterationCount,
				},
				AvailableTools: []string{"external_action"},
				Constraints:    PlannerConstraints{MaxSteps: MaxStepsPerPlan},
			})
			if planErr != nil {
				e.logger.Warn("plan generation failed", zap.Error(planErr))
				task.UpdatedAt = nowUTC()
				_ = e.tasks.Update(ctx, task)
				continue
			}
			task.Plan = plan
			task.CurrentStep = 0

			e.auditEvent(ctx, "execution.plan_generated", map[string]any{
				"task_id":    task.ID,
				"step_count": len(plan),
				"iteration":  task.IterationCount,
			})
		}

		// EXECUTE: get next pending step.
		step, stepIdx := e.getNextPendingStep(task)
		if step == nil {
			// All steps executed/blocked/skipped — check if goal is satisfied.
			if e.goalSatisfied(task) {
				return e.completeTask(ctx, task)
			}
			// No actionable steps remain — continue to next iteration (will re-plan).
			task.Plan = nil
			task.UpdatedAt = nowUTC()
			_ = e.tasks.Update(ctx, task)
			continue
		}

		task.CurrentStep = stepIdx

		// Check if step is blocked (identical failure detected).
		if e.observer.IsStepBlocked(ctx, task.ID, step.ID) {
			task.Plan[stepIdx].Status = StepStatusBlocked
			task.UpdatedAt = nowUTC()
			_ = e.tasks.Update(ctx, task)
			continue
		}

		// Execute the step.
		result := e.executor.Execute(ctx, *step, task.OpportunityID)

		// OBSERVE: record the observation.
		obs := ExecutionObservation{
			StepID:    step.ID,
			TaskID:    task.ID,
			Success:   result.Success,
			Timestamp: nowUTC(),
		}
		if result.Output != nil {
			obs.Output = result.Output
		}
		if result.Error != "" {
			obs.Error = result.Error
		}
		if err := e.observer.Record(ctx, obs); err != nil {
			e.logger.Warn("failed to record observation", zap.Error(err))
		}

		// REFLECT + UPDATE: update step status based on result.
		if result.RequiresReview {
			task.Plan[stepIdx].Status = StepStatusPendingReview
			task.Plan[stepIdx].AttemptCount++

			e.auditEvent(ctx, "execution.step_executed", map[string]any{
				"task_id":         task.ID,
				"step_id":         step.ID,
				"status":          "pending_review",
				"requires_review": true,
			})
		} else if result.Success {
			task.Plan[stepIdx].Status = StepStatusExecuted
			task.Plan[stepIdx].ResultRef = result.ActionID

			e.auditEvent(ctx, "execution.step_executed", map[string]any{
				"task_id": task.ID,
				"step_id": step.ID,
				"status":  "executed",
			})
		} else {
			task.Plan[stepIdx].Status = StepStatusFailed
			task.Plan[stepIdx].AttemptCount++

			e.auditEvent(ctx, "execution.step_failed", map[string]any{
				"task_id": task.ID,
				"step_id": step.ID,
				"error":   result.Error,
				"attempt": task.Plan[stepIdx].AttemptCount,
			})

			// Adapt plan on failure.
			task.Plan = e.planner.AdaptPlan(task.Plan, step.ID, result.Error)
		}

		// Persist task state.
		task.UpdatedAt = nowUTC()
		if err := e.tasks.Update(ctx, task); err != nil {
			e.logger.Warn("failed to persist task", zap.Error(err))
		}

		// Check if all steps are now complete.
		if e.goalSatisfied(task) {
			return e.completeTask(ctx, task)
		}
	}

	// Max iterations reached.
	return e.abortTask(ctx, task, "maximum iterations reached")
}

// --- internal helpers ---

func (e *Engine) abortTask(ctx context.Context, task ExecutionTask, reason string) (ExecutionTask, error) {
	task.Status = TaskStatusAborted
	task.AbortReason = reason
	task.UpdatedAt = nowUTC()
	if err := e.tasks.Update(ctx, task); err != nil {
		return task, fmt.Errorf("abort task: %w", err)
	}
	e.auditEvent(ctx, "execution.task_aborted", map[string]any{
		"task_id": task.ID,
		"reason":  reason,
	})
	return task, nil
}

func (e *Engine) completeTask(ctx context.Context, task ExecutionTask) (ExecutionTask, error) {
	task.Status = TaskStatusCompleted
	task.UpdatedAt = nowUTC()
	if err := e.tasks.Update(ctx, task); err != nil {
		return task, fmt.Errorf("complete task: %w", err)
	}
	e.auditEvent(ctx, "execution.task_completed", map[string]any{
		"task_id":    task.ID,
		"iterations": task.IterationCount,
	})
	return task, nil
}

func (e *Engine) getNextPendingStep(task ExecutionTask) (*ExecutionStep, int) {
	for i := range task.Plan {
		if task.Plan[i].Status == StepStatusPending {
			return &task.Plan[i], i
		}
	}
	return nil, -1
}

func (e *Engine) allStepsTerminal(steps []ExecutionStep) bool {
	for _, s := range steps {
		if s.Status == StepStatusPending {
			return false
		}
	}
	return true
}

func (e *Engine) goalSatisfied(task ExecutionTask) bool {
	if len(task.Plan) == 0 {
		return false
	}
	for _, s := range task.Plan {
		if s.Status == StepStatusPending || s.Status == StepStatusPendingReview {
			return false
		}
	}
	// Goal is satisfied if at least one step executed successfully.
	for _, s := range task.Plan {
		if s.Status == StepStatusExecuted {
			return true
		}
	}
	return false
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		e.logger.Warn("failed to marshal audit payload", zap.Error(err))
		return
	}
	_ = e.auditor.RecordEvent(ctx, "execution_loop", uuid.Nil, eventType, "system", "execution_engine", payloadJSON)
}
