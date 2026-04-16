package autonomy

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// --- Chain closure types ---

// TaskOrchestratorRunner manages task lifecycle from the autonomy orchestrator.
type TaskOrchestratorRunner interface {
	CreateTask(ctx context.Context, source, goal string, urgency, expectedValue, riskLevel float64, strategyType string) (CreatedTaskInfo, error)
	RecomputePriorities(ctx context.Context) error
	Dispatch(ctx context.Context) (DispatchResultInfo, error)
	FindByActuationDecision(ctx context.Context, decisionID string) (string, error) // returns task ID or "" if not found
	CompleteTask(ctx context.Context, id string) error
	FailTask(ctx context.Context, id, reason string) error
	PauseTask(ctx context.Context, id string) error
	SetActuationDecisionID(ctx context.Context, taskID, decisionID string) error
	SetExecutionTaskID(ctx context.Context, taskID, execTaskID string) error
	SetOutcome(ctx context.Context, taskID, outcomeType, lastError string, attemptCount int) error
	ListRunningTasks(ctx context.Context, limit int) ([]RunningTaskInfo, error)
}

// ExecutionLoopRunner provides execution lifecycle status.
type ExecutionLoopRunner interface {
	GetTask(ctx context.Context, id string) (ExecTaskInfo, error)
}

// ExecutionFeedbackStore persists structured execution feedback.
type ExecutionFeedbackStore interface {
	Insert(ctx context.Context, f ExecutionFeedback) error
	ListRecent(ctx context.Context, limit int) ([]ExecutionFeedback, error)
	CountByOutcome(ctx context.Context, outcomeType string, since time.Time) (int, error)
	CountBySignal(ctx context.Context, signal string, since time.Time) (int, error)
}

// GoalPlanningRunner runs goal decomposition and task emission cycles.
type GoalPlanningRunner interface {
	RunCycle(ctx context.Context) error
}

// --- Lightweight result types (no import cycles) ---

// CreatedTaskInfo is the result of creating a task via TaskOrchestratorRunner.
type CreatedTaskInfo struct {
	ID string
}

// DispatchResultInfo is the result of dispatching tasks.
type DispatchResultInfo struct {
	DispatchedCount int
	SkippedCount    int
	BlockedCount    int
	// DispatchedTaskIDs maps task ID to execution task ID.
	DispatchedTaskIDs map[string]string
}

// RunningTaskInfo describes a running orchestrated task.
type RunningTaskInfo struct {
	ID              string
	ExecutionTaskID string
	Goal            string
	AttemptCount    int
}

// ExecTaskInfo describes an execution loop task's status.
type ExecTaskInfo struct {
	ID             string
	Status         string // pending, running, completed, failed, aborted
	AbortReason    string
	IterationCount int
	StepsExecuted  int
	StepsFailed    int
	HasReviewBlock bool
}

// ExecutionFeedback is a structured record of execution outcomes.
type ExecutionFeedback struct {
	ID                 string    `json:"id"`
	TaskID             string    `json:"task_id"`
	ExecutionTaskID    string    `json:"execution_task_id"`
	OutcomeType        string    `json:"outcome_type"` // completed, failed, aborted, blocked
	Success            bool      `json:"success"`
	StepsExecuted      int       `json:"steps_executed"`
	StepsFailed        int       `json:"steps_failed"`
	ErrorSummary       string    `json:"error_summary"`
	SemanticSignal     string    `json:"semantic_signal"` // e.g. safe_action_succeeded, repeated_failure
	SourceDecisionType string    `json:"source_decision_type"`
	CreatedAt          time.Time `json:"created_at"`
}

// --- Actuation → Task materialization ---

// actuationTypeToGoal maps actuation decision types to bounded goal strings.
var actuationTypeToGoal = map[string]string{
	"stabilize_income":    "stabilize income by prioritizing safe revenue actions",
	"reduce_load":         "reduce owner overload by deferring low-value work",
	"rebalance_portfolio": "rebalance revenue strategy allocations",
	"adjust_pricing":      "recompute pricing for active opportunities",
	"shift_scheduling":    "shift scheduling to optimize time allocation",
	"increase_discovery":  "increase opportunity discovery activity",
	"trigger_automation":  "trigger automation workflow for efficiency",
}

// actuationTypeToStrategy maps actuation types to strategy types.
var actuationTypeToStrategy = map[string]string{
	"stabilize_income":    "consulting",
	"reduce_load":         "",
	"rebalance_portfolio": "",
	"adjust_pricing":      "",
	"shift_scheduling":    "",
	"increase_discovery":  "",
	"trigger_automation":  "automation_services",
}

