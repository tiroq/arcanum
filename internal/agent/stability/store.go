package stability

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists and retrieves the current stability state.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates a Store backed by PostgreSQL.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Get retrieves the current stability state. There is always exactly one row.
func (s *Store) Get(ctx context.Context) (*State, error) {
	const q = `
SELECT id, mode, throttle_multiplier, blocked_action_types, reason, updated_at
FROM agent_stability_state
ORDER BY updated_at DESC
LIMIT 1`

	var st State
	var blockedJSON []byte
	var mode string
	err := s.db.QueryRow(ctx, q).Scan(
		&st.ID, &mode, &st.ThrottleMultiplier,
		&blockedJSON, &st.Reason, &st.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get stability state: %w", err)
	}
	st.Mode = Mode(mode)
	if err := json.Unmarshal(blockedJSON, &st.BlockedActionTypes); err != nil {
		st.BlockedActionTypes = nil
	}
	return &st, nil
}

// Update overwrites the current stability state.
func (s *Store) Update(ctx context.Context, st *State) error {
	blockedJSON, err := json.Marshal(st.BlockedActionTypes)
	if err != nil {
		return fmt.Errorf("marshal blocked types: %w", err)
	}

	now := time.Now().UTC()
	const q = `
UPDATE agent_stability_state
SET mode = $1,
    throttle_multiplier = $2,
    blocked_action_types = $3,
    reason = $4,
    updated_at = $5
WHERE id = (SELECT id FROM agent_stability_state LIMIT 1)`

	_, err = s.db.Exec(ctx, q,
		string(st.Mode), st.ThrottleMultiplier,
		blockedJSON, st.Reason, now,
	)
	if err != nil {
		return fmt.Errorf("update stability state: %w", err)
	}
	st.UpdatedAt = now
	return nil
}

// Reset restores the system to normal mode.
func (s *Store) Reset(ctx context.Context, reason string) (*State, error) {
	st := &State{
		Mode:               ModeNormal,
		ThrottleMultiplier: 1.0,
		BlockedActionTypes: []string{},
		Reason:             reason,
	}
	if err := s.Update(ctx, st); err != nil {
		return nil, err
	}
	// Re-read to get the actual row.
	return s.Get(ctx)
}
