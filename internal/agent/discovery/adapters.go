package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// --- Signal adapter ---

// SignalStoreAdapter adapts the signals store to the discovery SignalProvider interface.
type SignalStoreAdapter struct {
	pool *pgxpool.Pool
}

// NewSignalStoreAdapter creates a SignalStoreAdapter.
func NewSignalStoreAdapter(pool *pgxpool.Pool) *SignalStoreAdapter {
	return &SignalStoreAdapter{pool: pool}
}

// ListRecentSignals returns signals within the given window.
func (a *SignalStoreAdapter) ListRecentSignals(ctx context.Context, windowHours int, limit int) ([]SignalRecord, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour)
	const q = `
SELECT signal_type, severity, value, source, observed_at
FROM agent_signals
WHERE observed_at >= $1
ORDER BY observed_at DESC
LIMIT $2`

	rows, err := a.pool.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent signals: %w", err)
	}
	defer rows.Close()

	var out []SignalRecord
	for rows.Next() {
		var s SignalRecord
		if err := rows.Scan(&s.SignalType, &s.Severity, &s.Value, &s.Source, &s.ObservedAt); err != nil {
			return nil, fmt.Errorf("scan signal: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// --- Outcome adapter ---

// OutcomeStoreAdapter adapts the outcome store to the discovery OutcomeProvider interface.
type OutcomeStoreAdapter struct {
	pool *pgxpool.Pool
}

// NewOutcomeStoreAdapter creates an OutcomeStoreAdapter.
func NewOutcomeStoreAdapter(pool *pgxpool.Pool) *OutcomeStoreAdapter {
	return &OutcomeStoreAdapter{pool: pool}
}

// ListRecentOutcomes returns outcomes within the given window.
func (a *OutcomeStoreAdapter) ListRecentOutcomes(ctx context.Context, windowHours int, limit int) ([]OutcomeRecord, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour)
	const q = `
SELECT action_type, goal_id, outcome_status, COALESCE('', '') as mode, evaluated_at
FROM agent_action_outcomes
WHERE evaluated_at >= $1
ORDER BY evaluated_at DESC
LIMIT $2`

	rows, err := a.pool.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent outcomes: %w", err)
	}
	defer rows.Close()

	var out []OutcomeRecord
	for rows.Next() {
		var o OutcomeRecord
		if err := rows.Scan(&o.ActionType, &o.GoalType, &o.Status, &o.Mode, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan outcome: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// --- Proposal adapter ---

// ProposalStoreAdapter adapts the income proposal store to the discovery ProposalProvider interface.
type ProposalStoreAdapter struct {
	pool *pgxpool.Pool
}

// NewProposalStoreAdapter creates a ProposalStoreAdapter.
func NewProposalStoreAdapter(pool *pgxpool.Pool) *ProposalStoreAdapter {
	return &ProposalStoreAdapter{pool: pool}
}

// ListRecentProposals returns proposals within the given window.
func (a *ProposalStoreAdapter) ListRecentProposals(ctx context.Context, windowHours int, limit int) ([]ProposalRecord, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour)
	const q = `
SELECT p.action_type, COALESCE(o.opportunity_type, 'other') as opportunity_type, p.status, p.created_at
FROM agent_income_proposals p
LEFT JOIN agent_income_opportunities o ON o.id = p.opportunity_id
WHERE p.created_at >= $1
ORDER BY p.created_at DESC
LIMIT $2`

	rows, err := a.pool.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent proposals: %w", err)
	}
	defer rows.Close()

	var out []ProposalRecord
	for rows.Next() {
		var p ProposalRecord
		if err := rows.Scan(&p.ActionType, &p.OpportunityType, &p.Status, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- Opportunity adapter ---

// OpportunityStoreAdapter checks for existing active income opportunities.
type OpportunityStoreAdapter struct {
	pool *pgxpool.Pool
}

// NewOpportunityStoreAdapter creates an OpportunityStoreAdapter.
func NewOpportunityStoreAdapter(pool *pgxpool.Pool) *OpportunityStoreAdapter {
	return &OpportunityStoreAdapter{pool: pool}
}

// HasActiveOpportunity returns true if an active (open/evaluated/proposed) opportunity exists
// with a matching type. The dedupeKey is checked against the title for a loose match.
func (a *OpportunityStoreAdapter) HasActiveOpportunity(ctx context.Context, opportunityType, dedupeKey string) (bool, error) {
	const q = `
SELECT COUNT(*) FROM agent_income_opportunities
WHERE opportunity_type = $1
  AND status IN ('open', 'evaluated', 'proposed')
  AND title LIKE '%' || $2 || '%'`

	var count int
	err := a.pool.QueryRow(ctx, q, opportunityType, dedupeKey).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check active opportunity: %w", err)
	}
	return count > 0, nil
}