// MaterializeDecisionAsTask converts a safe actuation decision into an orchestrated task.
// Returns the task ID and whether a task was actually created (false if skipped/duplicate).
func (o *Orchestrator) MaterializeDecisionAsTask(ctx context.Context, d ActuationDecisionInfo) (string, bool, error) {
	if o.taskOrchestrator == nil {
		return "", false, nil // fail-open
	}

	// 1. Check governance: frozen blocks task creation.
	mode := o.getEffectiveMode()
	if mode == ModeFrozen {
		o.auditEvent(ctx, "actuation.task_blocked_by_governance", map[string]any{
			"decision_id":   d.ID,
			"decision_type": d.Type,
			"mode":          string(mode),
		})
		return "", false, nil
	}

	// 2. In supervised mode, review-required or high-risk decisions must not auto-create runnable tasks.
	if mode == ModeSupervisedAutonomy && d.RequiresReview {
		o.auditEvent(ctx, "actuation.task_skipped_review_required", map[string]any{
			"decision_id":   d.ID,
			"decision_type": d.Type,
		})
		return "", false, nil
	}

	// 3. Deduplicate: check if task already exists for this decision.
	existingTaskID, err := o.taskOrchestrator.FindByActuationDecision(ctx, d.ID)
	if err != nil {
		o.logger.Warn("chain_closure: failed to check for existing task", zap.Error(err))
	}
	if existingTaskID != "" {
		o.auditEvent(ctx, "actuation.task_skipped_duplicate", map[string]any{
			"decision_id":   d.ID,
			"decision_type": d.Type,
			"existing_task": existingTaskID,
		})
		return existingTaskID, false, nil
	}

	// 4. Build task parameters.
	goal := actuationTypeToGoal[d.Type]
	if goal == "" {
		goal = fmt.Sprintf("execute actuation decision: %s", d.Type)
	}
	strategy := actuationTypeToStrategy[d.Type]
	urgency := clamp01Orch(d.Priority) // derive urgency from decision priority

	// Risk: review-required decisions get higher risk.
	riskLevel := 0.3
	if d.RequiresReview {
		riskLevel = 0.6
	}

	// 5. Create the task.
	result, err := o.taskOrchestrator.CreateTask(ctx, "actuation", goal, urgency, d.Priority, riskLevel, strategy)
	if err != nil {
		return "", false, fmt.Errorf("create task from actuation decision: %w", err)
	}

	// 6. Link the actuation decision to the task.
	if err := o.taskOrchestrator.SetActuationDecisionID(ctx, result.ID, d.ID); err != nil {
		o.logger.Warn("chain_closure: failed to link decision to task", zap.Error(err))
	}

	o.auditEvent(ctx, "actuation.task_created", map[string]any{
		"decision_id":   d.ID,
		"decision_type": d.Type,
		"task_id":       result.ID,
		"goal":          goal,
		"urgency":       urgency,
		"risk_level":    riskLevel,
	})

	return result.ID, true, nil
}

// --- Task lifecycle closure ---

