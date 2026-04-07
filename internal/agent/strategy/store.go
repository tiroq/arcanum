package strategy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists strategy plans to the database.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Save persists a strategy decision (all candidate strategies + selection).
func (s *Store) Save(ctx context.Context, decision StrategyDecision) error {
	for _, plan := range decision.CandidateStrategies {
		stepsJSON, err := json.Marshal(plan.Steps)
		if err != nil {
			return fmt.Errorf("marshal strategy steps: %w", err)
		}
		_, err = s.db.Exec(ctx, `
			INSERT INTO agent_strategy_plans
				(id, goal_id, goal_type, strategy_type, steps,
				 expected_utility, risk_score, confidence, explanation,
				 exploratory, selected, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`,
			plan.ID, plan.GoalID, plan.GoalType, string(plan.StrategyType),
			stepsJSON, plan.ExpectedUtility, plan.RiskScore, plan.Confidence,
			plan.Explanation, plan.Exploratory, plan.Selected, plan.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert strategy plan: %w", err)
		}
	}
	return nil
}

// ListRecent returns the most recent strategy plans, newest first.
func (s *Store) ListRecent(ctx context.Context, limit, offset int) ([]StrategyPlan, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, goal_id, goal_type, strategy_type, steps,
		       expected_utility, risk_score, confidence, explanation,
		       exploratory, selected, created_at
		FROM agent_strategy_plans
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list strategy plans: %w", err)
	}
	defer rows.Close()
	return scanPlans(rows)
}

// ListByGoal returns strategy plans for a specific goal.
func (s *Store) ListByGoal(ctx context.Context, goalID string) ([]StrategyPlan, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, goal_id, goal_type, strategy_type, steps,
		       expected_utility, risk_score, confidence, explanation,
		       exploratory, selected, created_at
		FROM agent_strategy_plans
		WHERE goal_id = $1
		ORDER BY created_at DESC
	`, goalID)
	if err != nil {
		return nil, fmt.Errorf("list strategies by goal: %w", err)
	}
	defer rows.Close()
	return scanPlans(rows)
}

// ListSelected returns recently selected strategies only.
func (s *Store) ListSelected(ctx context.Context, limit, offset int) ([]StrategyPlan, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, goal_id, goal_type, strategy_type, steps,
		       expected_utility, risk_score, confidence, explanation,
		       exploratory, selected, created_at
		FROM agent_strategy_plans
		WHERE selected = true
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list selected strategies: %w", err)
	}
	defer rows.Close()
	return scanPlans(rows)
}

type scannable interface {
	Next() bool
	Scan(dest ...any) error
}

func scanPlans(rows scannable) ([]StrategyPlan, error) {
	var plans []StrategyPlan
	for rows.Next() {
		var (
			p     StrategyPlan
			steps json.RawMessage
		)
		err := rows.Scan(
			&p.ID, &p.GoalID, &p.GoalType, &p.StrategyType, &steps,
			&p.ExpectedUtility, &p.RiskScore, &p.Confidence, &p.Explanation,
			&p.Exploratory, &p.Selected, &p.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan strategy plan: %w", err)
		}
		if err := json.Unmarshal(steps, &p.Steps); err != nil {
			return nil, fmt.Errorf("unmarshal strategy steps: %w", err)
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// GetByID returns a strategy plan by ID.
func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (*StrategyPlan, error) {
	var (
		p     StrategyPlan
		steps json.RawMessage
	)
	err := s.db.QueryRow(ctx, `
		SELECT id, goal_id, goal_type, strategy_type, steps,
		       expected_utility, risk_score, confidence, explanation,
		       exploratory, selected, created_at
		FROM agent_strategy_plans
		WHERE id = $1
	`, id).Scan(
		&p.ID, &p.GoalID, &p.GoalType, &p.StrategyType, &steps,
		&p.ExpectedUtility, &p.RiskScore, &p.Confidence, &p.Explanation,
		&p.Exploratory, &p.Selected, &p.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get strategy plan: %w", err)
	}
	if err := json.Unmarshal(steps, &p.Steps); err != nil {
		return nil, fmt.Errorf("unmarshal strategy steps: %w", err)
	}
	return &p, nil
}
