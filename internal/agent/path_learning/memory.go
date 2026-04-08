package pathlearning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MemoryStore persists and retrieves path memory records and path outcomes.
type MemoryStore struct {
	db *pgxpool.Pool
}

// NewMemoryStore creates a MemoryStore backed by PostgreSQL.
func NewMemoryStore(db *pgxpool.Pool) *MemoryStore {
	return &MemoryStore{db: db}
}

// --- Path Memory UPSERT ---

// UpdatePathMemory increments counters and recomputes rates for the given path outcome.
// Uses UPSERT to atomically create or update the record.
func (s *MemoryStore) UpdatePathMemory(ctx context.Context, pathSignature, goalType, outcomeStatus string) error {
	now := time.Now().UTC()
	succInc, failInc, neutralInc := outcomeIncrements(outcomeStatus)

	const q = `
		INSERT INTO agent_path_memory
			(id, path_signature, goal_type, total_runs, success_runs, failure_runs, neutral_runs,
			 success_rate, failure_rate, last_updated)
		VALUES ($1, $2, $3, 1, $4, $5, $6, $4::float, $5::float, $7)
		ON CONFLICT (path_signature, goal_type) DO UPDATE SET
			total_runs   = agent_path_memory.total_runs + 1,
			success_runs = agent_path_memory.success_runs + $4,
			failure_runs = agent_path_memory.failure_runs + $5,
			neutral_runs = agent_path_memory.neutral_runs + $6,
			success_rate = (agent_path_memory.success_runs + $4)::float
			             / (agent_path_memory.total_runs + 1)::float,
			failure_rate = (agent_path_memory.failure_runs + $5)::float
			             / (agent_path_memory.total_runs + 1)::float,
			last_updated = $7`

	_, err := s.db.Exec(ctx, q, uuid.New(), pathSignature, goalType,
		succInc, failInc, neutralInc, now)
	return err
}

// GetPathMemory retrieves the memory record for a path_signature + goal_type pair.
// Returns nil if no record exists.
func (s *MemoryStore) GetPathMemory(ctx context.Context, pathSignature, goalType string) (*PathMemoryRecord, error) {
	const q = `
		SELECT id, path_signature, goal_type, total_runs, success_runs,
		       failure_runs, neutral_runs, success_rate, failure_rate, last_updated
		FROM agent_path_memory
		WHERE path_signature = $1 AND goal_type = $2`

	var r PathMemoryRecord
	err := s.db.QueryRow(ctx, q, pathSignature, goalType).Scan(
		&r.ID, &r.PathSignature, &r.GoalType, &r.TotalRuns,
		&r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
		&r.SuccessRate, &r.FailureRate, &r.LastUpdated,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ListPathMemory returns all path memory records ordered by last_updated DESC.
func (s *MemoryStore) ListPathMemory(ctx context.Context) ([]PathMemoryRecord, error) {
	const q = `
		SELECT id, path_signature, goal_type, total_runs, success_runs,
		       failure_runs, neutral_runs, success_rate, failure_rate, last_updated
		FROM agent_path_memory
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PathMemoryRecord
	for rows.Next() {
		var r PathMemoryRecord
		if err := rows.Scan(
			&r.ID, &r.PathSignature, &r.GoalType, &r.TotalRuns,
			&r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
			&r.SuccessRate, &r.FailureRate, &r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// ListPathMemoryByGoalType returns path memory records for a specific goal type.
func (s *MemoryStore) ListPathMemoryByGoalType(ctx context.Context, goalType string) ([]PathMemoryRecord, error) {
	const q = `
		SELECT id, path_signature, goal_type, total_runs, success_runs,
		       failure_runs, neutral_runs, success_rate, failure_rate, last_updated
		FROM agent_path_memory
		WHERE goal_type = $1
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q, goalType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PathMemoryRecord
	for rows.Next() {
		var r PathMemoryRecord
		if err := rows.Scan(
			&r.ID, &r.PathSignature, &r.GoalType, &r.TotalRuns,
			&r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
			&r.SuccessRate, &r.FailureRate, &r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// --- Path Outcomes ---

// SavePathOutcome persists a path outcome record.
func (s *MemoryStore) SavePathOutcome(ctx context.Context, o PathOutcome) error {
	const q = `
		INSERT INTO agent_path_outcomes
			(id, path_id, goal_type, path_signature, path_length,
			 first_step_action, first_step_status, continuation_used,
			 final_status, improvement, evaluated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err := s.db.Exec(ctx, q,
		o.ID, o.PathID, o.GoalType, o.PathSignature, o.PathLength,
		o.FirstStepAction, o.FirstStepStatus, o.ContinuationUsed,
		o.FinalStatus, o.Improvement, o.EvaluatedAt,
	)
	return err
}

// ListPathOutcomes returns recent path outcomes.
func (s *MemoryStore) ListPathOutcomes(ctx context.Context, limit, offset int) ([]PathOutcome, error) {
	const q = `
		SELECT id, path_id, goal_type, path_signature, path_length,
		       first_step_action, first_step_status, continuation_used,
		       final_status, improvement, evaluated_at
		FROM agent_path_outcomes
		ORDER BY evaluated_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.db.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []PathOutcome
	for rows.Next() {
		var o PathOutcome
		if err := rows.Scan(
			&o.ID, &o.PathID, &o.GoalType, &o.PathSignature, &o.PathLength,
			&o.FirstStepAction, &o.FirstStepStatus, &o.ContinuationUsed,
			&o.FinalStatus, &o.Improvement, &o.EvaluatedAt,
		); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

// ListPathOutcomesByGoalType returns path outcomes filtered by goal type.
func (s *MemoryStore) ListPathOutcomesByGoalType(ctx context.Context, goalType string, limit, offset int) ([]PathOutcome, error) {
	const q = `
		SELECT id, path_id, goal_type, path_signature, path_length,
		       first_step_action, first_step_status, continuation_used,
		       final_status, improvement, evaluated_at
		FROM agent_path_outcomes
		WHERE goal_type = $1
		ORDER BY evaluated_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.db.Query(ctx, q, goalType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []PathOutcome
	for rows.Next() {
		var o PathOutcome
		if err := rows.Scan(
			&o.ID, &o.PathID, &o.GoalType, &o.PathSignature, &o.PathLength,
			&o.FirstStepAction, &o.FirstStepStatus, &o.ContinuationUsed,
			&o.FinalStatus, &o.Improvement, &o.EvaluatedAt,
		); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

// --- Helpers ---

func outcomeIncrements(status string) (succ, fail, neutral int) {
	switch status {
	case OutcomeSuccess:
		return 1, 0, 0
	case OutcomeFailure:
		return 0, 1, 0
	case OutcomeNeutral:
		return 0, 0, 1
	default:
		return 0, 0, 1
	}
}
