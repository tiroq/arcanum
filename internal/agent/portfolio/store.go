package portfolio

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StrategyStore manages strategy persistence in PostgreSQL.
type StrategyStore struct {
	pool *pgxpool.Pool
}

// NewStrategyStore creates a new StrategyStore.
func NewStrategyStore(pool *pgxpool.Pool) *StrategyStore {
	return &StrategyStore{pool: pool}
}

// Create persists a new strategy. ID is generated if empty.
func (s *StrategyStore) Create(ctx context.Context, st Strategy) (Strategy, error) {
	if st.ID == "" {
		st.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	st.CreatedAt = now
	st.UpdatedAt = now
	if st.Status == "" {
		st.Status = StatusActive
	}

	const q = `
		INSERT INTO agent_strategies (id, name, type, expected_return_per_hour, volatility, time_to_first_value, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := s.pool.Exec(ctx, q,
		st.ID, st.Name, st.Type,
		st.ExpectedReturnPerHr, st.Volatility, st.TimeToFirstValue,
		st.Status, st.CreatedAt, st.UpdatedAt,
	)
	if err != nil {
		return Strategy{}, fmt.Errorf("insert strategy: %w", err)
	}
	return st, nil
}

// Get retrieves a strategy by ID.
func (s *StrategyStore) Get(ctx context.Context, id string) (Strategy, error) {
	const q = `
		SELECT id, name, type, expected_return_per_hour, volatility, time_to_first_value, status, created_at, updated_at
		FROM agent_strategies WHERE id = $1`

	var st Strategy
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&st.ID, &st.Name, &st.Type,
		&st.ExpectedReturnPerHr, &st.Volatility, &st.TimeToFirstValue,
		&st.Status, &st.CreatedAt, &st.UpdatedAt,
	)
	if err != nil {
		return Strategy{}, fmt.Errorf("get strategy: %w", err)
	}
	return st, nil
}

// ListActive returns all active strategies, sorted by expected return DESC.
func (s *StrategyStore) ListActive(ctx context.Context) ([]Strategy, error) {
	const q = `
		SELECT id, name, type, expected_return_per_hour, volatility, time_to_first_value, status, created_at, updated_at
		FROM agent_strategies
		WHERE status = 'active'
		ORDER BY expected_return_per_hour DESC, name ASC`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list active strategies: %w", err)
	}
	defer rows.Close()

	var result []Strategy
	for rows.Next() {
		var st Strategy
		if err := rows.Scan(
			&st.ID, &st.Name, &st.Type,
			&st.ExpectedReturnPerHr, &st.Volatility, &st.TimeToFirstValue,
			&st.Status, &st.CreatedAt, &st.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan strategy: %w", err)
		}
		result = append(result, st)
	}
	return result, rows.Err()
}

// ListAll returns all strategies regardless of status.
func (s *StrategyStore) ListAll(ctx context.Context) ([]Strategy, error) {
	const q = `
		SELECT id, name, type, expected_return_per_hour, volatility, time_to_first_value, status, created_at, updated_at
		FROM agent_strategies
		ORDER BY status ASC, expected_return_per_hour DESC, name ASC`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list all strategies: %w", err)
	}
	defer rows.Close()

	var result []Strategy
	for rows.Next() {
		var st Strategy
		if err := rows.Scan(
			&st.ID, &st.Name, &st.Type,
			&st.ExpectedReturnPerHr, &st.Volatility, &st.TimeToFirstValue,
			&st.Status, &st.CreatedAt, &st.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan strategy: %w", err)
		}
		result = append(result, st)
	}
	return result, rows.Err()
}

// UpdateStatus changes the status of a strategy.
func (s *StrategyStore) UpdateStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE agent_strategies SET status = $1, updated_at = $2 WHERE id = $3`
	tag, err := s.pool.Exec(ctx, q, status, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update strategy status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("strategy not found: %s", id)
	}
	return nil
}

// FindByType returns the first active strategy matching the given type.
func (s *StrategyStore) FindByType(ctx context.Context, strategyType string) (Strategy, error) {
	const q = `
		SELECT id, name, type, expected_return_per_hour, volatility, time_to_first_value, status, created_at, updated_at
		FROM agent_strategies
		WHERE type = $1 AND status = 'active'
		ORDER BY expected_return_per_hour DESC
		LIMIT 1`

	var st Strategy
	err := s.pool.QueryRow(ctx, q, strategyType).Scan(
		&st.ID, &st.Name, &st.Type,
		&st.ExpectedReturnPerHr, &st.Volatility, &st.TimeToFirstValue,
		&st.Status, &st.CreatedAt, &st.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return Strategy{}, nil
	}
	if err != nil {
		return Strategy{}, fmt.Errorf("find strategy by type: %w", err)
	}
	return st, nil
}

// AllocationStore manages strategy allocation persistence.
type AllocationStore struct {
	pool *pgxpool.Pool
}

// NewAllocationStore creates a new AllocationStore.
func NewAllocationStore(pool *pgxpool.Pool) *AllocationStore {
	return &AllocationStore{pool: pool}
}

// Upsert creates or updates an allocation for a strategy.
func (s *AllocationStore) Upsert(ctx context.Context, alloc StrategyAllocation) (StrategyAllocation, error) {
	if alloc.ID == "" {
		alloc.ID = uuid.New().String()
	}
	if alloc.CreatedAt.IsZero() {
		alloc.CreatedAt = time.Now().UTC()
	}

	const q = `
		INSERT INTO agent_strategy_allocations (id, strategy_id, allocated_hours, actual_hours, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (strategy_id) DO UPDATE
			SET allocated_hours = EXCLUDED.allocated_hours,
				actual_hours = EXCLUDED.actual_hours,
				created_at = EXCLUDED.created_at`

	_, err := s.pool.Exec(ctx, q,
		alloc.ID, alloc.StrategyID, alloc.AllocatedHours, alloc.ActualHours, alloc.CreatedAt,
	)
	if err != nil {
		return StrategyAllocation{}, fmt.Errorf("upsert allocation: %w", err)
	}
	return alloc, nil
}

// GetByStrategy retrieves the current allocation for a strategy.
func (s *AllocationStore) GetByStrategy(ctx context.Context, strategyID string) (StrategyAllocation, error) {
	const q = `
		SELECT id, strategy_id, allocated_hours, actual_hours, created_at
		FROM agent_strategy_allocations
		WHERE strategy_id = $1`

	var alloc StrategyAllocation
	err := s.pool.QueryRow(ctx, q, strategyID).Scan(
		&alloc.ID, &alloc.StrategyID, &alloc.AllocatedHours, &alloc.ActualHours, &alloc.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return StrategyAllocation{}, nil
	}
	if err != nil {
		return StrategyAllocation{}, fmt.Errorf("get allocation: %w", err)
	}
	return alloc, nil
}

// ListAll returns all current allocations.
func (s *AllocationStore) ListAll(ctx context.Context) ([]StrategyAllocation, error) {
	const q = `
		SELECT id, strategy_id, allocated_hours, actual_hours, created_at
		FROM agent_strategy_allocations
		ORDER BY allocated_hours DESC`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list allocations: %w", err)
	}
	defer rows.Close()

	var result []StrategyAllocation
	for rows.Next() {
		var alloc StrategyAllocation
		if err := rows.Scan(
			&alloc.ID, &alloc.StrategyID, &alloc.AllocatedHours, &alloc.ActualHours, &alloc.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan allocation: %w", err)
		}
		result = append(result, alloc)
	}
	return result, rows.Err()
}

