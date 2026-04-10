package financialpressure

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists and retrieves the single FinancialState row.
// There is exactly one financial state per system (single-row UPSERT).
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Upsert inserts or updates the single financial state row.
// The row ID is always "current" to enforce single-row semantics.
func (s *Store) Upsert(ctx context.Context, state FinancialState) (FinancialState, error) {
	state.ID = "current"
	state.UpdatedAt = time.Now().UTC()

	const q = `
INSERT INTO agent_financial_state
  (id, current_income_month, target_income_month, monthly_expenses, cash_buffer, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
  current_income_month = EXCLUDED.current_income_month,
  target_income_month  = EXCLUDED.target_income_month,
  monthly_expenses     = EXCLUDED.monthly_expenses,
  cash_buffer          = EXCLUDED.cash_buffer,
  updated_at           = EXCLUDED.updated_at
RETURNING id, current_income_month, target_income_month, monthly_expenses, cash_buffer, updated_at`

	row := s.pool.QueryRow(ctx, q,
		state.ID, state.CurrentIncomeMonth, state.TargetIncomeMonth,
		state.MonthlyExpenses, state.CashBuffer, state.UpdatedAt,
	)
	return scanState(row)
}

// Get retrieves the current financial state. Returns a zero-value state if none exists.
func (s *Store) Get(ctx context.Context) (FinancialState, error) {
	const q = `
SELECT id, current_income_month, target_income_month, monthly_expenses, cash_buffer, updated_at
FROM agent_financial_state
WHERE id = 'current'`

	row := s.pool.QueryRow(ctx, q)
	st, err := scanState(row)
	if err != nil {
		// No row is a valid state — return zero values.
		return FinancialState{}, nil //nolint:nilerr
	}
	return st, nil
}

// --- row scanner ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanState(row rowScanner) (FinancialState, error) {
	var st FinancialState
	err := row.Scan(
		&st.ID, &st.CurrentIncomeMonth, &st.TargetIncomeMonth,
		&st.MonthlyExpenses, &st.CashBuffer, &st.UpdatedAt,
	)
	if err != nil {
		return FinancialState{}, fmt.Errorf("scan financial state: %w", err)
	}
	return st, nil
}
