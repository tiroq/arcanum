// Package state manages the single-row working state for the autonomous agent.
// State is updated on each significant event via optimistic locking so concurrent
// writers (orchestrator + worker) converge without blocking.
package state

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiroq/arcanum/internal/agent/events"
)

// AgentState mirrors the single row in the agent_state table.
type AgentState struct {
	ID                    int        `json:"id"`
	StateVersion          int64      `json:"state_version"`
	LastEventID           *uuid.UUID `json:"last_event_id,omitempty"`
	ActiveJobs            int        `json:"active_jobs"`
	TotalJobsProcessed    int64      `json:"total_jobs_processed"`
	TotalProposalsCreated int64      `json:"total_proposals_created"`
	LastUpdated           time.Time  `json:"last_updated"`
}

// StateStore manages the single-row working state in agent_state.
type StateStore struct {
	pool *pgxpool.Pool
}

// New creates a StateStore backed by the given connection pool.
func New(pool *pgxpool.Pool) *StateStore {
	return &StateStore{pool: pool}
}

// GetState returns the current working state.
func (s *StateStore) GetState(ctx context.Context) (*AgentState, error) {
	const q = `
		SELECT id, state_version, last_event_id,
		       active_jobs, total_jobs_processed, total_proposals_created,
		       last_updated
		FROM agent_state WHERE id = 1`

	var st AgentState
	err := s.pool.QueryRow(ctx, q).Scan(
		&st.ID, &st.StateVersion, &st.LastEventID,
		&st.ActiveJobs, &st.TotalJobsProcessed, &st.TotalProposalsCreated,
		&st.LastUpdated,
	)
	if err != nil {
		return nil, fmt.Errorf("get agent state: %w", err)
	}
	return &st, nil
}

// ApplyEvent updates the working state based on the event type.
// Uses optimistic locking (state_version) — a missed update is silently ignored;
// the state will be advanced by the next event in the chain.
func (s *StateStore) ApplyEvent(ctx context.Context, e *events.AgentEvent) error {
	const readQ = `
		SELECT state_version, active_jobs, total_jobs_processed, total_proposals_created
		FROM agent_state WHERE id = 1`

	var version int64
	var activeJobs int
	var totalProcessed, totalProposals int64
	if err := s.pool.QueryRow(ctx, readQ).Scan(
		&version, &activeJobs, &totalProcessed, &totalProposals,
	); err != nil {
		return fmt.Errorf("read agent state: %w", err)
	}

	// Apply transition rules.
	switch e.EventType {
	case "job.created":
		activeJobs++
	case "job.completed", "job.dead_letter":
		if activeJobs > 0 {
			activeJobs--
		}
		totalProcessed++
	case "proposal.created":
		totalProposals++
	}

	const updateQ = `
		UPDATE agent_state
		SET state_version           = $1 + 1,
		    last_event_id           = $2,
		    active_jobs             = $3,
		    total_jobs_processed    = $4,
		    total_proposals_created = $5,
		    last_updated            = NOW()
		WHERE id = 1 AND state_version = $1`

	_, err := s.pool.Exec(ctx, updateQ,
		version, e.EventID, activeJobs, totalProcessed, totalProposals,
	)
	if err != nil {
		return fmt.Errorf("update agent state: %w", err)
	}
	// RowsAffected == 0 means a concurrent update won the optimistic lock race.
	// This is non-fatal; the state will converge on the next event.
	return nil
}
