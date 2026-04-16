package taskorchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates multi-task priority scheduling and dispatch.
// It is deterministic, observable, and governed by safety constraints.
type Engine struct {
	tasks   TaskStoreInterface
	queue   QueueStoreInterface
	auditor audit.AuditRecorder
	logger  *zap.Logger

	objective     ObjectiveProvider
	governance    GovernanceProvider
	capacity      CapacityProvider
	portfolio     PortfolioProvider
	executionLoop ExecutionLoopProvider
	vector        VectorProvider
}

// NewEngine creates a new task orchestrator engine.
func NewEngine(
	tasks TaskStoreInterface,
	queue QueueStoreInterface,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		tasks:   tasks,
		queue:   queue,
		auditor: auditor,
		logger:  logger,
	}
}

// WithObjective sets the objective provider.
func (e *Engine) WithObjective(o ObjectiveProvider) *Engine {
	e.objective = o
	return e
}

// WithGovernance sets the governance provider.
func (e *Engine) WithGovernance(g GovernanceProvider) *Engine {
	e.governance = g
	return e
}

// WithCapacity sets the capacity provider.
func (e *Engine) WithCapacity(c CapacityProvider) *Engine {
	e.capacity = c
	return e
}

// WithPortfolio sets the portfolio provider.
func (e *Engine) WithPortfolio(p PortfolioProvider) *Engine {
	e.portfolio = p
	return e
}

// WithExecutionLoop sets the execution loop provider.
func (e *Engine) WithExecutionLoop(el ExecutionLoopProvider) *Engine {
	e.executionLoop = el
	return e
}

// WithVector sets the system vector provider for priority adjustment.
func (e *Engine) WithVector(v VectorProvider) *Engine {
	e.vector = v
	return e
}

