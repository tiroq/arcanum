package calibration

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tracker persists and manages calibration records using a bounded sliding window.
type Tracker struct {
	pool *pgxpool.Pool
}

// NewTracker creates a Tracker backed by PostgreSQL.
func NewTracker(pool *pgxpool.Pool) *Tracker {
	return &Tracker{pool: pool}
}

// Record persists a new calibration record and enforces the sliding window bound.
func (t *Tracker) Record(ctx context.Context, rec CalibrationRecord) error {
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}

	_, err := t.pool.Exec(ctx,
		`INSERT INTO agent_calibration_records (decision_id, predicted_confidence, actual_outcome, created_at)
		 VALUES ($1, $2, $3, $4)`,
		rec.DecisionID, rec.PredictedConfidence, rec.ActualOutcome, rec.CreatedAt,
	)
	if err != nil {
		return err
	}

	// Enforce sliding window: delete oldest records beyond MaxTrackerRecords.
	_, err = t.pool.Exec(ctx,
		`DELETE FROM agent_calibration_records
		 WHERE id NOT IN (
		   SELECT id FROM agent_calibration_records
		   ORDER BY created_at DESC
		   LIMIT $1
		 )`, MaxTrackerRecords)
	return err
}

// ListRecords returns all calibration records ordered by creation time descending.
func (t *Tracker) ListRecords(ctx context.Context, limit, offset int) ([]CalibrationRecord, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT decision_id, predicted_confidence, actual_outcome, created_at
		 FROM agent_calibration_records
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CalibrationRecord
	for rows.Next() {
		var r CalibrationRecord
		if err := rows.Scan(&r.DecisionID, &r.PredictedConfidence, &r.ActualOutcome, &r.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetAllRecords returns all calibration records (bounded by MaxTrackerRecords).
func (t *Tracker) GetAllRecords(ctx context.Context) ([]CalibrationRecord, error) {
	return t.ListRecords(ctx, MaxTrackerRecords, 0)
}

// SaveSummary persists the computed calibration summary as an UPSERT
// on a single-row summary table.
func (t *Tracker) SaveSummary(ctx context.Context, summary CalibrationSummary) error {
	_, err := t.pool.Exec(ctx,
		`INSERT INTO agent_calibration_summary (id, ece, overconfidence_score, underconfidence_score, total_records, last_updated)
		 VALUES (1, $1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO UPDATE SET
		   ece = EXCLUDED.ece,
		   overconfidence_score = EXCLUDED.overconfidence_score,
		   underconfidence_score = EXCLUDED.underconfidence_score,
		   total_records = EXCLUDED.total_records,
		   last_updated = EXCLUDED.last_updated`,
		summary.ExpectedCalibrationError,
		summary.OverconfidenceScore,
		summary.UnderconfidenceScore,
		summary.TotalRecords,
		summary.LastUpdated,
	)
	return err
}

// GetSummary retrieves the current calibration summary.
// Returns nil, nil if no summary has been computed yet.
func (t *Tracker) GetSummary(ctx context.Context) (*CalibrationSummary, error) {
	row := t.pool.QueryRow(ctx,
		`SELECT ece, overconfidence_score, underconfidence_score, total_records, last_updated
		 FROM agent_calibration_summary
		 WHERE id = 1`)

	var s CalibrationSummary
	err := row.Scan(&s.ExpectedCalibrationError, &s.OverconfidenceScore, &s.UnderconfidenceScore, &s.TotalRecords, &s.LastUpdated)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}
