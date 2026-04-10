package income

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OutcomeStore persists IncomeOutcome records.
type OutcomeStore struct {
	pool *pgxpool.Pool
}

// NewOutcomeStore creates an OutcomeStore backed by the given pool.
func NewOutcomeStore(pool *pgxpool.Pool) *OutcomeStore {
	return &OutcomeStore{pool: pool}
}

// Create inserts a new income outcome.
func (s *OutcomeStore) Create(ctx context.Context, o IncomeOutcome) (IncomeOutcome, error) {
	o.CreatedAt = time.Now().UTC()

	const q = `
INSERT INTO agent_income_outcomes
  (id, opportunity_id, proposal_id, outcome_status,
   actual_value, owner_time_saved, notes, created_at)
VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,NULLIF($7,''),$8)
RETURNING id, opportunity_id, COALESCE(proposal_id,''), outcome_status,
          actual_value, owner_time_saved, COALESCE(notes,''), created_at`

	row := s.pool.QueryRow(ctx, q,
		o.ID, o.OpportunityID, o.ProposalID, o.OutcomeStatus,
		o.ActualValue, o.OwnerTimeSaved, o.Notes, o.CreatedAt,
	)
	return scanOutcome(row)
}

// ListByOpportunity returns all outcomes for a given opportunity ordered by created_at DESC.
func (s *OutcomeStore) ListByOpportunity(ctx context.Context, opportunityID string) ([]IncomeOutcome, error) {
	const q = `
SELECT id, opportunity_id, COALESCE(proposal_id,''), outcome_status,
       actual_value, owner_time_saved, COALESCE(notes,''), created_at
FROM agent_income_outcomes
WHERE opportunity_id = $1
ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, opportunityID)
	if err != nil {
		return nil, fmt.Errorf("list outcomes by opportunity: %w", err)
	}
	defer rows.Close()

	var out []IncomeOutcome
	for rows.Next() {
		o, err := scanOutcome(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// List returns outcomes ordered by created_at DESC with pagination.
func (s *OutcomeStore) List(ctx context.Context, limit, offset int) ([]IncomeOutcome, error) {
	const q = `
SELECT id, opportunity_id, COALESCE(proposal_id,''), outcome_status,
       actual_value, owner_time_saved, COALESCE(notes,''), created_at
FROM agent_income_outcomes
ORDER BY created_at DESC
LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list outcomes: %w", err)
	}
	defer rows.Close()

	var out []IncomeOutcome
	for rows.Next() {
		o, err := scanOutcome(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func scanOutcome(row rowScanner) (IncomeOutcome, error) {
	var o IncomeOutcome
	err := row.Scan(
		&o.ID, &o.OpportunityID, &o.ProposalID, &o.OutcomeStatus,
		&o.ActualValue, &o.OwnerTimeSaved, &o.Notes, &o.CreatedAt,
	)
	if err != nil {
		return IncomeOutcome{}, fmt.Errorf("scan outcome: %w", err)
	}
	return o, nil
}
