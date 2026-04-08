package pathcomparison

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Evaluator evaluates comparative outcomes by comparing selected path results
// against the decision snapshot's alternatives.
type Evaluator struct {
	snapshotStore *SnapshotStore
	outcomeStore  *OutcomeStore
	memoryStore   *MemoryStore
	auditor       audit.AuditRecorder
	logger        *zap.Logger
}

// NewEvaluator creates a comparative path evaluator.
func NewEvaluator(
	snapshotStore *SnapshotStore,
	outcomeStore *OutcomeStore,
	memoryStore *MemoryStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Evaluator {
	return &Evaluator{
		snapshotStore: snapshotStore,
		outcomeStore:  outcomeStore,
		memoryStore:   memoryStore,
		auditor:       auditor,
		logger:        logger,
	}
}

// EvaluateComparison evaluates a path selection decision after the outcome is known.
//
// Parameters:
//   - decisionID: unique identifier linking to the snapshot
//   - selectedOutcome: the outcome of the selected path (success|neutral|failure)
func (e *Evaluator) EvaluateComparison(ctx context.Context, decisionID string, selectedOutcome string) error {
	// 1. Retrieve the snapshot for this decision.
	snapshot, err := e.snapshotStore.GetSnapshot(ctx, decisionID)
	if err != nil {
		e.logger.Error("comparative_snapshot_query_failed",
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
		return err
	}
	if snapshot == nil {
		// No snapshot → nothing to compare. Fail-open.
		return nil
	}

	// 2. Classify ranking errors.
	rankingError, overestimated, underestimated := ClassifyRankingError(
		snapshot.SelectedScore, selectedOutcome,
	)

	// 3. Detect better alternative.
	betterAltExists := DetectBetterAlternative(*snapshot, selectedOutcome)

	// 4. Build comparative outcome.
	outcome := ComparativeOutcome{
		DecisionID:              decisionID,
		GoalType:                snapshot.GoalType,
		SelectedPathSignature:   snapshot.SelectedPathSignature,
		SelectedOutcome:         selectedOutcome,
		RankingError:            rankingError,
		Overestimated:           overestimated,
		Underestimated:          underestimated,
		BetterAlternativeExists: betterAltExists,
		CreatedAt:               time.Now().UTC(),
	}

	// 5. Persist the comparative outcome.
	if err := e.outcomeStore.SaveOutcome(ctx, outcome); err != nil {
		e.logger.Error("comparative_outcome_save_failed",
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
		return err
	}

	// 6. Classify win/loss for the selected path.
	win, loss := ClassifyWinLoss(selectedOutcome, betterAltExists)

	// 7. Update comparative memory for the selected path.
	if err := e.memoryStore.RecordSelection(ctx, snapshot.SelectedPathSignature, snapshot.GoalType, win, loss); err != nil {
		e.logger.Error("comparative_memory_update_failed",
			zap.String("path_signature", snapshot.SelectedPathSignature),
			zap.Error(err),
		)
	}

	// 8. If better alternative exists, record missed wins for close alternatives.
	if betterAltExists {
		for _, c := range snapshot.Candidates {
			if c.PathSignature == snapshot.SelectedPathSignature {
				continue
			}
			scoreDiff := snapshot.SelectedScore - c.Score
			if scoreDiff < AlternativeScoreThreshold {
				if err := e.memoryStore.RecordMissedWin(ctx, c.PathSignature, snapshot.GoalType); err != nil {
					e.logger.Error("comparative_missed_win_update_failed",
						zap.String("path_signature", c.PathSignature),
						zap.Error(err),
					)
				}
			}
		}
	}

	// 9. Audit the comparison evaluation.
	e.auditEvent(ctx, "path.comparison_evaluated", map[string]any{
		"decision_id":               decisionID,
		"goal_type":                 snapshot.GoalType,
		"selected_path":             snapshot.SelectedPathSignature,
		"selected_outcome":          selectedOutcome,
		"ranking_error":             rankingError,
		"overestimated":             overestimated,
		"underestimated":            underestimated,
		"better_alternative_exists": betterAltExists,
		"win":                       win,
		"loss":                      loss,
	})

	// 10. Generate and audit comparative feedback.
	record, err := e.memoryStore.GetMemory(ctx, snapshot.SelectedPathSignature, snapshot.GoalType)
	if err == nil && record != nil {
		fb := GenerateComparativeFeedback(record)
		if fb.Recommendation != ComparativeNeutral {
			e.auditEvent(ctx, "path.comparison_feedback_generated", map[string]any{
				"path_signature": snapshot.SelectedPathSignature,
				"goal_type":      snapshot.GoalType,
				"recommendation": string(fb.Recommendation),
				"win_rate":       fb.WinRate,
				"loss_rate":      fb.LossRate,
				"missed_wins":    fb.MissedWinCount,
				"sample_size":    fb.SelectionCount,
			})
		}
	}

	return nil
}

// auditEvent records a comparative learning audit event.
func (e *Evaluator) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "path_comparison", uuid.New(), eventType,
		"system", "path_comparison_engine", payload)
}
