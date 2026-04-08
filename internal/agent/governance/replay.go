package governance

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// ReplayPackBuilder assembles decision replay packs and persists them.
// Replay packs capture the full decision context for post-hoc review.
type ReplayPackBuilder struct {
	store   *ReplayStore
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewReplayPackBuilder creates a ReplayPackBuilder.
func NewReplayPackBuilder(
	store *ReplayStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *ReplayPackBuilder {
	return &ReplayPackBuilder{
		store:   store,
		auditor: auditor,
		logger:  logger,
	}
}

// RecordReplayPack persists a replay pack for a decision.
// Called by the decision graph adapter after path selection.
// Best-effort: failures are logged but do not block execution.
func (b *ReplayPackBuilder) RecordReplayPack(ctx context.Context, rp ReplayPack) {
	if b.store == nil {
		return
	}
	if rp.CreatedAt.IsZero() {
		rp.CreatedAt = time.Now().UTC()
	}
	if rp.Signals == nil {
		rp.Signals = map[string]any{}
	}
	if rp.ArbitrationTrace == nil {
		rp.ArbitrationTrace = map[string]any{}
	}
	if rp.CalibrationInfo == nil {
		rp.CalibrationInfo = map[string]any{}
	}
	if rp.ComparativeInfo == nil {
		rp.ComparativeInfo = map[string]any{}
	}
	if rp.CounterfactualInfo == nil {
		rp.CounterfactualInfo = map[string]any{}
	}

	if err := b.store.Save(ctx, rp); err != nil {
		b.logger.Warn("replay_pack_save_failed",
			zap.String("decision_id", rp.DecisionID),
			zap.Error(err),
		)
		return
	}

	b.logger.Info("replay_pack_recorded",
		zap.String("decision_id", rp.DecisionID),
		zap.String("goal_type", rp.GoalType),
		zap.String("selected_mode", rp.SelectedMode),
	)
}

// GetReplayPack retrieves a replay pack by decision ID.
// Returns nil if not found.
func (b *ReplayPackBuilder) GetReplayPack(ctx context.Context, decisionID string) (*ReplayPack, error) {
	if b.store == nil {
		return nil, nil
	}

	rp, err := b.store.GetByDecisionID(ctx, decisionID)
	if err != nil {
		return nil, err
	}

	// Emit audit event for replay request.
	if b.auditor != nil {
		_ = b.auditor.RecordEvent(ctx, "governance", uuid.New(), "governance.replay_requested",
			"operator", "governance_controller", map[string]any{
				"decision_id": decisionID,
			})
	}

	return rp, nil
}
