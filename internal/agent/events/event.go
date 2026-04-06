// Package events defines the AgentEvent type — the unified envelope used by
// the agent's append-only event journal. Every significant system action
// produces an AgentEvent so that the full job lifecycle can be reconstructed
// from the journal alone.
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentEvent is a single entry in the agent's event journal.
// CorrelationID groups events that belong to the same job (= job_id).
// CausationID links each event to its immediate predecessor in the causal chain.
type AgentEvent struct {
	EventID       uuid.UUID       `json:"event_id"`
	EventType     string          `json:"event_type"`
	Source        string          `json:"source"`
	Timestamp     time.Time       `json:"timestamp"`
	CorrelationID *uuid.UUID      `json:"correlation_id,omitempty"`
	CausationID   *uuid.UUID      `json:"causation_id,omitempty"`
	Priority      int             `json:"priority"`
	Confidence    float64         `json:"confidence"`
	Payload       json.RawMessage `json:"payload"`
	Tags          []string        `json:"tags"`
}

// Salience returns a deterministic 0-1 score reflecting how significant an event is.
// Used to decide whether to persist an episodic memory entry.
func Salience(eventType string) float64 {
	switch eventType {
	case "job.dead_letter":
		return 0.95
	case "job.completed":
		return 0.90
	case "proposal.created":
		return 0.85
	case "job.failed":
		return 0.70
	case "llm.finished":
		return 0.60
	case "job.created":
		return 0.30
	case "job.leased":
		return 0.20
	default:
		return 0.40
	}
}

// SalienceThreshold is the minimum salience required to persist an episodic
// memory entry. Events below this threshold are journaled but not memorised.
const SalienceThreshold = 0.50
