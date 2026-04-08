package pathcomparison

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutcomeStore persists and retrieves comparative outcomes.
type OutcomeStore struct {
	db *pgxpool.Pool
}

// NewOutcomeStore creates an OutcomeStore backed by PostgreSQL.
func NewOutcomeStore(db *pgxpool.Pool) *OutcomeStore {
	return &OutcomeStore{db: db}
}

// SaveOutcome persists a comparative outcome.
func (s *OutcomeStore) SaveOutcome(ctx context.Context, o ComparativeOutcome) error {
	const q = `
		INSERT INTO agent_path_comparative_outcomes
			(id, decision_id, goal_type, selected_path, selected_outcome,
			 ranking_error, overestimated, underestimated, better_alternative_exists,
			 created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (decision_id) DO NOTHING`

	_, err := s.db.Exec(ctx, q,
		uuid.New(), o.DecisionID, o.GoalType,
		o.SelectedPathSignature, o.SelectedOutcome,
		o.RankingError, o.Overestimated, o.Underestimated,
		o.BetterAlternativeExists, o.CreatedAt,
	)
	return err
}

// ListOutcomes returns recent comparative outcomes.
func (s *OutcomeStore) ListOutcomes(ctx context.Context, limit, offset int) ([]ComparativeOutcome, error) {
	const q = `
		SELECT decision_id, goal_type, selected_path, selected_outcome,
		       ranking_error, overestimated, underestimated, better_alternative_exists,
		       created_at
		FROM agent_path_comparative_outcomes
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.db.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []ComparativeOutcome
	for rows.Next() {
		var o ComparativeOutcome
		if err := rows.Scan(
			&o.DecisionID, &o.GoalType,
			&o.SelectedPathSignature, &o.SelectedOutcome,
			&o.RankingError, &o.Overestimated, &o.Underestimated,
			&o.BetterAlternativeExists, &o.CreatedAt,
		); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

// ListOutcomesByGoalType returns comparative outcomes filtered by goal type.
func (s *OutcomeStore) ListOutcomesByGoalType(ctx context.Context, goalType string, limit, offset int) ([]ComparativeOutcome, error) {
	const q = `
		SELECT decision_id, goal_type, selected_path, selected_outcome,
		       ranking_error, overestimated, underestimated, better_alternative_exists,
		       created_at
		FROM agent_path_comparative_outcomes
		WHERE goal_type = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.db.Query(ctx, q, goalType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []ComparativeOutcome
	for rows.Next() {
		var o ComparativeOutcome
		if err := rows.Scan(
			&o.DecisionID, &o.GoalType,
			&o.SelectedPathSignature, &o.SelectedOutcome,
			&o.RankingError, &o.Overestimated, &o.Underestimated,
			&o.BetterAlternativeExists, &o.CreatedAt,
		); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

// MemoryStore persists and retrieves comparative memory records.
type MemoryStore struct {
	db *pgxpool.Pool
}

// NewMemoryStore creates a MemoryStore backed by PostgreSQL.
func NewMemoryStore(db *pgxpool.Pool) *MemoryStore {
	return &MemoryStore{db: db}
}

// RecordSelection updates comparative memory for a selected path (win or loss).
func (s *MemoryStore) RecordSelection(ctx context.Context, pathSignature, goalType string, win, loss bool) error {
	now := time.Now().UTC()
	winInc := 0
	lossInc := 0
	if win {
		winInc = 1
	}
	if loss {
		lossInc = 1
	}

	const q = `
		INSERT INTO agent_path_comparative_memory
			(id, path_signature, goal_type, selection_count, win_count, loss_count,
			 missed_win_count, win_rate, loss_rate, last_updated)
		VALUES ($1, $2, $3, 1, $4, $5, 0, $4::float, $5::float, $6)
		ON CONFLICT (path_signature, goal_type) DO UPDATE SET
			selection_count = agent_path_comparative_memory.selection_count + 1,
			win_count       = agent_path_comparative_memory.win_count + $4,
			loss_count      = agent_path_comparative_memory.loss_count + $5,
			win_rate        = (agent_path_comparative_memory.win_count + $4)::float
			                / (agent_path_comparative_memory.selection_count + 1)::float,
			loss_rate       = (agent_path_comparative_memory.loss_count + $5)::float
			                / (agent_path_comparative_memory.selection_count + 1)::float,
			last_updated    = $6`

	_, err := s.db.Exec(ctx, q,
		uuid.New(), pathSignature, goalType,
		winInc, lossInc, now,
	)
	return err
}

// RecordMissedWin increments the missed_win_count for a path that was not selected
// but may have been a better choice.
func (s *MemoryStore) RecordMissedWin(ctx context.Context, pathSignature, goalType string) error {
	now := time.Now().UTC()

	const q = `
		INSERT INTO agent_path_comparative_memory
			(id, path_signature, goal_type, selection_count, win_count, loss_count,
			 missed_win_count, win_rate, loss_rate, last_updated)
		VALUES ($1, $2, $3, 0, 0, 0, 1, 0, 0, $4)
		ON CONFLICT (path_signature, goal_type) DO UPDATE SET
			missed_win_count = agent_path_comparative_memory.missed_win_count + 1,
			last_updated     = $4`

	_, err := s.db.Exec(ctx, q,
		uuid.New(), pathSignature, goalType, now,
	)
	return err
}

// GetMemory retrieves the comparative memory record for a path_signature + goal_type pair.
// Returns nil if not found.
func (s *MemoryStore) GetMemory(ctx context.Context, pathSignature, goalType string) (*ComparativeMemoryRecord, error) {
	const q = `
		SELECT path_signature, goal_type, selection_count, win_count, loss_count,
		       missed_win_count, win_rate, loss_rate, last_updated
		FROM agent_path_comparative_memory
		WHERE path_signature = $1 AND goal_type = $2`

	var r ComparativeMemoryRecord
	err := s.db.QueryRow(ctx, q, pathSignature, goalType).Scan(
		&r.PathSignature, &r.GoalType,
		&r.SelectionCount, &r.WinCount, &r.LossCount,
		&r.MissedWinCount, &r.WinRate, &r.LossRate,
		&r.LastUpdated,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ListMemory returns all comparative memory records ordered by last_updated DESC.
func (s *MemoryStore) ListMemory(ctx context.Context) ([]ComparativeMemoryRecord, error) {
	const q = `
		SELECT path_signature, goal_type, selection_count, win_count, loss_count,
		       missed_win_count, win_rate, loss_rate, last_updated
		FROM agent_path_comparative_memory
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ComparativeMemoryRecord
	for rows.Next() {
		var r ComparativeMemoryRecord
		if err := rows.Scan(
			&r.PathSignature, &r.GoalType,
			&r.SelectionCount, &r.WinCount, &r.LossCount,
			&r.MissedWinCount, &r.WinRate, &r.LossRate,
			&r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// ListMemoryByGoalType returns comparative memory records for a specific goal type.
func (s *MemoryStore) ListMemoryByGoalType(ctx context.Context, goalType string) ([]ComparativeMemoryRecord, error) {
	const q = `
		SELECT path_signature, goal_type, selection_count, win_count, loss_count,
		       missed_win_count, win_rate, loss_rate, last_updated
		FROM agent_path_comparative_memory
		WHERE goal_type = $1
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q, goalType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ComparativeMemoryRecord
	for rows.Next() {
		var r ComparativeMemoryRecord
		if err := rows.Scan(
			&r.PathSignature, &r.GoalType,
			&r.SelectionCount, &r.WinCount, &r.LossCount,
			&r.MissedWinCount, &r.WinRate, &r.LossRate,
			&r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}
