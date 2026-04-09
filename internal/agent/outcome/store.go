package outcome

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists and retrieves ActionOutcome records.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates a Store backed by PostgreSQL.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Save inserts a new ActionOutcome into the database.
func (s *Store) Save(ctx context.Context, o *ActionOutcome) error {
	beforeJSON, err := json.Marshal(o.BeforeState)
	if err != nil {
		return fmt.Errorf("marshal before_state: %w", err)
	}
	afterJSON, err := json.Marshal(o.AfterState)
	if err != nil {
		return fmt.Errorf("marshal after_state: %w", err)
	}

	const q = `
		INSERT INTO agent_action_outcomes
			(id, action_id, goal_id, action_type, target_type, target_id,
			 outcome_status, effect_detected, improvement, before_state, after_state, evaluated_at,
			 income_value, family_value, owner_relief_value, risk_cost, utility_score)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`

	_, err = s.db.Exec(ctx, q,
		o.ID, o.ActionID, o.GoalID, o.ActionType,
		o.TargetType, o.TargetID, string(o.OutcomeStatus),
		o.EffectDetected, o.Improvement,
		beforeJSON, afterJSON, o.EvaluatedAt,
		o.IncomeValue, o.FamilyValue, o.OwnerReliefValue, o.RiskCost, o.UtilityScore,
	)
	if err != nil {
		return fmt.Errorf("insert outcome: %w", err)
	}
	return nil
}

// ListFilter provides optional filtering for outcome queries.
type ListFilter struct {
	ActionID *uuid.UUID
	TargetID *uuid.UUID
	Limit    int
	Offset   int
}

// List retrieves outcomes with optional filters.
func (s *Store) List(ctx context.Context, f ListFilter) ([]ActionOutcome, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}

	query := `
		SELECT id, action_id, goal_id, action_type, target_type, target_id,
		       outcome_status, effect_detected, improvement, before_state, after_state, evaluated_at,
		       income_value, family_value, owner_relief_value, risk_cost, utility_score
		FROM agent_action_outcomes
		WHERE 1=1`
	args := []any{}
	argIdx := 1

	if f.ActionID != nil {
		query += fmt.Sprintf(" AND action_id = $%d", argIdx)
		args = append(args, *f.ActionID)
		argIdx++
	}
	if f.TargetID != nil {
		query += fmt.Sprintf(" AND target_id = $%d", argIdx)
		args = append(args, *f.TargetID)
		argIdx++
	}

	query += " ORDER BY evaluated_at DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query outcomes: %w", err)
	}
	defer rows.Close()

	var outcomes []ActionOutcome
	for rows.Next() {
		var o ActionOutcome
		var beforeJSON, afterJSON []byte
		if err := rows.Scan(
			&o.ID, &o.ActionID, &o.GoalID, &o.ActionType,
			&o.TargetType, &o.TargetID, &o.OutcomeStatus,
			&o.EffectDetected, &o.Improvement,
			&beforeJSON, &afterJSON, &o.EvaluatedAt,
			&o.IncomeValue, &o.FamilyValue, &o.OwnerReliefValue, &o.RiskCost, &o.UtilityScore,
		); err != nil {
			return nil, fmt.Errorf("scan outcome: %w", err)
		}
		if err := json.Unmarshal(beforeJSON, &o.BeforeState); err != nil {
			o.BeforeState = map[string]any{}
		}
		if err := json.Unmarshal(afterJSON, &o.AfterState); err != nil {
			o.AfterState = map[string]any{}
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, rows.Err()
}