// CreateTask creates a new orchestrated task.
func (e *Engine) CreateTask(ctx context.Context, source, goal string, urgency, expectedValue, riskLevel float64, strategyType string) (OrchestratedTask, error) {
	now := nowUTC()
	task := OrchestratedTask{
		ID:            uuid.New().String(),
		Source:        source,
		Goal:          goal,
		PriorityScore: 0,
		Status:        TaskStatusPending,
		Urgency:       clamp01(urgency),
		ExpectedValue: expectedValue,
		RiskLevel:     clamp01(riskLevel),
		StrategyType:  strategyType,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := e.tasks.Insert(ctx, task); err != nil {
		return OrchestratedTask{}, fmt.Errorf("create task: %w", err)
	}

	e.auditEvent(ctx, "task.created", map[string]any{
		"task_id": task.ID,
		"source":  source,
		"goal":    goal,
	})

	return task, nil
}

// GetTask retrieves a task by ID.
func (e *Engine) GetTask(ctx context.Context, id string) (OrchestratedTask, error) {
	return e.tasks.Get(ctx, id)
}

// ListTasks returns tasks with a limit.
func (e *Engine) ListTasks(ctx context.Context, limit int) ([]OrchestratedTask, error) {
	return e.tasks.List(ctx, limit)
}

// RecomputePriorities re-scores all non-terminal, non-running tasks and updates the queue.
func (e *Engine) RecomputePriorities(ctx context.Context) error {
	now := nowUTC()
	input := e.gatherScoringInput(ctx)

	// Fetch pending and queued tasks.
	pending, err := e.tasks.ListByStatus(ctx, TaskStatusPending, MaxTasksInQueue)
	if err != nil {
		return fmt.Errorf("list pending tasks: %w", err)
	}
	queued, err := e.tasks.ListByStatus(ctx, TaskStatusQueued, MaxTasksInQueue)
	if err != nil {
		return fmt.Errorf("list queued tasks: %w", err)
	}
	paused, err := e.tasks.ListByStatus(ctx, TaskStatusPaused, MaxTasksInQueue)
	if err != nil {
		return fmt.Errorf("list paused tasks: %w", err)
	}

	// Combine all scoreable tasks.
	var tasks []OrchestratedTask
	tasks = append(tasks, pending...)
	tasks = append(tasks, queued...)
	tasks = append(tasks, paused...)

	scored := 0
	expired := 0
	for _, task := range tasks {
		// Expire old tasks.
		if IsExpired(task, now) {
			task.Status = TaskStatusFailed
			task.UpdatedAt = now
			_ = e.tasks.Update(ctx, task)
			_ = e.queue.Remove(ctx, task.ID)
			expired++
			continue
		}

		// Skip running tasks (do not re-score).
		if task.Status == TaskStatusRunning {
			continue
		}

		// Cooldown prevents re-scoring thrash, but pending tasks must always
		// be able to enter the queue on their first recompute — otherwise
		// newly-created tasks are stuck in pending for the full cooldown
		// window. Cooldown applies only to tasks that have already been
		// scored (queued or paused).
		if task.Status != TaskStatusPending && IsInCooldown(task, now) {
			continue
		}

		// Compute portfolio boost.
		portfolioBoost := 0.0
		if e.portfolio != nil && task.StrategyType != "" {
			portfolioBoost = e.portfolio.GetStrategyBoost(ctx, task.StrategyType)
		}

		// Score.
		var priority float64
		if e.vector != nil {
			vp := VectorScoringParams{
				RiskTolerance:  e.vector.GetRiskTolerance(),
				IncomePriority: e.vector.GetIncomePriority(),
			}
			priority = ComputePriorityWithVector(task, input, portfolioBoost, now, vp)
		} else {
			priority = ComputePriority(task, input, portfolioBoost, now)
		}
		task.PriorityScore = priority
		task.UpdatedAt = now

		// Move pending → queued.
		if task.Status == TaskStatusPending {
			task.Status = TaskStatusQueued
		}

		if err := e.tasks.Update(ctx, task); err != nil {
			e.logger.Warn("task_orchestrator: failed to update task", zap.String("task_id", task.ID), zap.Error(err))
			continue
		}

		// Update queue.
		if err := e.queue.Upsert(ctx, TaskQueueEntry{
			TaskID:        task.ID,
			PriorityScore: priority,
			InsertedAt:    task.CreatedAt,
			LastUpdatedAt: now,
		}); err != nil {
			e.logger.Warn("task_orchestrator: failed to upsert queue entry", zap.String("task_id", task.ID), zap.Error(err))
			continue
		}

		e.auditEvent(ctx, "task.scored", map[string]any{
			"task_id":  task.ID,
			"priority": priority,
		})
		scored++
	}

	e.auditEvent(ctx, "task.recompute_completed", map[string]any{
		"scored":  scored,
		"expired": expired,
	})

	queueSize, _ := e.queue.Count(ctx)
	e.logger.Info("task_orchestrator: recompute completed",
		zap.Int("scored", scored),
		zap.Int("expired", expired),
		zap.Int("pending_in", len(pending)),
		zap.Int("queued_in", len(queued)),
		zap.Int("paused_in", len(paused)),
		zap.Int("queue_size_after", queueSize),
	)

	return nil
}

// Dispatch runs a single orchestration cycle: selects top tasks and sends them to execution_loop.
func (e *Engine) Dispatch(ctx context.Context) (DispatchResult, error) {
	result := DispatchResult{}

	// Check governance.
	govMode := e.getGovernanceMode(ctx)
	if govMode == "frozen" {
		e.logger.Info("task_orchestrator: dispatch blocked, governance frozen")
		return result, ErrGovernanceFrozen
	}

	// Check capacity for overload reduction.
	capacityLoad := 0.0
	if e.capacity != nil {
		capacityLoad = e.capacity.GetLoad(ctx)
	}

	// Determine dispatch count.
	maxDispatch := MaxTasksPerCycle
	if ShouldReduceDispatch(capacityLoad) {
		maxDispatch = 1
	}

	// Count currently running tasks.
	runningCount, err := e.tasks.CountByStatus(ctx, TaskStatusRunning)
	if err != nil {
		return result, fmt.Errorf("count running tasks: %w", err)
	}

	availableSlots := MaxRunningTasks - runningCount
	if availableSlots <= 0 {
		return result, ErrMaxRunning
	}

	if maxDispatch > availableSlots {
		maxDispatch = availableSlots
	}

	// Get top entries from queue.
	entries, err := e.queue.List(ctx, maxDispatch*2) // fetch extra for filtering
	if err != nil {
		return result, fmt.Errorf("list queue: %w", err)
	}

	dispatched := 0
	now := nowUTC()
	for _, entry := range entries {
		if dispatched >= maxDispatch {
			break
		}

		task, err := e.tasks.Get(ctx, entry.TaskID)
		if err != nil {
			e.logger.Warn("task_orchestrator: task not found for queue entry", zap.String("task_id", entry.TaskID))
			_ = e.queue.Remove(ctx, entry.TaskID)
			continue
		}

		// Skip terminal or running tasks.
		if task.Status.IsTerminal() || task.Status == TaskStatusRunning {
			_ = e.queue.Remove(ctx, task.ID)
			continue
		}

		// Risk gate.
		if task.RiskLevel >= BlockedRisk {
			task.Status = TaskStatusPaused
			task.UpdatedAt = now
			_ = e.tasks.Update(ctx, task)
			_ = e.queue.Remove(ctx, task.ID)
			result.Blocked = append(result.Blocked, task.ID)
			e.auditEvent(ctx, "task.paused", map[string]any{
				"task_id": task.ID,
				"reason":  "risk_blocked",
			})
			continue
		}

		// Review gate for high-risk tasks in supervised mode.
		if task.RiskLevel > HighRiskThreshold && govMode == "supervised" {
			task.Status = TaskStatusPaused
			task.UpdatedAt = now
			_ = e.tasks.Update(ctx, task)
			// Remove from queue to prevent the same task thrashing on
			// every dispatch cycle. Resuming is an explicit action.
			_ = e.queue.Remove(ctx, task.ID)
			result.Skipped = append(result.Skipped, task.ID)
			e.auditEvent(ctx, "task.paused", map[string]any{
				"task_id": task.ID,
				"reason":  "requires_review",
			})
			continue
		}

		// Dispatch to execution loop.
		if e.executionLoop == nil {
			result.Skipped = append(result.Skipped, task.ID)
			e.logger.Warn("task_orchestrator: dispatch skipped, no execution loop configured",
				zap.String("task_id", task.ID),
			)
			continue
		}

		outcome, execErr := e.executionLoop.CreateAndRun(ctx, task.Goal)
		if execErr != nil {
			e.logger.Warn("task_orchestrator: dispatch failed",
				zap.String("task_id", task.ID),
				zap.Error(execErr),
			)
			// Treat dispatch error as task failure so state advances and
			// no ghost entry is left behind.
			task.Status = TaskStatusFailed
			task.LastError = execErr.Error()
			task.AttemptCount++
			task.CompletedAt = timePtr(now)
			task.UpdatedAt = now
			_ = e.tasks.Update(ctx, task)
			_ = e.queue.Remove(ctx, task.ID)
			result.Skipped = append(result.Skipped, task.ID)
			e.auditEvent(ctx, "task.failed", map[string]any{
				"task_id": task.ID,
				"reason":  "dispatch_error",
				"error":   execErr.Error(),
			})
			continue
		}

		// Transition to running and link execution task.
		task.Status = TaskStatusRunning
		task.ExecutionTaskID = outcome.ExecutionID
		task.AttemptCount++
		task.UpdatedAt = now
		if err := e.tasks.Update(ctx, task); err != nil {
			e.logger.Warn("task_orchestrator: failed to update task status",
				zap.String("task_id", task.ID),
				zap.Error(err),
			)
		}

		// Remove from queue — task is now running (or terminal).
		_ = e.queue.Remove(ctx, task.ID)

		result.Dispatched = append(result.Dispatched, task.ID)
		dispatched++

		e.auditEvent(ctx, "task.dispatched", map[string]any{
			"task_id":      task.ID,
			"execution_id": outcome.ExecutionID,
			"priority":     task.PriorityScore,
		})

		// Propagate terminal status from execution_loop. Because RunLoop is
		// currently synchronous and bounded, most dispatches return a
		// terminal status and the orchestrator must reflect that immediately.
		e.applyExecutionOutcome(ctx, task, outcome, now)
	}

	e.logger.Info("task_orchestrator: dispatch completed",
		zap.String("governance_mode", govMode),
		zap.Float64("capacity_load", capacityLoad),
		zap.Int("max_dispatch", maxDispatch),
		zap.Int("available_slots", availableSlots),
		zap.Int("queue_entries_considered", len(entries)),
		zap.Int("dispatched", len(result.Dispatched)),
		zap.Int("skipped", len(result.Skipped)),
		zap.Int("blocked", len(result.Blocked)),
	)

	return result, nil
}

// CompleteTask marks a task as completed.
func (e *Engine) CompleteTask(ctx context.Context, id string) (OrchestratedTask, error) {
	return e.transitionTask(ctx, id, TaskStatusCompleted, "task.completed")
}

// FailTask marks a task as failed.
func (e *Engine) FailTask(ctx context.Context, id string) (OrchestratedTask, error) {
	return e.transitionTask(ctx, id, TaskStatusFailed, "task.failed")
}

// FailTaskWithReason marks a task as failed with a reason.
func (e *Engine) FailTaskWithReason(ctx context.Context, id, reason string) (OrchestratedTask, error) {
	task, err := e.tasks.Get(ctx, id)
	if err != nil {
		return OrchestratedTask{}, err
	}

	if !ValidateTransition(task.Status, TaskStatusFailed) {
		return OrchestratedTask{}, ErrInvalidTransition
	}

	task.Status = TaskStatusFailed
	task.LastError = reason
	task.UpdatedAt = nowUTC()
	if err := e.tasks.Update(ctx, task); err != nil {
		return OrchestratedTask{}, fmt.Errorf("update task: %w", err)
	}

	_ = e.queue.Remove(ctx, id)

	e.auditEvent(ctx, "task.failed", map[string]any{
		"task_id": id,
		"status":  string(TaskStatusFailed),
		"reason":  reason,
	})

	return task, nil
}

// ListRunningTasks returns running tasks with their execution linkage.
func (e *Engine) ListRunningTasks(ctx context.Context, limit int) ([]OrchestratedTask, error) {
	return e.tasks.ListByStatus(ctx, TaskStatusRunning, limit)
}

// SetActuationDecisionID links an actuation decision to a task.
func (e *Engine) SetActuationDecisionID(ctx context.Context, taskID, decisionID string) error {
	return e.tasks.SetActuationDecisionID(ctx, taskID, decisionID)
}

// SetExecutionTaskID links an execution task to an orchestrated task.
func (e *Engine) SetExecutionTaskID(ctx context.Context, taskID, execTaskID string) error {
	return e.tasks.SetExecutionTaskID(ctx, taskID, execTaskID)
}

// SetOutcome records the execution outcome on a task.
func (e *Engine) SetOutcome(ctx context.Context, taskID, outcomeType, lastError string, attemptCount int) error {
	return e.tasks.SetOutcome(ctx, taskID, outcomeType, lastError, attemptCount)
}

// FindByActuationDecision returns the task ID linked to the given actuation decision.
func (e *Engine) FindByActuationDecision(ctx context.Context, decisionID string) (string, error) {
	return e.tasks.FindByActuationDecision(ctx, decisionID)
}

// PauseTask marks a task as paused.
func (e *Engine) PauseTask(ctx context.Context, id string) (OrchestratedTask, error) {
	return e.transitionTask(ctx, id, TaskStatusPaused, "task.paused")
}

// GetQueue returns the current queue state.
func (e *Engine) GetQueue(ctx context.Context, limit int) ([]TaskQueueEntry, error) {
	if limit <= 0 {
		limit = MaxTasksInQueue
	}
	return e.queue.List(ctx, limit)
}

// --- internal helpers ---

func (e *Engine) transitionTask(ctx context.Context, id string, to TaskStatus, event string) (OrchestratedTask, error) {
	task, err := e.tasks.Get(ctx, id)
	if err != nil {
		return OrchestratedTask{}, err
	}

	if !ValidateTransition(task.Status, to) {
		return OrchestratedTask{}, ErrInvalidTransition
	}

	task.Status = to
	task.UpdatedAt = nowUTC()
	if err := e.tasks.Update(ctx, task); err != nil {
		return OrchestratedTask{}, fmt.Errorf("update task: %w", err)
	}

	// Remove from queue on any non-dispatchable state (terminal or paused).
	// Only queued tasks should live in the queue.
	if to.IsTerminal() || to == TaskStatusPaused {
		_ = e.queue.Remove(ctx, id)
	}

	e.auditEvent(ctx, event, map[string]any{
		"task_id": id,
		"status":  string(to),
	})

	return task, nil
}

func (e *Engine) gatherScoringInput(ctx context.Context) ScoringInput {
	input := ScoringInput{}
	if e.objective != nil {
		input.ObjectiveSignalType = e.objective.GetSignalType(ctx)
		input.ObjectiveSignalStrength = e.objective.GetSignalStrength(ctx)
	}
	if e.capacity != nil {
		input.CapacityLoad = e.capacity.GetLoad(ctx)
	}
	return input
}

func (e *Engine) getGovernanceMode(ctx context.Context) string {
	if e.governance == nil {
		return "autonomous"
	}
	return e.governance.GetMode(ctx)
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx,
		"task_orchestrator", uuid.Nil,
		eventType, "system", "task_orchestrator",
		payload,
	)
}

