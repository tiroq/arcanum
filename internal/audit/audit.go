package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// contextKeyType is an unexported type for audit context keys to prevent collisions.
type contextKeyType struct{}

// JobIDKey is the context key used to store the current job's UUID during execution.
// Set this before calling proc.Process so the AuditedProvider can correlate LLM
// calls with the job that triggered them.
var JobIDKey = contextKeyType{}

// AuditRecorder defines the interface for recording audit events.
type AuditRecorder interface {
	RecordEvent(ctx context.Context, entityType string, entityID uuid.UUID, eventType, actorType, actorID string, payload any) error
}

// PostgresAuditRecorder writes audit events to PostgreSQL.
type PostgresAuditRecorder struct {
	pool *pgxpool.Pool
}

// NewPostgresAuditRecorder creates a new PostgresAuditRecorder.
func NewPostgresAuditRecorder(pool *pgxpool.Pool) *PostgresAuditRecorder {
	return &PostgresAuditRecorder{pool: pool}
}

// RecordEvent appends an audit event to the audit_events table.
// This operation is append-only; records are never updated or deleted.
func (r *PostgresAuditRecorder) RecordEvent(
	ctx context.Context,
	entityType string,
	entityID uuid.UUID,
	eventType string,
	actorType string,
	actorID string,
	payload any,
) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal audit payload: %w", err)
	}

	const q = `
		INSERT INTO audit_events (id, entity_type, entity_id, event_type, actor_type, actor_id, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err = r.pool.Exec(ctx, q,
		uuid.New(),
		entityType,
		entityID,
		eventType,
		actorType,
		actorID,
		payloadJSON,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return nil
}

// NoOpAuditRecorder discards all audit events. Useful for testing.
type NoOpAuditRecorder struct{}

func (r *NoOpAuditRecorder) RecordEvent(_ context.Context, _ string, _ uuid.UUID, _, _, _ string, _ any) error {
	return nil
}
