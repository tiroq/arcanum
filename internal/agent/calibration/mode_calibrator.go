package calibration

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// ModeCalibrator orchestrates mode-specific calibration:
// record outcome → rebuild buckets → recompute summary → persist.
type ModeCalibrator struct {
	tracker *ModeTracker
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewModeCalibrator creates a ModeCalibrator.
func NewModeCalibrator(tracker *ModeTracker, auditor audit.AuditRecorder, logger *zap.Logger) *ModeCalibrator {
	return &ModeCalibrator{
		tracker: tracker,
		auditor: auditor,
		logger:  logger,
	}
}

// RecordOutcome records a mode-specific prediction-vs-outcome data point,
// recomputes the mode's calibration summary, and persists it.
// Only records for known modes are used for calibration; unknown modes
// are still persisted but do not update summaries.
func (c *ModeCalibrator) RecordOutcome(ctx context.Context, decisionID, goalType, mode string, predictedConfidence float64, actualOutcome string) error {
	rec := ModeCalibrationRecord{
		DecisionID:          decisionID,
		GoalType:            goalType,
		Mode:                mode,
		PredictedConfidence: predictedConfidence,
		ActualOutcome:       actualOutcome,
		CreatedAt:           time.Now().UTC(),
	}

	// 1. Persist record.
	if err := c.tracker.Record(ctx, rec); err != nil {
		c.logger.Error("mode_calibration_record_failed",
			zap.String("decision_id", decisionID),
			zap.String("mode", mode),
			zap.Error(err),
		)
		return err
	}

	c.auditEvent(ctx, "calibration.mode_recorded", map[string]any{
		"decision_id":          decisionID,
		"goal_type":            goalType,
		"mode":                 mode,
		"predicted_confidence": predictedConfidence,
		"actual_outcome":       actualOutcome,
	})

	// 2. Rebuild summary from mode-specific records only.
	// No cross-contamination: only records matching this mode are used.
	if !IsKnownMode(mode) {
		return nil // Unknown mode — record persisted but no summary update.
	}

	records, err := c.tracker.GetRecordsByMode(ctx, mode)
	if err != nil {
		c.logger.Error("mode_calibration_records_fetch_failed",
			zap.String("mode", mode),
			zap.Error(err),
		)
		return err
	}

	summary := BuildModeSummary(mode, records)

	// 3. Persist updated summary.
	if err := c.tracker.SaveSummary(ctx, summary); err != nil {
		c.logger.Error("mode_calibration_summary_save_failed",
			zap.String("mode", mode),
			zap.Error(err),
		)
		return err
	}

	c.auditEvent(ctx, "calibration.mode_updated", map[string]any{
		"mode":                  mode,
		"ece":                   summary.ExpectedCalibrationError,
		"overconfidence_score":  summary.OverconfidenceScore,
		"underconfidence_score": summary.UnderconfidenceScore,
		"total_records":         summary.TotalRecords,
	})

	return nil
}

// CalibrateConfidenceForMode adjusts a raw confidence value based on
// mode-specific calibration data.
//
// Formula:
//
//	adjustment = (mode_accuracy - avg_mode_confidence) × ModeCalibrationWeight
//	clamped to ±ModeMaxAdjustment
//	adjusted = clamp(rawConfidence + adjustment, 0, 1)
//
// Fail-open: if no calibration data exists, the bucket has insufficient
// samples, or the mode is unknown, returns rawConfidence unchanged.
func (c *ModeCalibrator) CalibrateConfidenceForMode(ctx context.Context, rawConfidence float64, mode string) float64 {
	if !IsKnownMode(mode) {
		return rawConfidence // unknown mode — fail-open
	}

	summary, err := c.tracker.GetSummary(ctx, mode)
	if err != nil || summary == nil {
		return rawConfidence // fail-open
	}

	return CalibrateConfidenceFromModeSummary(rawConfidence, summary)
}

// CalibrateConfidenceFromModeSummary is the pure, deterministic mode-specific
// confidence correction function. Exported for use where summary is already available.
//
// adjustment = (bucket_accuracy - bucket_avg_confidence) × ModeCalibrationWeight
// clamped to [-ModeMaxAdjustment, +ModeMaxAdjustment]
// adjusted = clamp(rawConfidence + adjustment, 0, 1)
func CalibrateConfidenceFromModeSummary(rawConfidence float64, summary *ModeCalibrationSummary) float64 {
	if summary == nil || len(summary.Buckets) == 0 {
		return rawConfidence
	}

	idx := BucketIndex(rawConfidence)
	if idx >= len(summary.Buckets) {
		return rawConfidence
	}

	bucket := summary.Buckets[idx]
	if bucket.Count < ModeMinBucketSamples {
		return rawConfidence // insufficient data — fail-open
	}

	adjustment := (bucket.Accuracy - bucket.AvgConfidence) * ModeCalibrationWeight

	// Bound the adjustment.
	if adjustment > ModeMaxAdjustment {
		adjustment = ModeMaxAdjustment
	}
	if adjustment < -ModeMaxAdjustment {
		adjustment = -ModeMaxAdjustment
	}

	adjusted := rawConfidence + adjustment
	return clamp01(adjusted)
}

// GetSummary retrieves the mode calibration summary including populated buckets.
// Returns nil if no data exists for the mode.
func (c *ModeCalibrator) GetSummary(ctx context.Context, mode string) (*ModeCalibrationSummary, error) {
	summary, err := c.tracker.GetSummary(ctx, mode)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, nil
	}

	// Populate buckets from records for a full picture.
	records, err := c.tracker.GetRecordsByMode(ctx, mode)
	if err != nil {
		return summary, nil // return partial summary
	}
	summary.Buckets = BuildModeBuckets(mode, records)
	return summary, nil
}

// GetAllSummaries retrieves mode calibration summaries for all modes.
func (c *ModeCalibrator) GetAllSummaries(ctx context.Context) ([]ModeCalibrationSummary, error) {
	return c.tracker.GetAllSummaries(ctx)
}

// auditEvent records a mode calibration audit event.
func (c *ModeCalibrator) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if c.auditor == nil {
		return
	}
	_ = c.auditor.RecordEvent(ctx, "calibration", uuid.New(), eventType,
		"system", "mode_calibration_engine", payload)
}
