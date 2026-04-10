package actuation

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DecisionStore manages persistence for actuation decisions.
type DecisionStore struct {
	pool *pgxpool.Pool
}

// NewDecisionStore creates a new DecisionStore.
func NewDecisionStore(pool *pgxpool.Pool) *DecisionStore {
	return &DecisionStore{pool: pool}
}

// Insert persists a new actuation decision.
func (s *DecisionStore) Insert(ctx context.Context, d ActuationDecision) error {
	const q = `
		INSERT INTO agent_actuation_decisions
			(id, type, reason, signal_source, confidence, priority, requires_review, status, target, proposed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := s.pool.Exec(ctx, q,
		d.ID, string(d.Type), d.Reason, d.SignalSource,
		d.Confidence, d.Priority, d.RequiresReview,
		string(d.Status), d.Target, d.ProposedAt,
	)
	if err != nil {
		return fmt.Errorf("insert actuation decision: %w", err)
	}
	return nil
}

// Get retrieves a single actuation decision by ID.
func (s *DecisionStore) Get(ctx context.Context, id string) (ActuationDecision, error) {
	const q = `
		SELECT id, type, reason, signal_source, confidence, priority,
			requires_review, status, target, proposed_at, resolved_at
		FROM agent_actuation_decisions WHERE id = $1`
	var d ActuationDecision
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&d.ID, &d.Type, &d.Reason, &d.SignalSource,
		&d.Confidence, &d.Priority, &d.RequiresReview,
		&d.Status, &d.Target, &d.ProposedAt, &d.ResolvedAt,
	)
	if err == pgx.ErrNoRows {
		return ActuationDecision{}, fmt.Errorf("actuation decision not found: %s", id)
	}
	if err != nil {
		return ActuationDecision{}, fmt.Errorf("get actuation decision: %w", err)
	}
	return d, nil
}

// UpdateStatus transitions a decision to a new status.
func (s *DecisionStore) UpdateStatus(ctx context.Context, id string, status DecisionStatus, resolvedAt *time.Time) error {
	const q = `
		UPDATE agent_actuation_decisions
		SET status = $2, resolved_at = $3
		WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id, string(status), resolvedAt)
	if err != nil {
		return fmt.Errorf("update actuation decision status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("actuation decision not found: %s", id)
	}
	return nil
}

// List returns recent actuation decisions, ordered by proposed_at descending.
func (s *DecisionStore) List(ctx context.Context, limit int) ([]ActuationDecision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT id, type, reason, signal_source, confidence, priority,
			requires_review, status, target, proposed_at, resolved_at
		FROM agent_actuation_decisions
		ORDER BY proposed_at DESC
		LIMIT $1`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list actuation decisions: %w", err)
	}
	defer rows.Close()

	var decisions []ActuationDecision
	for rows.Next() {
		var d ActuationDecision
		if err := rows.Scan(
			&d.ID, &d.Type, &d.Reason, &d.SignalSource,
			&d.Confidence, &d.Priority, &d.RequiresReview,
			&d.Status, &d.Target, &d.ProposedAt, &d.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scan actuation decision: %w", err)
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}