// PropagateExecutionResults checks running tasks and propagates execution outcomes
// back to the task orchestrator and structured feedback.
func (o *Orchestrator) PropagateExecutionResults(ctx context.Context) (int, int, int, error) {
	if o.taskOrchestrator == nil || o.executionLoop == nil {
		return 0, 0, 0, nil
	}

	running, err := o.taskOrchestrator.ListRunningTasks(ctx, MaxRunningTasks*2)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list running tasks: %w", err)
	}

	completed := 0
	failed := 0
	paused := 0

	for _, task := range running {
		if task.ExecutionTaskID == "" {
			continue // no execution task linked yet
		}

		execInfo, err := o.executionLoop.GetTask(ctx, task.ExecutionTaskID)
		if err != nil {
			o.logger.Warn("chain_closure: failed to get execution task",
				zap.String("exec_task_id", task.ExecutionTaskID),
				zap.Error(err),
			)
			continue
		}

		switch execInfo.Status {
		case "completed":
			if err := o.taskOrchestrator.CompleteTask(ctx, task.ID); err != nil {
				o.logger.Warn("chain_closure: failed to complete task", zap.Error(err))
				continue
			}
			_ = o.taskOrchestrator.SetOutcome(ctx, task.ID, "completed", "", task.AttemptCount+1)
			o.recordFeedback(ctx, task, execInfo, "completed", true, "safe_action_succeeded")
			completed++

			o.auditEvent(ctx, "task.execution_completed", map[string]any{
				"task_id":      task.ID,
				"exec_task_id": task.ExecutionTaskID,
				"iterations":   execInfo.IterationCount,
			})

		case "failed":
			if err := o.taskOrchestrator.FailTask(ctx, task.ID, "execution failed"); err != nil {
				o.logger.Warn("chain_closure: failed to fail task", zap.Error(err))
				continue
			}
			signal := "execution_failure"
			if task.AttemptCount >= 2 {
				signal = "repeated_failure"
			}
			_ = o.taskOrchestrator.SetOutcome(ctx, task.ID, "failed", execInfo.AbortReason, task.AttemptCount+1)
			o.recordFeedback(ctx, task, execInfo, "failed", false, signal)
			failed++

			o.auditEvent(ctx, "task.execution_failed", map[string]any{
				"task_id":      task.ID,
				"exec_task_id": task.ExecutionTaskID,
				"reason":       execInfo.AbortReason,
			})

		case "aborted":
			// Determine whether to pause or fail based on abort reason.
			if execInfo.AbortReason == "manual abort" || execInfo.AbortReason == "" {
				if err := o.taskOrchestrator.PauseTask(ctx, task.ID); err != nil {
					o.logger.Warn("chain_closure: failed to pause task", zap.Error(err))
					continue
				}
				_ = o.taskOrchestrator.SetOutcome(ctx, task.ID, "aborted", execInfo.AbortReason, task.AttemptCount+1)
				o.recordFeedback(ctx, task, execInfo, "aborted", false, "execution_aborted")
				paused++
			} else {
				if err := o.taskOrchestrator.FailTask(ctx, task.ID, execInfo.AbortReason); err != nil {
					o.logger.Warn("chain_closure: failed to fail task", zap.Error(err))
					continue
				}
				signal := "execution_aborted"
				if contains(execInfo.AbortReason, "objective penalty") {
					signal = "objective_penalty_abort"
				} else if contains(execInfo.AbortReason, "consecutive failures") {
					signal = "repeated_failure"
				} else if contains(execInfo.AbortReason, "governance") {
					signal = "blocked_by_governance"
				}
				_ = o.taskOrchestrator.SetOutcome(ctx, task.ID, "aborted", execInfo.AbortReason, task.AttemptCount+1)
				o.recordFeedback(ctx, task, execInfo, "aborted", false, signal)
				failed++
			}

			o.auditEvent(ctx, "task.execution_aborted", map[string]any{
				"task_id":      task.ID,
				"exec_task_id": task.ExecutionTaskID,
				"reason":       execInfo.AbortReason,
			})

		case "pending", "running":
			// Still in progress — check for review blocks.
			if execInfo.HasReviewBlock {
				if err := o.taskOrchestrator.PauseTask(ctx, task.ID); err != nil {
					o.logger.Warn("chain_closure: failed to pause task for review", zap.Error(err))
					continue
				}
				o.recordFeedback(ctx, task, execInfo, "blocked", false, "blocked_by_review")
				paused++

				o.auditEvent(ctx, "task.execution_paused", map[string]any{
					"task_id":      task.ID,
					"exec_task_id": task.ExecutionTaskID,
					"reason":       "review_required",
				})
			}
			// Otherwise, still running — do nothing.
		}
	}

	return completed, failed, paused, nil
}

// recordFeedback persists structured execution feedback.
func (o *Orchestrator) recordFeedback(ctx context.Context, task RunningTaskInfo, exec ExecTaskInfo, outcomeType string, success bool, signal string) {
	if o.feedbackStore == nil {
		return
	}

	fb := ExecutionFeedback{
		ID:              generateID(),
		TaskID:          task.ID,
		ExecutionTaskID: exec.ID,
		OutcomeType:     outcomeType,
		Success:         success,
		StepsExecuted:   exec.StepsExecuted,
		StepsFailed:     exec.StepsFailed,
		ErrorSummary:    exec.AbortReason,
		SemanticSignal:  signal,
		CreatedAt:       nowUTCOrch(),
	}

	if err := o.feedbackStore.Insert(ctx, fb); err != nil {
		o.logger.Warn("chain_closure: failed to store feedback", zap.Error(err))
		return
	}

	o.state.mu.Lock()
	o.state.FeedbackRecorded++
	o.state.mu.Unlock()

	o.auditEvent(ctx, "execution.feedback_recorded", map[string]any{
		"task_id":         task.ID,
		"exec_task_id":    exec.ID,
		"outcome":         outcomeType,
		"semantic_signal": signal,
	})
}

