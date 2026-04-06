// Package eventstore provides an append-only journal for AgentEvents backed
// by the agent_events PostgreSQL table.
package eventstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiroq/arcanum/internal/agent/events"
)

// EventStore appends and retrieves AgentEvents from the agent_events journal.
type EventStore struct {
	pool *pgxpool.Pool
}

// New creates an EventStore backed by the given connection pool.
func New(pool *pgxpool.Pool) *EventStore {
	return &EventStore{pool: pool}
}

// AppendEvent inserts a new event into the journal.
// EventID and Timestamp are assigned automatically if not set.
func (s *EventStore) AppendEvent(ctx context.Context, e *events.AgentEvent) error {
	if e.EventID == uuid.Nil {
		e.EventID = uuid.New()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	payload := e.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	tags := e.Tags
	if tags == nil {
		tags = []string{}
	}

	const q = `
INSERT INTO agent_events
(event_id, event_type, source, timestamp,
 correlation_id, causation_id, priority, confidence, payload, tags)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := s.pool.Exec(ctx, q,
		e.EventID, e.EventType, e.Source, e.Timestamp,
		e.CorrelationID, e.CausationID,
		e.Priority, e.Confidence,
		payload, tags,
	)
	if err != nil {
		return fmt.Errorf("append agent event: %w", err)
	}
	return nil
}

// GetLastCausation returns the event_id of the most recent event with the
// given correlation_id, or nil if no prior event exists.
func (s *EventStore) GetLastCausation(ctx context.Context, correlationID uuid.UUID) (*uuid.UUID, error) {
	const q = `
SELECT event_id FROM agent_events
WHERE correlation_id = $1
ORDER BY timestamp DESC
LIMIT 1`

	var id uuid.UUID
	err := s.pool.QueryRow(ctx, q, correlationID).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get last causation: %w", err)
	}
	return &id, nil
}

// GetTimeline returns all events for a correlation_id in chronological order.
func (s *EventStore) GetTimeline(ctx context.Context, correlationID uuid.UUID) ([]events.AgentEvent, error) {
	const q = `
SELECT event_id, event_type, source, timestamp,
       correlation_id, causation_id, priority, confidence, payload, tags
FROM agent_events
WHERE correlation_id = $1
ORDER BY timestamp ASC`

	rows, err := s.pool.Query(ctx, q, correlationID)
	if err != nil {
		return nil, fmt.Errorf("query agent events: %w", err)
	}
	defer rows.Close()

	result := make([]events.AgentEvent, 0)
	for rows.Next() {
		var e events.AgentEvent
		if err := rows.Scan(
			&e.EventID, &e.EventType, &e.Source, &e.Timestamp,
			&e.CorrelationID, &e.CausationID,
			&e.Priority, &e.Confidence, &e.Payload, &e.Tags,
		); err != nil {
			return nil, fmt.Errorf("scan agent event: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
