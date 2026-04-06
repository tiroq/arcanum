package outcome

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/actions"
	"github.com/tiroq/arcanum/internal/audit"
)

// Handler implements actions.OutcomeHandler by evaluating, persisting,
// and auditing the real-world outcome of each executed action.
type Handler struct {
	evaluator   *DBEvaluator
	store       *Store
	memoryStore *actionmemory.Store
	auditor     audit.AuditRecorder
	logger      *zap.Logger
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

// WithMemoryStore attaches an action memory store for learning accumulation.
func (h *Handler) WithMemoryStore(ms *actionmemory.Store) *Handler {
	h.memoryStore = ms
	return h
}

// HandleOutcome evaluates the action's real-world impact, persists the
// outcome, updates action memory, and emits audit events.
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

	// Update action memory (best-effort).
	if h.memoryStore != nil {
		memInput := actionmemory.OutcomeInput{
			ActionType:    o.ActionType,
			TargetType:    o.TargetType,
			TargetID:      o.TargetID,
			OutcomeStatus: string(o.OutcomeStatus),
		}
		if memErr := h.memoryStore.Update(ctx, memInput); memErr != nil {
			h.logger.Warn("action_memory_update_failed",
				zap.String("action_id", action.ID),
				zap.Error(memErr),
			)
		} else {
			// Generate and audit feedback signal.
			record, _ := h.memoryStore.GetByActionType(ctx, o.ActionType)
			fb := actionmemory.GenerateFeedback(record)
			h.auditFeedback(ctx, action, fb)
		}
	}

	return nil
}

// auditFeedback emits an action.feedback_generated audit event.
func (h *Handler) auditFeedback(ctx context.Context, action actions.Action, fb actionmemory.ActionFeedback) {
	if h.auditor == nil {
		return
	}

	entityID, err := uuid.Parse(action.ID)
	if err != nil {
		entityID = uuid.New()
	}

	_ = h.auditor.RecordEvent(ctx, "action", entityID, "action.feedback_generated", "system", "action_engine", map[string]any{
		"action_type":    fb.ActionType,
		"success_rate":   fb.SuccessRate,
		"failure_rate":   fb.FailureRate,
		"sample_size":    fb.SampleSize,
		"recommendation": string(fb.Recommendation),
	})

	h.logger.Info("feedback_generated",
		zap.String("action_type", fb.ActionType),
		zap.Float64("success_rate", fb.SuccessRate),
		zap.Float64("failure_rate", fb.FailureRate),
		zap.String("recommendation", string(fb.Recommendation)),
	)
}