// PerformanceStore manages strategy performance persistence.
type PerformanceStore struct {
	pool *pgxpool.Pool
}

// NewPerformanceStore creates a new PerformanceStore.
func NewPerformanceStore(pool *pgxpool.Pool) *PerformanceStore {
	return &PerformanceStore{pool: pool}
}

// Upsert creates or updates performance data for a strategy.
func (s *PerformanceStore) Upsert(ctx context.Context, perf StrategyPerformance) error {
	if perf.UpdatedAt.IsZero() {
		perf.UpdatedAt = time.Now().UTC()
	}

	const q = `
		INSERT INTO agent_strategy_performance (strategy_id, total_revenue, total_time_spent, roi, conversion_rate, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (strategy_id) DO UPDATE
			SET total_revenue = EXCLUDED.total_revenue,
				total_time_spent = EXCLUDED.total_time_spent,
				roi = EXCLUDED.roi,
				conversion_rate = EXCLUDED.conversion_rate,
				updated_at = EXCLUDED.updated_at`

	_, err := s.pool.Exec(ctx, q,
		perf.StrategyID, perf.TotalRevenue, perf.TotalTimeSpent,
		perf.ROI, perf.ConversionRate, perf.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert performance: %w", err)
	}
	return nil
}

// Get retrieves performance data for a strategy.
func (s *PerformanceStore) Get(ctx context.Context, strategyID string) (StrategyPerformance, error) {
	const q = `
		SELECT strategy_id, total_revenue, total_time_spent, roi, conversion_rate, updated_at
		FROM agent_strategy_performance
		WHERE strategy_id = $1`

	var p StrategyPerformance
	err := s.pool.QueryRow(ctx, q, strategyID).Scan(
		&p.StrategyID, &p.TotalRevenue, &p.TotalTimeSpent,
		&p.ROI, &p.ConversionRate, &p.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return StrategyPerformance{}, nil
	}
	if err != nil {
		return StrategyPerformance{}, fmt.Errorf("get performance: %w", err)
	}
	return p, nil
}

// ListAll returns all performance records.
func (s *PerformanceStore) ListAll(ctx context.Context) ([]StrategyPerformance, error) {
	const q = `
		SELECT strategy_id, total_revenue, total_time_spent, roi, conversion_rate, updated_at
		FROM agent_strategy_performance
		ORDER BY roi DESC`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list performance: %w", err)
	}
	defer rows.Close()

	var result []StrategyPerformance
	for rows.Next() {
		var p StrategyPerformance
		if err := rows.Scan(
			&p.StrategyID, &p.TotalRevenue, &p.TotalTimeSpent,
			&p.ROI, &p.ConversionRate, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan performance: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
