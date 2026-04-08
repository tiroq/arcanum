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
	return s.UpdateMemoryWithContinuation(ctx, strategyType, goalType, outcomeStatus, "", "", false, false)
}

// UpdateMemoryWithContinuation increments counters including step-level and
// continuation tracking. Backward compatible: if step1Status is empty,
// only the original counters are updated.
func (s *MemoryStore) UpdateMemoryWithContinuation(
	ctx context.Context,
	strategyType, goalType, outcomeStatus string,
	step1Status, step2Status string,
	continuationUsed, continuationGain bool,
) error {
	now := time.Now().UTC()
	succInc, failInc, neutralInc := outcomeIncrements(outcomeStatus)

	// Step-level increments.
	step1SuccInc := 0
	if step1Status == OutcomeSuccess {
		step1SuccInc = 1
	}
	step2SuccInc := 0
	if step2Status == OutcomeSuccess {
		step2SuccInc = 1
	}
	contUsedInc := 0
	if continuationUsed {
		contUsedInc = 1
	}
	contGainInc := 0
	if continuationGain {
		contGainInc = 1
	}

	const q = `
		INSERT INTO agent_strategy_memory
			(id, strategy_type, goal_type, total_runs, success_runs, failure_runs, neutral_runs,
			 success_rate, failure_rate,
			 step1_success_runs, step2_success_runs,
			 continuation_used_runs, continuation_gain_runs,
			 step1_success_rate, step2_success_rate, continuation_gain_rate,
			 last_updated)
		VALUES ($1, $2, $3, 1, $4, $5, $6, $4::float, $5::float,
			 $8, $9, $10, $11,
			 $8::float, $9::float,
			 CASE WHEN $10 > 0 THEN $11::float / $10::float ELSE 0 END,
			 $7)
		ON CONFLICT (strategy_type, goal_type) DO UPDATE SET
			total_runs   = agent_strategy_memory.total_runs + 1,
			success_runs = agent_strategy_memory.success_runs + $4,
			failure_runs = agent_strategy_memory.failure_runs + $5,
			neutral_runs = agent_strategy_memory.neutral_runs + $6,
			success_rate = (agent_strategy_memory.success_runs + $4)::float
			             / (agent_strategy_memory.total_runs + 1)::float,
			failure_rate = (agent_strategy_memory.failure_runs + $5)::float
			             / (agent_strategy_memory.total_runs + 1)::float,
			step1_success_runs = agent_strategy_memory.step1_success_runs + $8,
			step2_success_runs = agent_strategy_memory.step2_success_runs + $9,
			continuation_used_runs = agent_strategy_memory.continuation_used_runs + $10,
			continuation_gain_runs = agent_strategy_memory.continuation_gain_runs + $11,
			step1_success_rate = (agent_strategy_memory.step1_success_runs + $8)::float
			                   / (agent_strategy_memory.total_runs + 1)::float,
			step2_success_rate = CASE
				WHEN (agent_strategy_memory.continuation_used_runs + $10) > 0 THEN
					(agent_strategy_memory.step2_success_runs + $9)::float
					/ (agent_strategy_memory.continuation_used_runs + $10)::float
				ELSE 0
				END,
			continuation_gain_rate = CASE
				WHEN (agent_strategy_memory.continuation_used_runs + $10) > 0 THEN
					(agent_strategy_memory.continuation_gain_runs + $11)::float
					/ (agent_strategy_memory.continuation_used_runs + $10)::float
				ELSE 0
				END,
			last_updated = $7`

	_, err := s.db.Exec(ctx, q, uuid.New(), strategyType, goalType,
		succInc, failInc, neutralInc, now,
		step1SuccInc, step2SuccInc, contUsedInc, contGainInc)
	return err
}

// GetMemory retrieves the memory record for a strategy_type + goal_type pair.
// Returns nil if no record exists.
func (s *MemoryStore) GetMemory(ctx context.Context, strategyType, goalType string) (*StrategyMemoryRecord, error) {
	const q = `
		SELECT id, strategy_type, goal_type, total_runs, success_runs,
		       failure_runs, neutral_runs, success_rate, failure_rate, last_updated,
		       step1_success_runs, step2_success_runs,
		       continuation_used_runs, continuation_gain_runs,
		       step1_success_rate, step2_success_rate, continuation_gain_rate,
		       selection_count, win_count, win_rate
		FROM agent_strategy_memory
		WHERE strategy_type = $1 AND goal_type = $2`

	var r StrategyMemoryRecord
	err := s.db.QueryRow(ctx, q, strategyType, goalType).Scan(
		&r.ID, &r.StrategyType, &r.GoalType, &r.TotalRuns,
		&r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
		&r.SuccessRate, &r.FailureRate, &r.LastUpdated,
		&r.Step1SuccessRuns, &r.Step2SuccessRuns,
		&r.ContinuationUsedRuns, &r.ContinuationGainRuns,
		&r.Step1SuccessRate, &r.Step2SuccessRate, &r.ContinuationGainRate,
		&r.SelectionCount, &r.WinCount, &r.WinRate,
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
		       failure_runs, neutral_runs, success_rate, failure_rate, last_updated,
		       step1_success_runs, step2_success_runs,
		       continuation_used_runs, continuation_gain_runs,
		       step1_success_rate, step2_success_rate, continuation_gain_rate,
		       selection_count, win_count, win_rate
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
			&r.Step1SuccessRuns, &r.Step2SuccessRuns,
			&r.ContinuationUsedRuns, &r.ContinuationGainRuns,
			&r.Step1SuccessRate, &r.Step2SuccessRate, &r.ContinuationGainRate,
			&r.SelectionCount, &r.WinCount, &r.WinRate,
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
			 step2_executed, final_status, improvement, evaluated_at,
			 step1_status, step2_status, continuation_used, continuation_gain)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

	_, err := s.db.Exec(ctx, q,
		o.ID, o.StrategyID, o.StrategyType, o.GoalType, o.Step1Action,
		o.Step2Executed, o.FinalStatus, o.Improvement, o.EvaluatedAt,
		o.Step1Status, o.Step2Status, o.ContinuationUsed, o.ContinuationGain,
	)
	return err
}

// ListOutcomes returns recent strategy outcomes.
func (s *MemoryStore) ListOutcomes(ctx context.Context, limit, offset int) ([]StrategyOutcome, error) {
	const q = `
		SELECT id, strategy_id, strategy_type, goal_type, step1_action,
		       step2_executed, final_status, improvement, evaluated_at,
		       step1_status, step2_status, continuation_used, continuation_gain
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
			&o.Step1Status, &o.Step2Status, &o.ContinuationUsed, &o.ContinuationGain,
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

// --- Iteration 19: Portfolio selection tracking ---

// RecordSelection increments the selection count for a strategy+goal pair.
// If won is true, also increments the win counter and recomputes win_rate.
func (s *MemoryStore) RecordSelection(ctx context.Context, strategyType, goalType string, won bool) error {
	winInc := 0
	if won {
		winInc = 1
	}

	const q = `
		UPDATE agent_strategy_memory
		SET selection_count = selection_count + 1,
		    win_count = win_count + $3,
		    win_rate = CASE
		        WHEN (selection_count + 1) > 0 THEN
		            (win_count + $3)::float / (selection_count + 1)::float
		        ELSE 0
		    END,
		    last_updated = NOW()
		WHERE strategy_type = $1 AND goal_type = $2`

	_, err := s.db.Exec(ctx, q, strategyType, goalType, winInc)
	return err
}