// applyExecutionOutcome transitions the orchestrated task based on the
// terminal status returned by the execution loop. Silent failures are not
// acceptable: unknown or empty status is treated as failure so no task can
// get stuck in running forever.
func (e *Engine) applyExecutionOutcome(ctx context.Context, task OrchestratedTask, outcome DispatchOutcome, now time.Time) {
	switch outcome.TerminalStatus {
	case "running", "":
		// Execution did not finish synchronously. Leave task in running
		// state; completion must arrive via another callback.
		if outcome.TerminalStatus == "" {
			e.logger.Info("task_orchestrator: execution returned no terminal status, leaving task running",
				zap.String("task_id", task.ID),
				zap.String("execution_id", outcome.ExecutionID),
			)
		}
		return
	case "completed":
		task.Status = TaskStatusCompleted
		task.OutcomeType = "success"
		task.CompletedAt = timePtr(now)
		task.UpdatedAt = now
		_ = e.tasks.Update(ctx, task)
		_ = e.queue.Remove(ctx, task.ID)
		e.auditEvent(ctx, "task.completed", map[string]any{
			"task_id":      task.ID,
			"execution_id": outcome.ExecutionID,
		})
	case "failed", "aborted":
		task.Status = TaskStatusFailed
		task.OutcomeType = outcome.TerminalStatus
		task.LastError = outcome.Error
		task.CompletedAt = timePtr(now)
		task.UpdatedAt = now
		_ = e.tasks.Update(ctx, task)
		_ = e.queue.Remove(ctx, task.ID)
		e.auditEvent(ctx, "task.failed", map[string]any{
			"task_id":      task.ID,
			"execution_id": outcome.ExecutionID,
			"reason":       outcome.TerminalStatus,
			"error":        outcome.Error,
		})
	default:
		e.logger.Warn("task_orchestrator: unknown execution terminal status",
			zap.String("task_id", task.ID),
			zap.String("execution_id", outcome.ExecutionID),
			zap.String("status", outcome.TerminalStatus),
		)
		task.Status = TaskStatusFailed
		task.OutcomeType = "unknown_terminal_status"
		task.LastError = "unknown terminal status: " + outcome.TerminalStatus
		task.CompletedAt = timePtr(now)
		task.UpdatedAt = now
		_ = e.tasks.Update(ctx, task)
		_ = e.queue.Remove(ctx, task.ID)
		e.auditEvent(ctx, "task.failed", map[string]any{
			"task_id":      task.ID,
			"execution_id": outcome.ExecutionID,
			"reason":       "unknown_terminal_status",
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }
