package income

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProposalStore persists IncomeActionProposal records.
type ProposalStore struct {
	pool *pgxpool.Pool
}

// NewProposalStore creates a ProposalStore backed by the given pool.
func NewProposalStore(pool *pgxpool.Pool) *ProposalStore {
	return &ProposalStore{pool: pool}
}

// Create inserts a new proposal.
func (s *ProposalStore) Create(ctx context.Context, p IncomeActionProposal) (IncomeActionProposal, error) {
	p.CreatedAt = time.Now().UTC()
	if p.Status == "" {
		p.Status = ProposalStatusPending
	}

	const q = `
INSERT INTO agent_income_proposals
  (id, opportunity_id, action_type, title, reason,
   expected_value, risk_level, requires_review, status, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING id, opportunity_id, action_type, title, reason,
          expected_value, risk_level, requires_review, status, created_at`

	row := s.pool.QueryRow(ctx, q,
		p.ID, p.OpportunityID, p.ActionType, p.Title, p.Reason,
		p.ExpectedValue, p.RiskLevel, p.RequiresReview, p.Status, p.CreatedAt,
	)
	return scanProposal(row)
}

// ListByOpportunity returns all proposals for a given opportunity ordered by created_at DESC.
func (s *ProposalStore) ListByOpportunity(ctx context.Context, opportunityID string) ([]IncomeActionProposal, error) {
	const q = `
SELECT id, opportunity_id, action_type, title, reason,
       expected_value, risk_level, requires_review, status, created_at
FROM agent_income_proposals
WHERE opportunity_id = $1
ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, opportunityID)
	if err != nil {
		return nil, fmt.Errorf("list proposals by opportunity: %w", err)
	}
	defer rows.Close()

	var out []IncomeActionProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// List returns proposals ordered by created_at DESC with pagination.
func (s *ProposalStore) List(ctx context.Context, limit, offset int) ([]IncomeActionProposal, error) {
	const q = `
SELECT id, opportunity_id, action_type, title, reason,
       expected_value, risk_level, requires_review, status, created_at
FROM agent_income_proposals
ORDER BY created_at DESC
LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()

	var out []IncomeActionProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProposal(row rowScanner) (IncomeActionProposal, error) {
	var p IncomeActionProposal
	err := row.Scan(
		&p.ID, &p.OpportunityID, &p.ActionType, &p.Title, &p.Reason,
		&p.ExpectedValue, &p.RiskLevel, &p.RequiresReview, &p.Status, &p.CreatedAt,
	)
	if err != nil {
		return IncomeActionProposal{}, fmt.Errorf("scan proposal: %w", err)
	}
	return p, nil
}
