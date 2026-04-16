package vector

import (
	"context"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/audit"
	"go.uber.org/zap"
)

// Engine manages the system vector lifecycle.
type Engine struct {
	store   StoreInterface
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewEngine creates a new vector engine.
func NewEngine(store StoreInterface, auditor audit.AuditRecorder, logger *zap.Logger) *Engine {
	return &Engine{store: store, auditor: auditor, logger: logger}
}

// Get returns the current system vector.
func (e *Engine) Get(ctx context.Context) (SystemVector, error) {
	return e.store.Get(ctx)
}

// Set updates the system vector and emits an audit event.
func (e *Engine) Set(ctx context.Context, v SystemVector) error {
	v.Clamp()
	if err := e.store.Set(ctx, v); err != nil {
		return err
	}

	if e.auditor != nil {
		e.auditor.RecordEvent(ctx, "system_vector", uuid.Nil, "vector.updated", "owner", "telegram", map[string]interface{}{
			"income_priority":         v.IncomePriority,
			"family_safety_priority":  v.FamilySafetyPriority,
			"infra_priority":          v.InfraPriority,
			"automation_priority":     v.AutomationPriority,
			"exploration_level":       v.ExplorationLevel,
			"risk_tolerance":          v.RiskTolerance,
			"human_review_strictness": v.HumanReviewStrictness,
		})
	}

	e.logger.Info("system vector updated",
		zap.Float64("income_priority", v.IncomePriority),
		zap.Float64("family_safety_priority", v.FamilySafetyPriority),
		zap.Float64("risk_tolerance", v.RiskTolerance),
		zap.Float64("human_review_strictness", v.HumanReviewStrictness),
	)
	return nil
}

// GetVector implements VectorProvider for use by other subsystems.
func (e *Engine) GetVector() SystemVector {
	v, _ := e.store.Get(context.Background())
	return v
}
