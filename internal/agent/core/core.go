// Package core provides AgentCore — the central hub of the Agent Foundation Layer.
//
// AgentCore implements audit.AuditRecorder as a drop-in wrapper around the base
// PostgresAuditRecorder. When RecordEvent is called:
//  1. The base recorder commits the event to audit_events (source of truth).
//  2. AgentCore maps the audit event to an AgentEvent and appends it to the
//     agent_events journal, forming a causal chain via causation_id.
//  3. The agent working state (agent_state) is updated via optimistic locking.
//  4. If the event's salience is above the threshold, an episodic memory entry
//     is derived and persisted.
//
// Steps 2-4 are best-effort: failures are logged but not returned to callers,
// preserving the existing audit and job execution paths.
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/events"
	"github.com/tiroq/arcanum/internal/agent/eventstore"
	agentmemory "github.com/tiroq/arcanum/internal/agent/memory"
	agentstate "github.com/tiroq/arcanum/internal/agent/state"
	"github.com/tiroq/arcanum/internal/audit"
)

// AgentCore wraps audit.AuditRecorder and drives the Agent Foundation Layer.
type AgentCore struct {
	base       audit.AuditRecorder
	eventStore *eventstore.EventStore
	stateStore *agentstate.StateStore
	memStore   *agentmemory.MemoryStore
	logger     *zap.Logger
}

// New creates an AgentCore.
func New(
	base audit.AuditRecorder,
	eventStore *eventstore.EventStore,
	stateStore *agentstate.StateStore,
	memStore *agentmemory.MemoryStore,
	logger *zap.Logger,
) *AgentCore {
	return &AgentCore{
		base:       base,
		eventStore: eventStore,
		stateStore: stateStore,
		memStore:   memStore,
		logger:     logger,
	}
}

// RecordEvent implements audit.AuditRecorder.
// It always delegates to the base recorder first. Agent-layer processing is
// best-effort: errors are logged but not surfaced to callers so that a failure
// in the agent layer cannot break the operational audit trail.
func (c *AgentCore) RecordEvent(
	ctx context.Context,
	entityType string,
	entityID uuid.UUID,
	eventType, actorType, actorID string,
	payload any,
) error {
	if err := c.base.RecordEvent(ctx, entityType, entityID, eventType, actorType, actorID, payload); err != nil {
		return err
	}
	if err := c.processEvent(ctx, entityType, entityID, eventType, actorType, actorID, payload); err != nil {
		c.logger.Warn("agent event processing failed",
			zap.String("event_type", eventType),
			zap.String("entity_type", entityType),
			zap.Error(err),
		)
	}
	return nil
}

func (c *AgentCore) processEvent(
	ctx context.Context,
	entityType string,
	entityID uuid.UUID,
	eventType, actorType, actorID string,
	payload any,
) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		payloadBytes = []byte("{}")
	}

	correlationID := extractCorrelationID(entityType, entityID, payloadBytes)

	var causationID *uuid.UUID
	if correlationID != nil {
		causationID, err = c.eventStore.GetLastCausation(ctx, *correlationID)
		if err != nil {
			c.logger.Warn("get causation id failed", zap.Error(err))
		}
	}

	e := &events.AgentEvent{
		EventID:       uuid.New(),
		EventType:     eventType,
		Source:        actorType + "/" + actorID,
		Timestamp:     time.Now().UTC(),
		CorrelationID: correlationID,
		CausationID:   causationID,
		Priority:      0,
		Confidence:    1.0,
		Payload:       json.RawMessage(payloadBytes),
		Tags:          []string{entityType},
	}

	if err := c.eventStore.AppendEvent(ctx, e); err != nil {
		return fmt.Errorf("append agent event: %w", err)
	}

	if err := c.stateStore.ApplyEvent(ctx, e); err != nil {
		c.logger.Warn("apply agent state failed", zap.Error(err))
	}

	if err := c.memStore.DeriveFromEvent(ctx, e); err != nil {
		c.logger.Warn("derive memory failed", zap.Error(err))
	}

	return nil
}

// extractCorrelationID derives the correlation_id for grouping events by job.
// job entities use entityID directly; proposal entities use job_id from payload.
func extractCorrelationID(entityType string, entityID uuid.UUID, payloadBytes []byte) *uuid.UUID {
	switch entityType {
	case "job":
		id := entityID
		return &id
	case "proposal":
		var m map[string]any
		if err := json.Unmarshal(payloadBytes, &m); err == nil {
			if jobIDStr, ok := m["job_id"].(string); ok {
				if id, err := uuid.Parse(jobIDStr); err == nil {
					return &id
				}
			}
		}
		id := entityID
		return &id
	default:
		return nil
	}
}
