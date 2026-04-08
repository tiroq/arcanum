package meta_reasoning

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MemoryStore manages mode memory persistence in agent_meta_reasoning_memory.
type MemoryStore struct {
	pool *pgxpool.Pool
}

// NewMemoryStore creates a MemoryStore.
func NewMemoryStore(pool *pgxpool.Pool) *MemoryStore {
	return &MemoryStore{pool: pool}
}

// RecordSelection increments selection_count for a mode+goal_type.
// Uses UPSERT to create the record if it does not exist.
func (s *MemoryStore) RecordSelection(ctx context.Context, mode DecisionMode, goalType string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_meta_reasoning_memory (mode, goal_type, selection_count, last_selected_at, created_at, updated_at)
		VALUES ($1, $2, 1, $3, $3, $3)
		ON CONFLICT (mode, goal_type) DO UPDATE SET
			selection_count = agent_meta_reasoning_memory.selection_count + 1,
			last_selected_at = $3,
			updated_at = $3
	`, string(mode), goalType, now)
	return err
}

// RecordOutcome updates success/failure counts and recomputes success_rate.
func (s *MemoryStore) RecordOutcome(ctx context.Context, mode DecisionMode, goalType string, success bool) error {
	now := time.Now().UTC()
	successInc := 0
	failureInc := 0
	if success {
		successInc = 1
	} else {
		failureInc = 1
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_meta_reasoning_memory (mode, goal_type, selection_count, success_count, failure_count, success_rate, created_at, updated_at)
		VALUES ($1, $2, 0, $3, $4, 0, $5, $5)
		ON CONFLICT (mode, goal_type) DO UPDATE SET
			success_count = agent_meta_reasoning_memory.success_count + $3,
			failure_count = agent_meta_reasoning_memory.failure_count + $4,
			success_rate = CASE
				WHEN agent_meta_reasoning_memory.selection_count > 0
				THEN (agent_meta_reasoning_memory.success_count + $3)::DOUBLE PRECISION / agent_meta_reasoning_memory.selection_count
				ELSE 0
			END,
			updated_at = $5
	`, string(mode), goalType, successInc, failureInc, now)
	return err
}

// GetByModeAndGoal retrieves a single memory record.
func (s *MemoryStore) GetByModeAndGoal(ctx context.Context, mode DecisionMode, goalType string) (*ModeMemoryRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, mode, goal_type, selection_count, success_count, failure_count, success_rate, last_selected_at, created_at, updated_at
		FROM agent_meta_reasoning_memory
		WHERE mode = $1 AND goal_type = $2
	`, string(mode), goalType)

	var r ModeMemoryRecord
	err := row.Scan(&r.ID, &r.Mode, &r.GoalType, &r.SelectionCount, &r.SuccessCount, &r.FailureCount, &r.SuccessRate, &r.LastSelectedAt, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetAllForGoal retrieves all mode memory records for a goal type.
func (s *MemoryStore) GetAllForGoal(ctx context.Context, goalType string) (map[DecisionMode]*ModeMemoryRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, mode, goal_type, selection_count, success_count, failure_count, success_rate, last_selected_at, created_at, updated_at
		FROM agent_meta_reasoning_memory
		WHERE goal_type = $1
		ORDER BY mode
	`, goalType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[DecisionMode]*ModeMemoryRecord)
	for rows.Next() {
		var r ModeMemoryRecord
		if err := rows.Scan(&r.ID, &r.Mode, &r.GoalType, &r.SelectionCount, &r.SuccessCount, &r.FailureCount, &r.SuccessRate, &r.LastSelectedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		result[r.Mode] = &r
	}
	return result, rows.Err()
}

// ListMemory returns all mode memory records, ordered by mode, goal_type.
func (s *MemoryStore) ListMemory(ctx context.Context) ([]ModeMemoryRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, mode, goal_type, selection_count, success_count, failure_count, success_rate, last_selected_at, created_at, updated_at
		FROM agent_meta_reasoning_memory
		ORDER BY mode, goal_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ModeMemoryRecord
	for rows.Next() {
		var r ModeMemoryRecord
		if err := rows.Scan(&r.ID, &r.Mode, &r.GoalType, &r.SelectionCount, &r.SuccessCount, &r.FailureCount, &r.SuccessRate, &r.LastSelectedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// HistoryStore manages mode selection history in agent_meta_reasoning_history.
type HistoryStore struct {
	pool *pgxpool.Pool
}

// NewHistoryStore creates a HistoryStore.
func NewHistoryStore(pool *pgxpool.Pool) *HistoryStore {
	return &HistoryStore{pool: pool}
}

// RecordDecision persists a mode selection event.
func (s *HistoryStore) RecordDecision(ctx context.Context, goalType string, decision ModeDecision) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_meta_reasoning_history (goal_type, selected_mode, confidence, reason, created_at)
		VALUES ($1, $2, $3, $4, NOW())
	`, goalType, string(decision.Mode), decision.Confidence, decision.Reason)
	return err
}

// UpdateOutcome sets the outcome for the most recent history record matching goal_type+mode.
func (s *HistoryStore) UpdateOutcome(ctx context.Context, goalType string, mode DecisionMode, outcome string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_meta_reasoning_history
		SET outcome = $1
		WHERE id = (
			SELECT id FROM agent_meta_reasoning_history
			WHERE goal_type = $2 AND selected_mode = $3 AND outcome = ''
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, outcome, goalType, string(mode))
	return err
}

// ListHistory returns recent history records, optionally filtered by goal_type.
func (s *HistoryStore) ListHistory(ctx context.Context, goalType string, limit int) ([]ModeHistoryRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	var query string
	var args []any
	if goalType != "" {
		query = `
			SELECT id, goal_type, selected_mode, confidence, reason, outcome, created_at
			FROM agent_meta_reasoning_history
			WHERE goal_type = $1
			ORDER BY created_at DESC
			LIMIT $2`
		args = []any{goalType, limit}
	} else {
		query = `
			SELECT id, goal_type, selected_mode, confidence, reason, outcome, created_at
			FROM agent_meta_reasoning_history
			ORDER BY created_at DESC
			LIMIT $1`
		args = []any{limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ModeHistoryRecord
	for rows.Next() {
		var r ModeHistoryRecord
		if err := rows.Scan(&r.ID, &r.GoalType, &r.SelectedMode, &r.Confidence, &r.Reason, &r.Outcome, &r.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
