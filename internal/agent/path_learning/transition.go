package pathlearning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TransitionStore persists and retrieves transition memory records.
type TransitionStore struct {
	db *pgxpool.Pool
}

// NewTransitionStore creates a TransitionStore backed by PostgreSQL.
func NewTransitionStore(db *pgxpool.Pool) *TransitionStore {
	return &TransitionStore{db: db}
}

// --- Transition Memory UPSERT ---

// UpdateTransitionMemory increments counters and recomputes rates for a transition.
// Uses UPSERT to atomically create or update the record.
// helpfulness: "helpful", "unhelpful", or "neutral".
func (s *TransitionStore) UpdateTransitionMemory(ctx context.Context, goalType, fromAction, toAction, helpfulness string) error {
	now := time.Now().UTC()
	transitionKey := BuildTransitionKey(fromAction, toAction)

	helpfulInc, unhelpfulInc, neutralInc := helpfulnessIncrements(helpfulness)

	const q = `
		INSERT INTO agent_transition_memory
			(id, goal_type, from_action_type, to_action_type, transition_key,
			 total_uses, helpful_uses, unhelpful_uses, neutral_uses,
			 helpful_rate, unhelpful_rate, last_updated)
		VALUES ($1, $2, $3, $4, $5, 1, $6, $7, $8, $6::float, $7::float, $9)
		ON CONFLICT (goal_type, transition_key) DO UPDATE SET
			total_uses    = agent_transition_memory.total_uses + 1,
			helpful_uses  = agent_transition_memory.helpful_uses + $6,
			unhelpful_uses = agent_transition_memory.unhelpful_uses + $7,
			neutral_uses  = agent_transition_memory.neutral_uses + $8,
			helpful_rate  = (agent_transition_memory.helpful_uses + $6)::float
			              / (agent_transition_memory.total_uses + 1)::float,
			unhelpful_rate = (agent_transition_memory.unhelpful_uses + $7)::float
			               / (agent_transition_memory.total_uses + 1)::float,
			last_updated  = $9`

	_, err := s.db.Exec(ctx, q, uuid.New(), goalType, fromAction, toAction, transitionKey,
		helpfulInc, unhelpfulInc, neutralInc, now)
	return err
}

// GetTransitionMemory retrieves the memory record for a goal_type + transition_key pair.
// Returns nil if no record exists.
func (s *TransitionStore) GetTransitionMemory(ctx context.Context, goalType, transitionKey string) (*TransitionMemoryRecord, error) {
	const q = `
		SELECT id, goal_type, from_action_type, to_action_type, transition_key,
		       total_uses, helpful_uses, unhelpful_uses, neutral_uses,
		       helpful_rate, unhelpful_rate, last_updated
		FROM agent_transition_memory
		WHERE goal_type = $1 AND transition_key = $2`

	var r TransitionMemoryRecord
	err := s.db.QueryRow(ctx, q, goalType, transitionKey).Scan(
		&r.ID, &r.GoalType, &r.FromActionType, &r.ToActionType, &r.TransitionKey,
		&r.TotalUses, &r.HelpfulUses, &r.UnhelpfulUses, &r.NeutralUses,
		&r.HelpfulRate, &r.UnhelpfulRate, &r.LastUpdated,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ListTransitionMemory returns all transition memory records ordered by last_updated DESC.
func (s *TransitionStore) ListTransitionMemory(ctx context.Context) ([]TransitionMemoryRecord, error) {
	const q = `
		SELECT id, goal_type, from_action_type, to_action_type, transition_key,
		       total_uses, helpful_uses, unhelpful_uses, neutral_uses,
		       helpful_rate, unhelpful_rate, last_updated
		FROM agent_transition_memory
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []TransitionMemoryRecord
	for rows.Next() {
		var r TransitionMemoryRecord
		if err := rows.Scan(
			&r.ID, &r.GoalType, &r.FromActionType, &r.ToActionType, &r.TransitionKey,
			&r.TotalUses, &r.HelpfulUses, &r.UnhelpfulUses, &r.NeutralUses,
			&r.HelpfulRate, &r.UnhelpfulRate, &r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// ListTransitionMemoryByGoalType returns transition memory records for a specific goal type.
func (s *TransitionStore) ListTransitionMemoryByGoalType(ctx context.Context, goalType string) ([]TransitionMemoryRecord, error) {
	const q = `
		SELECT id, goal_type, from_action_type, to_action_type, transition_key,
		       total_uses, helpful_uses, unhelpful_uses, neutral_uses,
		       helpful_rate, unhelpful_rate, last_updated
		FROM agent_transition_memory
		WHERE goal_type = $1
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q, goalType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []TransitionMemoryRecord
	for rows.Next() {
		var r TransitionMemoryRecord
		if err := rows.Scan(
			&r.ID, &r.GoalType, &r.FromActionType, &r.ToActionType, &r.TransitionKey,
			&r.TotalUses, &r.HelpfulUses, &r.UnhelpfulUses, &r.NeutralUses,
			&r.HelpfulRate, &r.UnhelpfulRate, &r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// --- Helpers ---

func helpfulnessIncrements(helpfulness string) (helpful, unhelpful, neutral int) {
	switch helpfulness {
	case "helpful":
		return 1, 0, 0
	case "unhelpful":
		return 0, 1, 0
	case "neutral":
		return 0, 0, 1
	default:
		return 0, 0, 1
	}
}