// --- Feedback adapters for upstream subsystems ---

// GetReflectionFeedback returns execution feedback signals suitable for reflection consumption.
func (o *Orchestrator) GetReflectionFeedback(ctx context.Context) ([]ReflectionFeedbackSignal, error) {
	if o.feedbackStore == nil {
		return nil, nil
	}

	since := nowUTCOrch().Add(-24 * time.Hour)
	recent, err := o.feedbackStore.ListRecent(ctx, 50)
	if err != nil {
		return nil, fmt.Errorf("list recent feedback: %w", err)
	}

	var signals []ReflectionFeedbackSignal
	for _, fb := range recent {
		if fb.CreatedAt.Before(since) {
			continue
		}
		signals = append(signals, ReflectionFeedbackSignal{
			Signal:    fb.SemanticSignal,
			Outcome:   fb.OutcomeType,
			Success:   fb.Success,
			TaskID:    fb.TaskID,
			CreatedAt: fb.CreatedAt,
		})
	}

	if len(signals) > 0 {
		o.auditEvent(ctx, "execution.feedback_exposed_to_reflection", map[string]any{
			"signal_count": len(signals),
		})
	}

	return signals, nil
}

// GetObjectiveFeedback returns execution health metrics suitable for objective consumption.
func (o *Orchestrator) GetObjectiveFeedback(ctx context.Context) (ObjectiveFeedbackMetrics, error) {
	if o.feedbackStore == nil {
		return ObjectiveFeedbackMetrics{}, nil
	}

	since := nowUTCOrch().Add(-24 * time.Hour)

	totalCompleted, _ := o.feedbackStore.CountByOutcome(ctx, "completed", since)
	totalFailed, _ := o.feedbackStore.CountByOutcome(ctx, "failed", since)
	totalAborted, _ := o.feedbackStore.CountByOutcome(ctx, "aborted", since)
	totalBlocked, _ := o.feedbackStore.CountByOutcome(ctx, "blocked", since)
	repeatedFails, _ := o.feedbackStore.CountBySignal(ctx, "repeated_failure", since)

	total := totalCompleted + totalFailed + totalAborted + totalBlocked
	successRate := 0.0
	if total > 0 {
		successRate = float64(totalCompleted) / float64(total)
	}

	metrics := ObjectiveFeedbackMetrics{
		SuccessRate:      successRate,
		CompletedCount:   totalCompleted,
		FailedCount:      totalFailed,
		AbortedCount:     totalAborted,
		BlockedCount:     totalBlocked,
		RepeatedFailures: repeatedFails,
		TotalExecutions:  total,
		MeasuredSince:    since,
	}

	o.auditEvent(ctx, "execution.feedback_exposed_to_objective", map[string]any{
		"success_rate":     metrics.SuccessRate,
		"total_executions": metrics.TotalExecutions,
	})

	return metrics, nil
}

// --- Feedback result types ---

// ReflectionFeedbackSignal is a semantic execution signal for reflection consumption.
type ReflectionFeedbackSignal struct {
	Signal    string    `json:"signal"`
	Outcome   string    `json:"outcome"`
	Success   bool      `json:"success"`
	TaskID    string    `json:"task_id"`
	CreatedAt time.Time `json:"created_at"`
}

// ObjectiveFeedbackMetrics provides aggregated execution health for objective consumption.
type ObjectiveFeedbackMetrics struct {
	SuccessRate      float64   `json:"success_rate"`
	CompletedCount   int       `json:"completed_count"`
	FailedCount      int       `json:"failed_count"`
	AbortedCount     int       `json:"aborted_count"`
	BlockedCount     int       `json:"blocked_count"`
	RepeatedFailures int       `json:"repeated_failures"`
	TotalExecutions  int       `json:"total_executions"`
	MeasuredSince    time.Time `json:"measured_since"`
}

// MaxRunningTasks mirrors the task orchestrator constant for feedback queries.
const MaxRunningTasks = 4

// --- Helpers ---

func clamp01Orch(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchSubstring(s, substr)))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func generateID() string {
	return fmt.Sprintf("%d", nowUTCOrch().UnixNano())
}

var nowUTCOrch = func() time.Time { return time.Now().UTC() }
