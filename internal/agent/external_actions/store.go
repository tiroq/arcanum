package externalactions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists external actions and results in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new external actions store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// --- Actions ---

// CreateAction inserts a new external action.
func (s *Store) CreateAction(ctx context.Context, a ExternalAction) (ExternalAction, error) {
	const q = `
		INSERT INTO agent_external_actions (
			id, opportunity_id, action_type, payload, status, connector_name,
			idempotency_key, risk_level, review_reason, retry_count, max_retries,
			dry_run_completed, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, opportunity_id, action_type, payload, status, connector_name,
		          idempotency_key, risk_level, review_reason, retry_count, max_retries,
		          dry_run_completed, created_at, updated_at`

	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now

	payloadBytes, err := json.Marshal(a.Payload)
	if err != nil {
		return ExternalAction{}, err
	}

	err = s.pool.QueryRow(ctx, q,
		a.ID, a.OpportunityID, a.ActionType, payloadBytes, a.Status, a.ConnectorName,
		a.IdempotencyKey, a.RiskLevel, a.ReviewReason, a.RetryCount, a.MaxRetries,
		a.DryRunCompleted, a.CreatedAt, a.UpdatedAt,
	).Scan(
		&a.ID, &a.OpportunityID, &a.ActionType, &a.Payload, &a.Status, &a.ConnectorName,
		&a.IdempotencyKey, &a.RiskLevel, &a.ReviewReason, &a.RetryCount, &a.MaxRetries,
		&a.DryRunCompleted, &a.CreatedAt, &a.UpdatedAt,
	)
	return a, err
}

// GetAction retrieves an action by ID.
func (s *Store) GetAction(ctx context.Context, id string) (ExternalAction, error) {
	const q = `
		SELECT id, opportunity_id, action_type, payload, status, connector_name,
		       idempotency_key, risk_level, review_reason, retry_count, max_retries,
		       dry_run_completed, created_at, updated_at
		FROM agent_external_actions
		WHERE id = $1`

	var a ExternalAction
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.OpportunityID, &a.ActionType, &a.Payload, &a.Status, &a.ConnectorName,
		&a.IdempotencyKey, &a.RiskLevel, &a.ReviewReason, &a.RetryCount, &a.MaxRetries,
		&a.DryRunCompleted, &a.CreatedAt, &a.UpdatedAt,
	)
	return a, err
}

// ListActions returns recent actions ordered by creation time.
func (s *Store) ListActions(ctx context.Context, limit int) ([]ExternalAction, error) {
	const q = `
		SELECT id, opportunity_id, action_type, payload, status, connector_name,
		       idempotency_key, risk_level, review_reason, retry_count, max_retries,
		       dry_run_completed, created_at, updated_at
		FROM agent_external_actions
		ORDER BY created_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []ExternalAction
	for rows.Next() {
		var a ExternalAction
		if err := rows.Scan(
			&a.ID, &a.OpportunityID, &a.ActionType, &a.Payload, &a.Status, &a.ConnectorName,
			&a.IdempotencyKey, &a.RiskLevel, &a.ReviewReason, &a.RetryCount, &a.MaxRetries,
			&a.DryRunCompleted, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

// UpdateActionStatus updates the status (and updated_at) of an action.
func (s *Store) UpdateActionStatus(ctx context.Context, id, status string) error {
	const q = `
		UPDATE agent_external_actions
		SET status = $2, updated_at = $3
		WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, status, time.Now().UTC())
	return err
}

// UpdateActionDryRun marks dry_run_completed on an action.
func (s *Store) UpdateActionDryRun(ctx context.Context, id string) error {
	const q = `
		UPDATE agent_external_actions
		SET dry_run_completed = true, updated_at = $2
		WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, time.Now().UTC())
	return err
}

// IncrementRetryCount increments retry_count and updates updated_at.
func (s *Store) IncrementRetryCount(ctx context.Context, id string) error {
	const q = `
		UPDATE agent_external_actions
		SET retry_count = retry_count + 1, updated_at = $2
		WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, time.Now().UTC())
	return err
}

// GetActionByIdempotencyKey retrieves an action by its idempotency key.
func (s *Store) GetActionByIdempotencyKey(ctx context.Context, key string) (ExternalAction, error) {
	const q = `
		SELECT id, opportunity_id, action_type, payload, status, connector_name,
		       idempotency_key, risk_level, review_reason, retry_count, max_retries,
		       dry_run_completed, created_at, updated_at
		FROM agent_external_actions
		WHERE idempotency_key = $1`

	var a ExternalAction
	err := s.pool.QueryRow(ctx, q, key).Scan(
		&a.ID, &a.OpportunityID, &a.ActionType, &a.Payload, &a.Status, &a.ConnectorName,
		&a.IdempotencyKey, &a.RiskLevel, &a.ReviewReason, &a.RetryCount, &a.MaxRetries,
		&a.DryRunCompleted, &a.CreatedAt, &a.UpdatedAt,
	)
	return a, err
}

// --- Results ---

// CreateResult inserts an execution result.
func (s *Store) CreateResult(ctx context.Context, r ExecutionResult) (ExecutionResult, error) {
	const q = `
		INSERT INTO agent_external_action_results (
			id, action_id, success, external_id, response_payload,
			error_message, mode, duration_ms, executed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, action_id, success, external_id, response_payload,
		          error_message, mode, duration_ms, executed_at`

	if r.ExecutedAt.IsZero() {
		r.ExecutedAt = time.Now().UTC()
	}

	respBytes, err := json.Marshal(r.ResponsePayload)
	if err != nil {
		respBytes = []byte("null")
	}

	err = s.pool.QueryRow(ctx, q,
		r.ID, r.ActionID, r.Success, r.ExternalID, respBytes,
		r.ErrorMessage, r.Mode, r.DurationMs, r.ExecutedAt,
	).Scan(
		&r.ID, &r.ActionID, &r.Success, &r.ExternalID, &r.ResponsePayload,
		&r.ErrorMessage, &r.Mode, &r.DurationMs, &r.ExecutedAt,
	)
	return r, err
}

// ListResultsByAction returns results for a given action.
func (s *Store) ListResultsByAction(ctx context.Context, actionID string) ([]ExecutionResult, error) {
	const q = `
		SELECT id, action_id, success, external_id, response_payload,
		       error_message, mode, duration_ms, executed_at
		FROM agent_external_action_results
		WHERE action_id = $1
		ORDER BY executed_at DESC`

	rows, err := s.pool.Query(ctx, q, actionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ExecutionResult
	for rows.Next() {
		var r ExecutionResult
		if err := rows.Scan(
			&r.ID, &r.ActionID, &r.Success, &r.ExternalID, &r.ResponsePayload,
			&r.ErrorMessage, &r.Mode, &r.DurationMs, &r.ExecutedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
