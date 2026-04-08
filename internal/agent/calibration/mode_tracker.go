package calibration

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ModeTracker persists and manages mode-specific calibration records
// using a bounded sliding window per mode.
type ModeTracker struct {
	pool *pgxpool.Pool
}

// NewModeTracker creates a ModeTracker backed by PostgreSQL.
func NewModeTracker(pool *pgxpool.Pool) *ModeTracker {
	return &ModeTracker{pool: pool}
}

// Record persists a new mode calibration record and enforces the per-mode sliding window.
func (t *ModeTracker) Record(ctx context.Context, rec ModeCalibrationRecord) error {
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}

	_, err := t.pool.Exec(ctx,
		`INSERT INTO agent_mode_calibration_records (decision_id, goal_type, mode, predicted_confidence, actual_outcome, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		rec.DecisionID, rec.GoalType, rec.Mode, rec.PredictedConfidence, rec.ActualOutcome, rec.CreatedAt,
	)
	if err != nil {
		return err
	}

	// Enforce sliding window per mode: delete oldest records beyond ModeMaxTrackerRecords.
	_, err = t.pool.Exec(ctx,
		`DELETE FROM agent_mode_calibration_records
		 WHERE mode = $1 AND id NOT IN (
		   SELECT id FROM agent_mode_calibration_records
		   WHERE mode = $1
		   ORDER BY created_at DESC
		   LIMIT $2
		 )`, rec.Mode, ModeMaxTrackerRecords)
	return err
}

// GetRecordsByMode returns all calibration records for a specific mode.
func (t *ModeTracker) GetRecordsByMode(ctx context.Context, mode string) ([]ModeCalibrationRecord, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT decision_id, goal_type, mode, predicted_confidence, actual_outcome, created_at
		 FROM agent_mode_calibration_records
		 WHERE mode = $1
		 ORDER BY created_at DESC
		 LIMIT $2`, mode, ModeMaxTrackerRecords)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ModeCalibrationRecord
	for rows.Next() {
		var r ModeCalibrationRecord
		if err := rows.Scan(&r.DecisionID, &r.GoalType, &r.Mode, &r.PredictedConfidence, &r.ActualOutcome, &r.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// ListRecords returns recent mode calibration records across all modes.
func (t *ModeTracker) ListRecords(ctx context.Context, limit, offset int) ([]ModeCalibrationRecord, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT decision_id, goal_type, mode, predicted_confidence, actual_outcome, created_at
		 FROM agent_mode_calibration_records
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ModeCalibrationRecord
	for rows.Next() {
		var r ModeCalibrationRecord
		if err := rows.Scan(&r.DecisionID, &r.GoalType, &r.Mode, &r.PredictedConfidence, &r.ActualOutcome, &r.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// SaveSummary persists the computed mode calibration summary as an UPSERT.
func (t *ModeTracker) SaveSummary(ctx context.Context, summary ModeCalibrationSummary) error {
	_, err := t.pool.Exec(ctx,
		`INSERT INTO agent_mode_calibration_summary (mode, ece, overconfidence_score, underconfidence_score, total_records, last_updated)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (mode) DO UPDATE SET
		   ece = EXCLUDED.ece,
		   overconfidence_score = EXCLUDED.overconfidence_score,
		   underconfidence_score = EXCLUDED.underconfidence_score,
		   total_records = EXCLUDED.total_records,
		   last_updated = EXCLUDED.last_updated`,
		summary.Mode,
		summary.ExpectedCalibrationError,
		summary.OverconfidenceScore,
		summary.UnderconfidenceScore,
		summary.TotalRecords,
		summary.LastUpdated,
	)
	return err
}

// GetSummary retrieves the calibration summary for a specific mode.
// Returns nil, nil if no summary exists for the mode.
func (t *ModeTracker) GetSummary(ctx context.Context, mode string) (*ModeCalibrationSummary, error) {
	row := t.pool.QueryRow(ctx,
		`SELECT mode, ece, overconfidence_score, underconfidence_score, total_records, last_updated
		 FROM agent_mode_calibration_summary
		 WHERE mode = $1`, mode)

	var s ModeCalibrationSummary
	err := row.Scan(&s.Mode, &s.ExpectedCalibrationError, &s.OverconfidenceScore, &s.UnderconfidenceScore, &s.TotalRecords, &s.LastUpdated)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// GetAllSummaries retrieves calibration summaries for all modes.
func (t *ModeTracker) GetAllSummaries(ctx context.Context) ([]ModeCalibrationSummary, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT mode, ece, overconfidence_score, underconfidence_score, total_records, last_updated
		 FROM agent_mode_calibration_summary
		 ORDER BY mode`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []ModeCalibrationSummary
	for rows.Next() {
		var s ModeCalibrationSummary
		if err := rows.Scan(&s.Mode, &s.ExpectedCalibrationError, &s.OverconfidenceScore, &s.UnderconfidenceScore, &s.TotalRecords, &s.LastUpdated); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}
