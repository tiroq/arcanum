package calibration

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ContextStore manages per-context calibration records in PostgreSQL.
type ContextStore struct {
	pool *pgxpool.Pool
}

// NewContextStore creates a ContextStore.
func NewContextStore(pool *pgxpool.Pool) *ContextStore {
	return &ContextStore{pool: pool}
}

// GetByContext retrieves a calibration context record by exact dimension match.
// Returns nil if no record exists.
func (s *ContextStore) GetByContext(ctx context.Context, calCtx CalibrationContext) (*CalibrationContextRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, goal_type, provider_name, strategy_type,
		       sample_count, avg_predicted_confidence, avg_actual_success,
		       calibration_error, last_updated
		FROM agent_confidence_calibration_context
		WHERE COALESCE(goal_type, '') = $1
		  AND COALESCE(provider_name, '') = $2
		  AND COALESCE(strategy_type, '') = $3
	`, calCtx.GoalType, calCtx.ProviderName, calCtx.StrategyType)

	var rec CalibrationContextRecord
	err := row.Scan(
		&rec.ID,
		&rec.GoalType,
		&rec.ProviderName,
		&rec.StrategyType,
		&rec.SampleCount,
		&rec.AvgPredictedConfidence,
		&rec.AvgActualSuccess,
		&rec.CalibrationError,
		&rec.LastUpdated,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// UpsertContext performs an incremental update (UPSERT) for a context record.
// Uses incremental rolling average: new_avg = old_avg + (new_value - old_avg) / new_count.
func (s *ContextStore) UpsertContext(ctx context.Context, calCtx CalibrationContext, predictedConfidence float64, actualSuccess float64) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_confidence_calibration_context
			(goal_type, provider_name, strategy_type, sample_count,
			 avg_predicted_confidence, avg_actual_success, calibration_error, last_updated)
		VALUES (NULLIF($1, ''), NULLIF($2, ''), NULLIF($3, ''), 1, $4, $5, $4 - $5, $6)
		ON CONFLICT (COALESCE(goal_type, ''), COALESCE(provider_name, ''), COALESCE(strategy_type, ''))
		DO UPDATE SET
			sample_count = agent_confidence_calibration_context.sample_count + 1,
			avg_predicted_confidence = agent_confidence_calibration_context.avg_predicted_confidence +
				($4 - agent_confidence_calibration_context.avg_predicted_confidence) /
				(agent_confidence_calibration_context.sample_count + 1),
			avg_actual_success = agent_confidence_calibration_context.avg_actual_success +
				($5 - agent_confidence_calibration_context.avg_actual_success) /
				(agent_confidence_calibration_context.sample_count + 1),
			calibration_error = (agent_confidence_calibration_context.avg_predicted_confidence +
				($4 - agent_confidence_calibration_context.avg_predicted_confidence) /
				(agent_confidence_calibration_context.sample_count + 1)) -
				(agent_confidence_calibration_context.avg_actual_success +
				($5 - agent_confidence_calibration_context.avg_actual_success) /
				(agent_confidence_calibration_context.sample_count + 1)),
			last_updated = $6
	`, calCtx.GoalType, calCtx.ProviderName, calCtx.StrategyType,
		predictedConfidence, actualSuccess, now)
	return err
}

// GetAll retrieves all context calibration records.
func (s *ContextStore) GetAll(ctx context.Context) ([]CalibrationContextRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, goal_type, provider_name, strategy_type,
		       sample_count, avg_predicted_confidence, avg_actual_success,
		       calibration_error, last_updated
		FROM agent_confidence_calibration_context
		ORDER BY sample_count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CalibrationContextRecord
	for rows.Next() {
		var rec CalibrationContextRecord
		if err := rows.Scan(
			&rec.ID,
			&rec.GoalType,
			&rec.ProviderName,
			&rec.StrategyType,
			&rec.SampleCount,
			&rec.AvgPredictedConfidence,
			&rec.AvgActualSuccess,
			&rec.CalibrationError,
			&rec.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// GetByGoalType retrieves context calibration records filtered by goal type.
func (s *ContextStore) GetByGoalType(ctx context.Context, goalType string) ([]CalibrationContextRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, goal_type, provider_name, strategy_type,
		       sample_count, avg_predicted_confidence, avg_actual_success,
		       calibration_error, last_updated
		FROM agent_confidence_calibration_context
		WHERE goal_type = $1
		ORDER BY sample_count DESC
	`, goalType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CalibrationContextRecord
	for rows.Next() {
		var rec CalibrationContextRecord
		if err := rows.Scan(
			&rec.ID,
			&rec.GoalType,
			&rec.ProviderName,
			&rec.StrategyType,
			&rec.SampleCount,
			&rec.AvgPredictedConfidence,
			&rec.AvgActualSuccess,
			&rec.CalibrationError,
			&rec.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}
