package outcome

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actions"
)

// Evaluator assesses the real-world impact of executed actions by reading
// system state after execution and comparing it to expectations.
type Evaluator interface {
	Evaluate(ctx context.Context, action actions.Action, result actions.ActionResult) (*ActionOutcome, error)
}

// DBEvaluator implements Evaluator by querying the database for post-action state.
type DBEvaluator struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

// NewEvaluator creates a DBEvaluator.
func NewEvaluator(db *pgxpool.Pool, logger *zap.Logger) *DBEvaluator {
	return &DBEvaluator{db: db, logger: logger}
}

// Evaluate inspects system state after an action and produces a deterministic ActionOutcome.
func (e *DBEvaluator) Evaluate(ctx context.Context, action actions.Action, result actions.ActionResult) (*ActionOutcome, error) {
	switch action.Type {
	case string(actions.ActionRetryJob):
		return e.evaluateRetryJob(ctx, action, result)
	case string(actions.ActionTriggerResync):
		return e.evaluateResync(ctx, action, result)
	case string(actions.ActionLogRecommendation):
		return e.evaluateRecommendation(action), nil
	default:
		return e.evaluateRecommendation(action), nil
	}
}

// evaluateRetryJob checks the actual job status after a retry action.
func (e *DBEvaluator) evaluateRetryJob(ctx context.Context, action actions.Action, result actions.ActionResult) (*ActionOutcome, error) {
	jobIDStr, _ := action.Params["job_id"].(string)
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse job_id param: %w", err)
	}

	// Read current job state (AFTER action execution).
	var status string
	var attemptCount int
	const q = `SELECT status, attempt_count FROM processing_jobs WHERE id = $1`
	if err := e.db.QueryRow(ctx, q, jobID).Scan(&status, &attemptCount); err != nil {
		return nil, fmt.Errorf("query job %s: %w", jobID, err)
	}

	afterState := map[string]any{
		"status":        status,
		"attempt_count": attemptCount,
	}

	// Construct before_state from action context — the planner only targets
	// retry_scheduled or dead_letter jobs, so we capture that intent.
	beforeState := map[string]any{
		"action_triggered": "retry",
	}

	o := &ActionOutcome{
		ID:          uuid.New(),
		ActionID:    mustParseUUID(action.ID),
		GoalID:      action.GoalID,
		ActionType:  action.Type,
		TargetType:  "job",
		TargetID:    jobID,
		BeforeState: beforeState,
		AfterState:  afterState,
		EvaluatedAt: time.Now().UTC(),
	}

	switch status {
	case "succeeded":
		o.OutcomeStatus = OutcomeSuccess
		o.EffectDetected = true
		o.Improvement = true
	case "dead_letter":
		o.OutcomeStatus = OutcomeFailure
		o.EffectDetected = true
		o.Improvement = false
	case "retry_scheduled":
		o.OutcomeStatus = OutcomeNeutral
		o.EffectDetected = true
		o.Improvement = false
	default:
		// Status unchanged or transitional — no meaningful effect yet.
		o.OutcomeStatus = OutcomeNeutral
		o.EffectDetected = false
		o.Improvement = false
	}

	return o, nil
}

// evaluateResync checks whether a resync action produced new jobs.
func (e *DBEvaluator) evaluateResync(ctx context.Context, action actions.Action, result actions.ActionResult) (*ActionOutcome, error) {
	taskIDStr, _ := action.Params["source_task_id"].(string)
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse source_task_id param: %w", err)
	}

	// Check if any jobs were created for this task since the action was created.
	var jobCount int
	const q = `SELECT COUNT(*) FROM processing_jobs WHERE source_task_id = $1 AND created_at >= $2`
	if err := e.db.QueryRow(ctx, q, taskID, action.CreatedAt).Scan(&jobCount); err != nil {
		return nil, fmt.Errorf("count new jobs for task %s: %w", taskID, err)
	}

	afterState := map[string]any{
		"new_jobs_created": jobCount,
	}
	beforeState := map[string]any{
		"action_triggered": "resync",
	}

	o := &ActionOutcome{
		ID:          uuid.New(),
		ActionID:    mustParseUUID(action.ID),
		GoalID:      action.GoalID,
		ActionType:  action.Type,
		TargetType:  "task",
		TargetID:    taskID,
		BeforeState: beforeState,
		AfterState:  afterState,
		EvaluatedAt: time.Now().UTC(),
	}

	if jobCount > 0 {
		o.OutcomeStatus = OutcomeSuccess
		o.EffectDetected = true
		o.Improvement = true
	} else {
		o.OutcomeStatus = OutcomeNeutral
		o.EffectDetected = false
		o.Improvement = false
	}

	return o, nil
}

// evaluateRecommendation always produces a neutral outcome for log-only actions.
func (e *DBEvaluator) evaluateRecommendation(action actions.Action) *ActionOutcome {
	return &ActionOutcome{
		ID:             uuid.New(),
		ActionID:       mustParseUUID(action.ID),
		GoalID:         action.GoalID,
		ActionType:     action.Type,
		TargetType:     "recommendation",
		TargetID:       uuid.Nil,
		OutcomeStatus:  OutcomeNeutral,
		BeforeState:    map[string]any{},
		AfterState:     map[string]any{},
		EffectDetected: false,
		Improvement:    false,
		EvaluatedAt:    time.Now().UTC(),
	}
}

func mustParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.New()
	}
	return id
}
