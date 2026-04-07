package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/goals"
)

// Planner converts advisory goals into concrete, executable actions.
// It is deterministic: the same set of goals always produces the same actions.
type Planner struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

// NewPlanner creates a Planner.
func NewPlanner(db *pgxpool.Pool, logger *zap.Logger) *Planner {
	return &Planner{db: db, logger: logger}
}

// PlanActions maps each goal to zero or more concrete actions.
func (p *Planner) PlanActions(ctx context.Context, gs []goals.Goal) ([]Action, error) {
	var actions []Action
	for _, g := range gs {
		planned, err := p.planForGoal(ctx, g)
		if err != nil {
			p.logger.Warn("plan_for_goal_failed",
				zap.String("goal_id", g.ID),
				zap.String("goal_type", g.Type),
				zap.Error(err),
			)
			continue
		}
		actions = append(actions, planned...)
	}
	return actions, nil
}

func (p *Planner) planForGoal(ctx context.Context, g goals.Goal) ([]Action, error) {
	switch g.Type {
	case string(goals.GoalReduceRetryRate), string(goals.GoalInvestigateFailures):
		return p.planRetryActions(ctx, g)
	case string(goals.GoalResolveBacklog):
		return p.planResyncActions(ctx, g)
	default:
		// For goals without a concrete action, emit a log recommendation.
		return []Action{p.logRecommendation(g)}, nil
	}
}

// planRetryActions finds jobs in retry_scheduled or dead_letter that can be retried.
func (p *Planner) planRetryActions(ctx context.Context, g goals.Goal) ([]Action, error) {
	const query = `
		SELECT id FROM processing_jobs
		WHERE status IN ('retry_scheduled', 'dead_letter')
		  AND attempt_count < max_attempts
		ORDER BY priority DESC, created_at ASC
		LIMIT 10`

	rows, err := p.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query retryable jobs: %w", err)
	}
	defer rows.Close()

	var actions []Action
	for rows.Next() {
		var jobID uuid.UUID
		if err := rows.Scan(&jobID); err != nil {
			continue
		}
		actions = append(actions, Action{
			ID:          uuid.New().String(),
			Type:        string(ActionRetryJob),
			Priority:    g.Priority,
			Confidence:  g.Confidence,
			GoalID:      g.ID,
			Description: fmt.Sprintf("Retry job %s (goal: %s)", jobID, g.Type),
			Params: map[string]any{
				"job_id": jobID.String(),
			},
			Safe:      true,
			CreatedAt: time.Now().UTC(),
		})
	}
	return actions, rows.Err()
}

// planResyncActions finds source tasks that could benefit from a resync.
func (p *Planner) planResyncActions(ctx context.Context, g goals.Goal) ([]Action, error) {
	// Find source tasks with queued backlog but no recent resync.
	const query = `
		SELECT DISTINCT st.id
		FROM source_tasks st
		JOIN processing_jobs pj ON pj.source_task_id = st.id
		WHERE pj.status IN ('queued', 'retry_scheduled')
		ORDER BY st.id
		LIMIT 5`

	rows, err := p.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query resync candidates: %w", err)
	}
	defer rows.Close()

	var actions []Action
	for rows.Next() {
		var taskID uuid.UUID
		if err := rows.Scan(&taskID); err != nil {
			continue
		}
		actions = append(actions, Action{
			ID:          uuid.New().String(),
			Type:        string(ActionTriggerResync),
			Priority:    g.Priority,
			Confidence:  g.Confidence,
			GoalID:      g.ID,
			Description: fmt.Sprintf("Trigger resync for task %s (goal: %s)", taskID, g.Type),
			Params: map[string]any{
				"source_task_id": taskID.String(),
			},
			Safe:      true,
			CreatedAt: time.Now().UTC(),
		})
	}
	return actions, rows.Err()
}

// logRecommendation creates a no-op action that only emits an audit event.
func (p *Planner) logRecommendation(g goals.Goal) Action {
	return Action{
		ID:          uuid.New().String(),
		Type:        string(ActionLogRecommendation),
		Priority:    g.Priority,
		Confidence:  g.Confidence,
		GoalID:      g.ID,
		Description: fmt.Sprintf("Recommendation: %s (goal: %s)", g.Description, g.Type),
		Params: map[string]any{
			"goal_type":   g.Type,
			"description": g.Description,
		},
		Safe:      true,
		CreatedAt: time.Now().UTC(),
	}
}

// FindRetryTargets returns concrete retry_job actions for retryable jobs.
// Exported for use by the adaptive planning layer.
func (p *Planner) FindRetryTargets(ctx context.Context, g goals.Goal) ([]Action, error) {
	return p.planRetryActions(ctx, g)
}

// FindResyncTargets returns concrete trigger_resync actions for eligible source tasks.
// Exported for use by the adaptive planning layer.
func (p *Planner) FindResyncTargets(ctx context.Context, g goals.Goal) ([]Action, error) {
	return p.planResyncActions(ctx, g)
}
