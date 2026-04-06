package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/goals"
	"github.com/tiroq/arcanum/internal/audit"
)

// OutcomeHandler is called after each executed action to evaluate and persist
// the real-world outcome. Implemented by the outcome verification layer.
type OutcomeHandler interface {
	HandleOutcome(ctx context.Context, action Action, result ActionResult) error
}

// Engine is the top-level action engine. It ties together the planner,
// guardrails, and executor into a single RunCycle operation.
type Engine struct {
	goalEngine     *goals.GoalEngine
	planner        *Planner
	guardrails     *Guardrails
	executor       *Executor
	auditor        audit.AuditRecorder
	outcomeHandler OutcomeHandler
	logger         *zap.Logger
}

// NewEngine creates an Engine.
func NewEngine(
	goalEngine *goals.GoalEngine,
	planner *Planner,
	guardrails *Guardrails,
	executor *Executor,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		goalEngine: goalEngine,
		planner:    planner,
		guardrails: guardrails,
		executor:   executor,
		auditor:    auditor,
		logger:     logger,
	}
}

// WithOutcomeVerification attaches an OutcomeHandler to the engine.
// When set, every executed action will be evaluated for real-world impact.
func (e *Engine) WithOutcomeVerification(handler OutcomeHandler) *Engine {
	e.outcomeHandler = handler
	return e
}

// RunCycle performs one complete action cycle:
//  1. Evaluate goals
//  2. Plan actions
//  3. Filter through guardrails
//  4. Execute safe actions
//  5. Audit every decision
func (e *Engine) RunCycle(ctx context.Context) (*CycleReport, error) {
	cycleID := uuid.New().String()
	e.logger.Info("action_cycle_start", zap.String("cycle_id", cycleID))

	// 1. Fetch goals.
	goalList, err := e.goalEngine.Evaluate(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate goals: %w", err)
	}
	e.logger.Info("action_cycle_goals",
		zap.String("cycle_id", cycleID),
		zap.Int("goal_count", len(goalList)),
	)

	if len(goalList) == 0 {
		e.logger.Info("action_cycle_no_goals", zap.String("cycle_id", cycleID))
		return &CycleReport{
			CycleID:   cycleID,
			Timestamp: time.Now().UTC(),
		}, nil
	}

	// 2. Plan actions.
	planned, err := e.planner.PlanActions(ctx, goalList)
	if err != nil {
		return nil, fmt.Errorf("plan actions: %w", err)
	}
	e.logger.Info("action_cycle_planned",
		zap.String("cycle_id", cycleID),
		zap.Int("action_count", len(planned)),
	)

	report := &CycleReport{
		CycleID:   cycleID,
		Planned:   planned,
		Timestamp: time.Now().UTC(),
	}

	// Emit audit events for all planned actions.
	for _, a := range planned {
		e.auditAction(ctx, a, "action.planned", "", "")
	}

	// 3. Filter through guardrails.
	var safe []Action
	for _, a := range planned {
		ok, reason := e.guardrails.EvaluateSafety(ctx, a)
		if !ok {
			e.logger.Info("action_rejected",
				zap.String("cycle_id", cycleID),
				zap.String("action_id", a.ID),
				zap.String("type", a.Type),
				zap.String("reason", reason),
			)
			e.auditAction(ctx, a, "action.rejected", reason, "")
			report.Rejected = append(report.Rejected, ActionResult{
				ActionID: a.ID,
				Status:   StatusRejected,
				Reason:   reason,
			})
			continue
		}
		safe = append(safe, a)
	}

	// 4. Execute safe actions.
	for _, a := range safe {
		result := e.executor.ExecuteAction(ctx, a)
		e.guardrails.RecordExecution(a)

		switch result.Status {
		case StatusExecuted:
			e.auditAction(ctx, a, "action.executed", "", "")
			report.Executed = append(report.Executed, result)

			// 5. Evaluate outcome (non-invasive, best-effort).
			e.evaluateOutcome(ctx, a, result)
		case StatusFailed:
			e.auditAction(ctx, a, "action.failed", "", result.Error)
			report.Failed = append(report.Failed, result)
		}
	}

	e.logger.Info("action_cycle_complete",
		zap.String("cycle_id", cycleID),
		zap.Int("planned", len(planned)),
		zap.Int("rejected", len(report.Rejected)),
		zap.Int("executed", len(report.Executed)),
		zap.Int("failed", len(report.Failed)),
	)

	return report, nil
}

// evaluateOutcome runs the outcome handler and audits the result.
// This is a best-effort, non-invasive post-execution step.
func (e *Engine) evaluateOutcome(ctx context.Context, a Action, result ActionResult) {
	if e.outcomeHandler == nil {
		return
	}

	if err := e.outcomeHandler.HandleOutcome(ctx, a, result); err != nil {
		e.logger.Warn("outcome_evaluation_failed",
			zap.String("action_id", a.ID),
			zap.Error(err),
		)
	}
}

// auditAction records an audit event for an action lifecycle step.
func (e *Engine) auditAction(ctx context.Context, a Action, eventType, reason, errMsg string) {
	if e.auditor == nil {
		return
	}

	entityID, err := uuid.Parse(a.ID)
	if err != nil {
		// Action IDs are always UUIDs but defend against misuse.
		entityID = uuid.New()
	}

	payload := map[string]any{
		"action_id":   a.ID,
		"goal_id":     a.GoalID,
		"action_type": a.Type,
		"params":      a.Params,
		"priority":    a.Priority,
		"confidence":  a.Confidence,
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}

	if auditErr := e.auditor.RecordEvent(
		ctx,
		"action",
		entityID,
		eventType,
		"system",
		"action_engine",
		payload,
	); auditErr != nil {
		e.logger.Warn("audit_action_failed",
			zap.String("action_id", a.ID),
			zap.String("event_type", eventType),
			zap.Error(auditErr),
		)
	}
}
