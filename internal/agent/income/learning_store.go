package income

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LearningStore persists per-type learning records in PostgreSQL (Iteration 39).
type LearningStore struct {
	pool *pgxpool.Pool
}

// NewLearningStore creates a LearningStore backed by the given pool.
func NewLearningStore(pool *pgxpool.Pool) *LearningStore {
	return &LearningStore{pool: pool}
}

// Upsert inserts or updates a learning record for the given opportunity type.
func (s *LearningStore) Upsert(ctx context.Context, r LearningRecord) error {
	r.UpdatedAt = time.Now().UTC()

	const q = `
INSERT INTO agent_income_learning
  (opportunity_type, total_outcomes, success_count, total_accuracy,
   avg_accuracy, success_rate, confidence_adjustment, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (opportunity_type) DO UPDATE SET
  total_outcomes        = EXCLUDED.total_outcomes,
  success_count         = EXCLUDED.success_count,
  total_accuracy        = EXCLUDED.total_accuracy,
  avg_accuracy          = EXCLUDED.avg_accuracy,
  success_rate          = EXCLUDED.success_rate,
  confidence_adjustment = EXCLUDED.confidence_adjustment,
  updated_at            = EXCLUDED.updated_at`

	_, err := s.pool.Exec(ctx, q,
		r.OpportunityType, r.TotalOutcomes, r.SuccessCount, r.TotalAccuracy,
		r.AvgAccuracy, r.SuccessRate, r.ConfidenceAdjustment, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert learning record: %w", err)
	}
	return nil
}

// GetByType returns the learning record for a given opportunity type.
// Returns a zero-value record if none exists.
func (s *LearningStore) GetByType(ctx context.Context, oppType string) (LearningRecord, error) {
	const q = `
SELECT opportunity_type, total_outcomes, success_count, total_accuracy,
       avg_accuracy, success_rate, confidence_adjustment, updated_at
FROM agent_income_learning
WHERE opportunity_type = $1`

	var r LearningRecord
	err := s.pool.QueryRow(ctx, q, oppType).Scan(
		&r.OpportunityType, &r.TotalOutcomes, &r.SuccessCount, &r.TotalAccuracy,
		&r.AvgAccuracy, &r.SuccessRate, &r.ConfidenceAdjustment, &r.UpdatedAt,
	)
	if err != nil {
		// Return zero-value for not-found; other errors propagate.
		return LearningRecord{OpportunityType: oppType}, nil //nolint:nilerr
	}
	return r, nil
}

// GetAll returns all learning records sorted by opportunity_type.
func (s *LearningStore) GetAll(ctx context.Context) ([]LearningRecord, error) {
	const q = `
SELECT opportunity_type, total_outcomes, success_count, total_accuracy,
       avg_accuracy, success_rate, confidence_adjustment, updated_at
FROM agent_income_learning
ORDER BY opportunity_type`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list learning records: %w", err)
	}
	defer rows.Close()

	var out []LearningRecord
	for rows.Next() {
		var r LearningRecord
		if err := rows.Scan(
			&r.OpportunityType, &r.TotalOutcomes, &r.SuccessCount, &r.TotalAccuracy,
			&r.AvgAccuracy, &r.SuccessRate, &r.ConfidenceAdjustment, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan learning record: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
