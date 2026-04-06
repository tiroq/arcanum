// Package memory manages episodic memory derived from the agent event journal.
// A memory entry is created only when an event's salience meets or exceeds
// SalienceThreshold — high-signal moments are retained, routine events are not.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiroq/arcanum/internal/agent/events"
)

// EpisodicMemory is a significant moment derived from an AgentEvent.
type EpisodicMemory struct {
	ID            uuid.UUID  `json:"id"`
	EventID       uuid.UUID  `json:"event_id"`
	CorrelationID *uuid.UUID `json:"correlation_id,omitempty"`
	Summary       string     `json:"summary"`
	Salience      float64    `json:"salience"`
	CreatedAt     time.Time  `json:"created_at"`
}

// MemoryStore writes and reads episodic memory from agent_memory_episodic.
type MemoryStore struct {
	pool *pgxpool.Pool
}

// New creates a MemoryStore backed by the given connection pool.
func New(pool *pgxpool.Pool) *MemoryStore {
	return &MemoryStore{pool: pool}
}

// DeriveFromEvent creates an episodic memory entry if the event's salience
// meets or exceeds SalienceThreshold. Safe to call for every event — low-salience
// events are silently skipped.
func (s *MemoryStore) DeriveFromEvent(ctx context.Context, e *events.AgentEvent) error {
	sal := events.Salience(e.EventType)
	if sal < events.SalienceThreshold {
		return nil
	}

	payload := e.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}

	const q = `
		INSERT INTO agent_memory_episodic
			(id, event_id, correlation_id, summary, salience, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())`

	_, err := s.pool.Exec(ctx, q,
		uuid.New(), e.EventID, e.CorrelationID,
		buildSummary(e), sal, payload,
	)
	if err != nil {
		return fmt.Errorf("insert episodic memory: %w", err)
	}
	return nil
}

// GetByCorrelationID returns all episodic memories for a correlation_id,
// ordered by salience descending (most significant first).
func (s *MemoryStore) GetByCorrelationID(ctx context.Context, correlationID uuid.UUID) ([]EpisodicMemory, error) {
	const q = `
		SELECT id, event_id, correlation_id, summary, salience, created_at
		FROM agent_memory_episodic
		WHERE correlation_id = $1
		ORDER BY salience DESC, created_at ASC`

	rows, err := s.pool.Query(ctx, q, correlationID)
	if err != nil {
		return nil, fmt.Errorf("query episodic memories: %w", err)
	}
	defer rows.Close()

	result := make([]EpisodicMemory, 0)
	for rows.Next() {
		var m EpisodicMemory
		if err := rows.Scan(
			&m.ID, &m.EventID, &m.CorrelationID,
			&m.Summary, &m.Salience, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan episodic memory: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// buildSummary produces a human-readable one-liner for an event.
func buildSummary(e *events.AgentEvent) string {
	switch e.EventType {
	case "job.completed":
		return fmt.Sprintf("Job completed by %s", e.Source)
	case "job.dead_letter":
		return fmt.Sprintf("Job moved to dead-letter by %s", e.Source)
	case "proposal.created":
		return fmt.Sprintf("Proposal created by %s", e.Source)
	case "job.failed":
		return fmt.Sprintf("Job failed at %s", e.Source)
	case "llm.finished":
		return fmt.Sprintf("LLM execution finished via %s", e.Source)
	default:
		return fmt.Sprintf("%s (%s)", e.EventType, e.Source)
	}
}
