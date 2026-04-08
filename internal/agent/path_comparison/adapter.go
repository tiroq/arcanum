package pathcomparison

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	decision_graph "github.com/tiroq/arcanum/internal/agent/decision_graph"
	"github.com/tiroq/arcanum/internal/audit"
)

// ComparativeLearningProvider is the interface used by the decision graph layer
// to retrieve comparative feedback without importing this package directly.
type ComparativeLearningProvider interface {
	// GetAllComparativeFeedbackMap returns comparative feedback as
	// map[pathSignature] → recommendation string for a given goal type.
	GetAllComparativeFeedbackMap(ctx context.Context, goalType string) map[string]string
}

// GraphAdapter adapts the comparative learning layer to the decision graph.
// Implements ComparativeLearningProvider for use by the graph planner adapter.
type GraphAdapter struct {
	memoryStore *MemoryStore
	logger      *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter.
func NewGraphAdapter(memoryStore *MemoryStore, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{
		memoryStore: memoryStore,
		logger:      logger,
	}
}

// GetAllComparativeFeedbackMap returns comparative feedback as a map[pathSignature] → recommendation string.
// Fail-open: returns empty map if data is unavailable.
func (a *GraphAdapter) GetAllComparativeFeedbackMap(ctx context.Context, goalType string) map[string]string {
	result := make(map[string]string)

	if a.memoryStore == nil {
		return result
	}

	records, err := a.memoryStore.ListMemoryByGoalType(ctx, goalType)
	if err != nil {
		a.logger.Warn("comparative_feedback_list_failed",
			zap.String("goal_type", goalType),
			zap.Error(err),
		)
		return result
	}

	for i := range records {
		fb := GenerateComparativeFeedback(&records[i])
		result[records[i].PathSignature] = string(fb.Recommendation)
	}

	return result
}

// --- Snapshot Capturer ---

// SnapshotCapturerAdapter implements decision_graph.SnapshotCapturer.
// Bridges decision graph's ScoredPathExport to local ScoredPathInfo types.
type SnapshotCapturerAdapter struct {
	snapshotStore *SnapshotStore
	auditor       audit.AuditRecorder
	logger        *zap.Logger
}

// NewSnapshotCapturerAdapter creates a SnapshotCapturerAdapter.
func NewSnapshotCapturerAdapter(
	snapshotStore *SnapshotStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *SnapshotCapturerAdapter {
	return &SnapshotCapturerAdapter{
		snapshotStore: snapshotStore,
		auditor:       auditor,
		logger:        logger,
	}
}

// CaptureAndSave captures a decision snapshot and persists it.
// Satisfies decision_graph.SnapshotCapturer interface.
func (a *SnapshotCapturerAdapter) CaptureAndSave(ctx context.Context, decisionID, goalType string, scoredPaths []decision_graph.ScoredPathExport, selectedSignature string, selectedScore float64) error {
	paths := make([]ScoredPathInfo, len(scoredPaths))
	for i, sp := range scoredPaths {
		paths[i] = ScoredPathInfo{PathSignature: sp.PathSignature, Score: sp.Score}
	}

	snap := CaptureSnapshot(decisionID, goalType, paths, selectedSignature, selectedScore)

	if err := a.snapshotStore.SaveSnapshot(ctx, snap); err != nil {
		return err
	}

	// Audit the snapshot.
	if a.auditor != nil {
		_ = a.auditor.RecordEvent(ctx, "path_comparison", uuid.New(), "path.snapshot_created",
			"system", "path_comparison_engine", map[string]any{
				"decision_id":     decisionID,
				"goal_type":       goalType,
				"selected_path":   selectedSignature,
				"selected_score":  selectedScore,
				"candidate_count": len(paths),
			})
	}

	return nil
}

// CaptureSnapshotAt creates a snapshot with an explicit creation time.
func CaptureSnapshotAt(decisionID, goalType string, scoredPaths []ScoredPathInfo, selectedSignature string, selectedScore float64, createdAt time.Time) DecisionSnapshot {
	snap := CaptureSnapshot(decisionID, goalType, scoredPaths, selectedSignature, selectedScore)
	snap.CreatedAt = createdAt
	return snap
}
