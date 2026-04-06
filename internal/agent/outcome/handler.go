package outcome

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actions"
	"github.com/tiroq/arcanum/internal/audit"
)

// Handler implements actions.OutcomeHandler by evaluating, persisting,
// and auditing the real-world outcome of each executed action.
type Handler struct {
	evaluator *DBEvaluator
	store     *Store
	auditor   audit.AuditRecorder
	logger    *zap.Logger
}

// NewHandler creates an OutcomeHandler.
func NewHandler(evaluator *DBEvaluator, store *Store, auditor audit.AuditRecorder, logger *zap.Logger) *Handler {
	return &Handler{
		evaluator: evaluator,
		store:     store,
		auditor:   auditor,
		logger:    logger,
	}
}

// HandleOutcome evaluates the action's real-world impact, persists the
// outcome, and emits an audit event.
func (h *Handler) HandleOutcome(ctx context.Context, action actions.Action, result actions.ActionResult) error {
	o, err := h.evaluator.Evaluate(ctx, action, result)
	if err != nil {
		return fmt.Errorf("evaluate outcome: %w", err)
	}

	if err := h.store.Save(ctx, o); err != nil {
		return fmt.Errorf("persist outcome: %w", err)
	}

	// Audit: action.outcome_evaluated
	if h.auditor != nil {
		entityID, parseErr := uuid.Parse(action.ID)
		if parseErr != nil {
			entityID = uuid.New()
		}
		_ = h.auditor.RecordEvent(ctx, "action", entityID, "action.outcome_evaluated", "system", "action_engine", map[string]any{
			"action_id":       action.ID,
			"outcome_id":      o.ID.String(),
			"outcome_status":  string(o.OutcomeStatus),
			"effect_detected": o.EffectDetected,
			"improvement":     o.Improvement,
		})
	}

	h.logger.Info("outcome_evaluated",
		zap.String("action_id", action.ID),
		zap.String("outcome_status", string(o.OutcomeStatus)),
		zap.Bool("effect_detected", o.EffectDetected),
		zap.Bool("improvement", o.Improvement),
	)

	return nil
}
