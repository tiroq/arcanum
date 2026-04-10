package capacity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists capacity state and decisions in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new capacity store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// UpsertState saves or updates the current capacity state (single-row, id='current').
func (s *Store) UpsertState(ctx context.Context, state CapacityState) (CapacityState, error) {
	const q = `
		INSERT INTO agent_capacity_state (
			id, available_hours_today, available_hours_week, blocked_hours_today,
			owner_load_score, max_daily_work_hours, min_family_time_hours, updated_at
		) VALUES (
			'current', $1, $2, $3, $4, $5, $6, $7
		)
		ON CONFLICT (id) DO UPDATE SET
			available_hours_today = EXCLUDED.available_hours_today,
			available_hours_week  = EXCLUDED.available_hours_week,
			blocked_hours_today   = EXCLUDED.blocked_hours_today,
			owner_load_score      = EXCLUDED.owner_load_score,
			max_daily_work_hours  = EXCLUDED.max_daily_work_hours,
			min_family_time_hours = EXCLUDED.min_family_time_hours,
			updated_at            = EXCLUDED.updated_at
		RETURNING available_hours_today, available_hours_week, blocked_hours_today,
		          owner_load_score, max_daily_work_hours, min_family_time_hours, updated_at`

	now := time.Now().UTC()
	state.UpdatedAt = now

	err := s.pool.QueryRow(ctx, q,
		state.AvailableHoursToday,
		state.AvailableHoursWeek,
		state.BlockedHoursToday,
		state.OwnerLoadScore,
		state.MaxDailyWorkHours,
		state.MinFamilyTimeHours,
		now,
	).Scan(
		&state.AvailableHoursToday,
		&state.AvailableHoursWeek,
		&state.BlockedHoursToday,
		&state.OwnerLoadScore,
		&state.MaxDailyWorkHours,
		&state.MinFamilyTimeHours,
		&state.UpdatedAt,
	)
	return state, err
}

// GetState retrieves the current capacity state. Returns zero state if not found.
func (s *Store) GetState(ctx context.Context) (CapacityState, error) {
	const q = `
		SELECT available_hours_today, available_hours_week, blocked_hours_today,
		       owner_load_score, max_daily_work_hours, min_family_time_hours, updated_at
		FROM agent_capacity_state
		WHERE id = 'current'`

	var state CapacityState
	err := s.pool.QueryRow(ctx, q).Scan(
		&state.AvailableHoursToday,
		&state.AvailableHoursWeek,
		&state.BlockedHoursToday,
		&state.OwnerLoadScore,
		&state.MaxDailyWorkHours,
		&state.MinFamilyTimeHours,
		&state.UpdatedAt,
	)
	if err != nil {
		// Fail-open: return zero state on error (including pgx.ErrNoRows).
		return CapacityState{}, nil
	}
	return state, nil
}

// SaveDecisions persists a batch of capacity decisions.
func (s *Store) SaveDecisions(ctx context.Context, decisions []CapacityDecision) error {
	if len(decisions) == 0 {
		return nil
	}
	const q = `
		INSERT INTO agent_capacity_decisions (
			id, item_type, item_id, estimated_effort, expected_value,
			value_per_hour, capacity_fit_score, recommended, defer_reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	for _, d := range decisions {
		_, err := s.pool.Exec(ctx, q,
			d.ID, d.ItemType, d.ItemID, d.EstimatedEffort, d.ExpectedValue,
			d.ValuePerHour, d.CapacityFitScore, d.Recommended, d.DeferReason, d.CreatedAt,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListRecentDecisions returns the most recent capacity decisions.
func (s *Store) ListRecentDecisions(ctx context.Context, limit int) ([]CapacityDecision, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, item_type, item_id, estimated_effort, expected_value,
		       value_per_hour, capacity_fit_score, recommended, defer_reason, created_at
		FROM agent_capacity_decisions
		ORDER BY created_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []CapacityDecision
	for rows.Next() {
		var d CapacityDecision
		if err := rows.Scan(
			&d.ID, &d.ItemType, &d.ItemID, &d.EstimatedEffort, &d.ExpectedValue,
			&d.ValuePerHour, &d.CapacityFitScore, &d.Recommended, &d.DeferReason, &d.CreatedAt,
		); err != nil {
			continue
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}
