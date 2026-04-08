package strategylearning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MemoryStore persists and retrieves strategy memory records and outcomes.
type MemoryStore struct {
	db *pgxpool.Pool
}

// NewMemoryStore creates a MemoryStore backed by PostgreSQL.
func NewMemoryStore(db *pgxpool.Pool) *MemoryStore {
	return &MemoryStore{db: db}
}

// --- Strategy Memory UPSERT ---

// UpdateMemory increments counters and recomputes rates for the given strategy outcome.
// Uses UPSERT to atomically create or update the record.
func (s *MemoryStore) UpdateMemory(ctx context.Context, strategyType, goalType, outcomeStatus string) error {
	now := time.Now().UTC()
	succInc, failInc, neutralInc := outcomeIncrements(outcomeStatus)

	const q = `
		INSERT INTO agent_strategy_memory
			(id, strategy_type, goal_type, total_runs, success_runs, failure_runs, neutral_runs,
			 success_rate, failure_rate, last_updated)
		VALUES ($1, $2, $3, 1, $4, $5, $6, $4::float, $5::float, $7)
		ON CONFLICT (strategy_type, goal_type) DO UPDATE SET
			total_runs   = agent_strategy_memory.total_runs + 1,
			success_runs = agent_strategy_memory.success_runs + $4,
			failure_runs = agent_strategy_memory.failure_runs + $5,
			neutral_runs = agent_strategy_memory.neutral_runs + $6,
			success_rate = (agent_strategy_memory.success_runs + $4)::float
			             / (agent_strategy_memory.total_runs + 1)::float,
			failure_rate = (agent_strategy_memory.failure_runs + $5)::float
			             / (agent_strategy_memory.total_runs + 1)::float,
			last_updated = $7`

	_, err := s.db.Exec(ctx, q, uuid.New(), strategyType, goalType,
		succInc, failInc, neutralInc, now)
	return err
}

// GetMemory retrieves the memory record for a strategy_type + goal_type pair.
// Returns nil if no record exists.
func (s *MemoryStore) GetMemory(ctx context.Context, strategyType, goalType string) (*StrategyMemoryRecord, error) {
	const q = `
		SELECT id, strategy_type, goal_type, total_runs, success_runs,
		       failure_runs, neutral_runs, success_rate, failure_rate, last_updated
		FROM agent_strategy_memory
		WHERE strategy_type = $1 AND goal_type = $2`

	var r StrategyMemoryRecord
	err := s.db.QueryRow(ctx, q, strategyType, goalType).Scan(
		&r.ID, &r.StrategyType, &r.GoalType, &r.TotalRuns,
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

// ListMemory returns all strategy memory records.
func (s *MemoryStore) ListMemory(ctx context.Context) ([]StrategyMemoryRecord, error) {
	const q = `
		SELECT id, strategy_type, goal_type, total_runs, success_runs,
		       failure_runs, neutral_runs, success_rate, failure_rate, last_updated
		FROM agent_strategy_memory
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []StrategyMemoryRecord
	for rows.Next() {
		var r StrategyMemoryRecord
		if err := rows.Scan(
			&r.ID, &r.StrategyType, &r.GoalType, &r.TotalRuns,
			&r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
			&r.SuccessRate, &r.FailureRate, &r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// --- Strategy Outcomes ---

// SaveOutcome persists a strategy outcome record.
func (s *MemoryStore) SaveOutcome(ctx context.Context, o StrategyOutcome) error {
	const q = `
		INSERT INTO agent_strategy_outcomes
			(id, strategy_id, strategy_type, goal_type, step1_action,
			 step2_executed, final_status, improvement, evaluated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := s.db.Exec(ctx, q,
		o.ID, o.StrategyID, o.StrategyType, o.GoalType, o.Step1Action,
		o.Step2Executed, o.FinalStatus, o.Improvement, o.EvaluatedAt,
	)
	return err
}

// ListOutcomes returns recent strategy outcomes.
func (s *MemoryStore) ListOutcomes(ctx context.Context, limit, offset int) ([]StrategyOutcome, error) {
	const q = `
		SELECT id, strategy_id, strategy_type, goal_type, step1_action,
		       step2_executed, final_status, improvement, evaluated_at
		FROM agent_strategy_outcomes
		ORDER BY evaluated_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.db.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []StrategyOutcome
	for rows.Next() {
		var o StrategyOutcome
		if err := rows.Scan(
			&o.ID, &o.StrategyID, &o.StrategyType, &o.GoalType, &o.Step1Action,
			&o.Step2Executed, &o.FinalStatus, &o.Improvement, &o.EvaluatedAt,
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
