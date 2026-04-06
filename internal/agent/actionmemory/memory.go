package actionmemory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists and retrieves action memory records.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates a Store backed by PostgreSQL.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// OutcomeInput contains the fields needed to update action memory.
// Defined here to avoid importing the outcome package.
type OutcomeInput struct {
	ActionType    string
	TargetType    string
	TargetID      uuid.UUID
	OutcomeStatus string // "success", "neutral", "failure"
}

// Update increments counters and recomputes rates for the given outcome.
// Uses UPSERT to atomically create or update the record.
func (s *Store) Update(ctx context.Context, o OutcomeInput) error {
	now := time.Now().UTC()

	// Update aggregate record (action_type + target_type).
	if err := s.upsertAggregate(ctx, o, now); err != nil {
		return fmt.Errorf("upsert aggregate: %w", err)
	}

	// Update per-target record.
	if o.TargetID != uuid.Nil {
		if err := s.upsertTarget(ctx, o, now); err != nil {
			return fmt.Errorf("upsert target: %w", err)
		}
	}

	return nil
}

func (s *Store) upsertAggregate(ctx context.Context, o OutcomeInput, now time.Time) error {
	succInc, failInc, neutralInc := outcomeIncrements(o.OutcomeStatus)

	const q = `
		INSERT INTO agent_action_memory (id, action_type, target_type, total_runs, success_runs, failure_runs, neutral_runs, success_rate, failure_rate, last_updated)
		VALUES ($1, $2, $3, 1, $4, $5, $6, $4::float, $5::float, $7)
		ON CONFLICT (action_type, target_type) DO UPDATE SET
			total_runs   = agent_action_memory.total_runs + 1,
			success_runs = agent_action_memory.success_runs + $4,
			failure_runs = agent_action_memory.failure_runs + $5,
			neutral_runs = agent_action_memory.neutral_runs + $6,
			success_rate = (agent_action_memory.success_runs + $4)::float / (agent_action_memory.total_runs + 1)::float,
			failure_rate = (agent_action_memory.failure_runs + $5)::float / (agent_action_memory.total_runs + 1)::float,
			last_updated = $7`

	_, err := s.db.Exec(ctx, q, uuid.New(), o.ActionType, o.TargetType, succInc, failInc, neutralInc, now)
	return err
}

func (s *Store) upsertTarget(ctx context.Context, o OutcomeInput, now time.Time) error {
	succInc, failInc, neutralInc := outcomeIncrements(o.OutcomeStatus)

	const q = `
		INSERT INTO agent_action_memory_targets (id, action_type, target_type, target_id, total_runs, success_runs, failure_runs, neutral_runs, success_rate, failure_rate, last_updated)
		VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $5::float, $6::float, $8)
		ON CONFLICT (action_type, target_type, target_id) DO UPDATE SET
			total_runs   = agent_action_memory_targets.total_runs + 1,
			success_runs = agent_action_memory_targets.success_runs + $5,
			failure_runs = agent_action_memory_targets.failure_runs + $6,
			neutral_runs = agent_action_memory_targets.neutral_runs + $7,
			success_rate = (agent_action_memory_targets.success_runs + $5)::float / (agent_action_memory_targets.total_runs + 1)::float,
			failure_rate = (agent_action_memory_targets.failure_runs + $6)::float / (agent_action_memory_targets.total_runs + 1)::float,
			last_updated = $8`

	_, err := s.db.Exec(ctx, q, uuid.New(), o.ActionType, o.TargetType, o.TargetID, succInc, failInc, neutralInc, now)
	return err
}

// List returns all aggregate memory records.
func (s *Store) List(ctx context.Context) ([]ActionMemoryRecord, error) {
	const q = `
		SELECT id, action_type, target_type, total_runs, success_runs, failure_runs, neutral_runs, success_rate, failure_rate, last_updated
		FROM agent_action_memory
		ORDER BY total_runs DESC`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query action memory: %w", err)
	}
	defer rows.Close()

	var records []ActionMemoryRecord
	for rows.Next() {
		var r ActionMemoryRecord
		if err := rows.Scan(&r.ID, &r.ActionType, &r.TargetType, &r.TotalRuns,
			&r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
			&r.SuccessRate, &r.FailureRate, &r.LastUpdated); err != nil {
			return nil, fmt.Errorf("scan action memory: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetByActionType returns the aggregate record for a specific action type.
// Returns nil if no record exists.
func (s *Store) GetByActionType(ctx context.Context, actionType string) (*ActionMemoryRecord, error) {
	const q = `
		SELECT id, action_type, target_type, total_runs, success_runs, failure_runs, neutral_runs, success_rate, failure_rate, last_updated
		FROM agent_action_memory
		WHERE action_type = $1
		LIMIT 1`

	var r ActionMemoryRecord
	err := s.db.QueryRow(ctx, q, actionType).Scan(
		&r.ID, &r.ActionType, &r.TargetType, &r.TotalRuns,
		&r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
		&r.SuccessRate, &r.FailureRate, &r.LastUpdated)
	if err != nil {
		return nil, nil // no record yet
	}
	return &r, nil
}

func outcomeIncrements(status string) (succ, fail, neutral int) {
	switch status {
	case "success":
		return 1, 0, 0
	case "failure":
		return 0, 1, 0
	case "neutral":
		return 0, 0, 1
	default:
		return 0, 0, 1
	}
}
