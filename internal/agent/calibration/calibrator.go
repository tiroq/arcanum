package calibration

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Calibrator orchestrates the full calibration lifecycle:
// record outcome → rebuild buckets → recompute summary → persist.
type Calibrator struct {
	tracker *Tracker
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewCalibrator creates a Calibrator.
func NewCalibrator(tracker *Tracker, auditor audit.AuditRecorder, logger *zap.Logger) *Calibrator {
	return &Calibrator{
		tracker: tracker,
		auditor: auditor,
		logger:  logger,
	}
}

// RecordOutcome records a prediction-vs-outcome data point, recomputes
// calibration summary, and persists the updated summary.
func (c *Calibrator) RecordOutcome(ctx context.Context, decisionID string, predictedConfidence float64, actualOutcome string) error {
	rec := CalibrationRecord{
		DecisionID:          decisionID,
		PredictedConfidence: predictedConfidence,
		ActualOutcome:       actualOutcome,
		CreatedAt:           time.Now().UTC(),
	}

	// 1. Persist record.
	if err := c.tracker.Record(ctx, rec); err != nil {
		c.logger.Error("calibration_record_failed",
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
		return err
	}

	c.auditEvent(ctx, "calibration.recorded", map[string]any{
		"decision_id":          decisionID,
		"predicted_confidence": predictedConfidence,
		"actual_outcome":       actualOutcome,
	})

	// 2. Rebuild summary from all records.
	records, err := c.tracker.GetAllRecords(ctx)
	if err != nil {
		c.logger.Error("calibration_records_fetch_failed", zap.Error(err))
		return err
	}

	summary := BuildSummary(records)

	// 3. Persist updated summary.
	if err := c.tracker.SaveSummary(ctx, summary); err != nil {
		c.logger.Error("calibration_summary_save_failed", zap.Error(err))
		return err
	}

	c.auditEvent(ctx, "calibration.updated", map[string]any{
		"ece":                   summary.ExpectedCalibrationError,
		"overconfidence_score":  summary.OverconfidenceScore,
		"underconfidence_score": summary.UnderconfidenceScore,
		"total_records":         summary.TotalRecords,
	})

	return nil
}

// CalibrateConfidence adjusts a raw confidence value based on current calibration data.
//
// Rule: adjustment = (accuracy - avg_confidence) × CalibrationWeight
// Where accuracy and avg_confidence come from the bucket matching rawConfidence.
//
// Fail-open: if no calibration data exists or the bucket has insufficient
// samples, returns rawConfidence unchanged.
func (c *Calibrator) CalibrateConfidence(ctx context.Context, rawConfidence float64) float64 {
	summary, err := c.tracker.GetSummary(ctx)
	if err != nil || summary == nil {
		return rawConfidence // fail-open
	}
	return CalibrateConfidenceFromSummary(rawConfidence, summary)
}

// CalibrateConfidenceFromSummary is the pure, deterministic confidence correction
// function. Exported for use in contexts where summary is already available.
func CalibrateConfidenceFromSummary(rawConfidence float64, summary *CalibrationSummary) float64 {
	if summary == nil || len(summary.Buckets) == 0 {
		return rawConfidence
	}

	idx := BucketIndex(rawConfidence)
	if idx >= len(summary.Buckets) {
		return rawConfidence
	}

	bucket := summary.Buckets[idx]
	if bucket.Count < MinBucketSamples {
		return rawConfidence // insufficient data — fail-open
	}

	adjustment := (bucket.Accuracy - bucket.AvgConfidence) * CalibrationWeight
	adjusted := rawConfidence + adjustment
	return clamp01(adjusted)
}

// GetSummary retrieves the current CalibrationSummary.
// Returns nil if no calibration data exists.
func (c *Calibrator) GetSummary(ctx context.Context) (*CalibrationSummary, error) {
	summary, err := c.tracker.GetSummary(ctx)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, nil
	}

	// Populate buckets from records for a full picture.
	records, err := c.tracker.GetAllRecords(ctx)
	if err != nil {
		return summary, nil // return partial summary
	}
	summary.Buckets = BuildBuckets(records)
	return summary, nil
}

// auditEvent records a calibration audit event.
func (c *Calibrator) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if c.auditor == nil {
		return
	}
	_ = c.auditor.RecordEvent(ctx, "calibration", uuid.New(), eventType,
		"system", "calibration_engine", payload)
}

// clamp01 restricts a value to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
