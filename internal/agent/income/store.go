package income

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OpportunityStore persists IncomeOpportunity records in PostgreSQL.
type OpportunityStore struct {
	pool *pgxpool.Pool
}

// NewOpportunityStore creates an OpportunityStore backed by the given pool.
func NewOpportunityStore(pool *pgxpool.Pool) *OpportunityStore {
	return &OpportunityStore{pool: pool}
}

// Create inserts a new opportunity and returns the saved record.
func (s *OpportunityStore) Create(ctx context.Context, o IncomeOpportunity) (IncomeOpportunity, error) {
	now := time.Now().UTC()
	o.CreatedAt = now
	o.UpdatedAt = now
	if o.Status == "" {
		o.Status = StatusOpen
	}

	const q = `
INSERT INTO agent_income_opportunities
  (id, source, title, description, opportunity_type,
   estimated_value, estimated_effort, confidence, status, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING id, source, title, description, opportunity_type,
          estimated_value, estimated_effort, confidence, status, created_at, updated_at`

	row := s.pool.QueryRow(ctx, q,
		o.ID, o.Source, o.Title, o.Description, o.OpportunityType,
		o.EstimatedValue, o.EstimatedEffort, o.Confidence, o.Status,
		o.CreatedAt, o.UpdatedAt,
	)
	return scanOpportunity(row)
}

// GetByID retrieves a single opportunity by primary key.
func (s *OpportunityStore) GetByID(ctx context.Context, id string) (IncomeOpportunity, error) {
	const q = `
SELECT id, source, title, description, opportunity_type,
       estimated_value, estimated_effort, confidence, status, created_at, updated_at
FROM agent_income_opportunities WHERE id = $1`

	row := s.pool.QueryRow(ctx, q, id)
	return scanOpportunity(row)
}

// UpdateStatus sets the status of an opportunity.
func (s *OpportunityStore) UpdateStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE agent_income_opportunities SET status=$1, updated_at=$2 WHERE id=$3`
	tag, err := s.pool.Exec(ctx, q, status, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update opportunity status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("opportunity not found: %s", id)
	}
	return nil
}

// List returns opportunities ordered by created_at DESC with pagination.
func (s *OpportunityStore) List(ctx context.Context, limit, offset int) ([]IncomeOpportunity, error) {
	const q = `
SELECT id, source, title, description, opportunity_type,
       estimated_value, estimated_effort, confidence, status, created_at, updated_at
FROM agent_income_opportunities
ORDER BY created_at DESC
LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list opportunities: %w", err)
	}
	defer rows.Close()

	var out []IncomeOpportunity
	for rows.Next() {
		o, err := scanOpportunityRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// BestOpenScore returns the highest income score among open opportunities,
// or 0 if none exist. Used by the graph adapter for the income signal.
func (s *OpportunityStore) BestOpenScore(ctx context.Context) float64 {
	// Re-compute score in SQL using the same formula as scorer.go:
	//   value_score = LEAST(estimated_value / 10000, 1)
	//   score = value_score*0.40 + confidence*0.30 - estimated_effort*0.20
	const q = `
SELECT COALESCE(MAX(
  LEAST(estimated_value / 10000.0, 1.0) * 0.40
  + confidence * 0.30
  - estimated_effort * 0.20
), 0)
FROM agent_income_opportunities
WHERE status = 'open'`

	var score float64
	if err := s.pool.QueryRow(ctx, q).Scan(&score); err != nil {
		return 0
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// CountOpen returns the count of open opportunities.
func (s *OpportunityStore) CountOpen(ctx context.Context) int {
	const q = `SELECT COUNT(*) FROM agent_income_opportunities WHERE status = 'open'`
	var n int
	s.pool.QueryRow(ctx, q).Scan(&n) //nolint:errcheck
	return n
}

// --- row scanner helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOpportunity(row rowScanner) (IncomeOpportunity, error) {
	var o IncomeOpportunity
	err := row.Scan(
		&o.ID, &o.Source, &o.Title, &o.Description, &o.OpportunityType,
		&o.EstimatedValue, &o.EstimatedEffort, &o.Confidence, &o.Status,
		&o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return IncomeOpportunity{}, fmt.Errorf("scan opportunity: %w", err)
	}
	return o, nil
}

func scanOpportunityRow(row rowScanner) (IncomeOpportunity, error) {
	return scanOpportunity(row)
}
