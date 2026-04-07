package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StoredDecision is a PlanningDecision persisted to the database with
// additional metadata (ID, CycleID).
type StoredDecision struct {
	ID             uuid.UUID                `json:"id"`
	CycleID        string                   `json:"cycle_id"`
	GoalID         string                   `json:"goal_id"`
	GoalType       string                   `json:"goal_type"`
	SelectedAction string                   `json:"selected_action"`
	Explanation    string                   `json:"explanation"`
	Candidates     []PlannedActionCandidate `json:"candidates"`
	PlannedAt      time.Time                `json:"planned_at"`
	CreatedAt      time.Time                `json:"created_at"`
}

// DecisionJournal persists PlanningDecision records to PostgreSQL for
// durable history and downstream reflection.
type DecisionJournal struct {
	db *pgxpool.Pool
}

// NewDecisionJournal creates a DecisionJournal.
func NewDecisionJournal(db *pgxpool.Pool) *DecisionJournal {
	return &DecisionJournal{db: db}
}

// Save persists a batch of planning decisions from a single cycle.
func (j *DecisionJournal) Save(ctx context.Context, cycleID string, decisions []PlanningDecision) error {
	if len(decisions) == 0 {
		return nil
	}

	const q = `
INSERT INTO agent_planning_decisions
(id, cycle_id, goal_id, goal_type, selected_action, explanation, candidates, planned_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	for _, d := range decisions {
		candidatesJSON, err := json.Marshal(d.Candidates)
		if err != nil {
			return fmt.Errorf("marshal candidates: %w", err)
		}

		_, err = j.db.Exec(ctx, q,
			uuid.New(),
			cycleID,
			d.GoalID,
			d.GoalType,
			d.SelectedActionType,
			d.Explanation,
			candidatesJSON,
			d.PlannedAt,
		)
		if err != nil {
			return fmt.Errorf("insert decision: %w", err)
		}
	}
	return nil
}

// ListRecent retrieves the most recent stored decisions, ordered by planned_at DESC.
func (j *DecisionJournal) ListRecent(ctx context.Context, limit int) ([]StoredDecision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	const q = `
SELECT id, cycle_id, goal_id, goal_type, selected_action, explanation,
       candidates, planned_at, created_at
FROM agent_planning_decisions
ORDER BY planned_at DESC
LIMIT $1`

	rows, err := j.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}
	defer rows.Close()

	var results []StoredDecision
	for rows.Next() {
		var d StoredDecision
		var candidatesJSON []byte
		if err := rows.Scan(
			&d.ID, &d.CycleID, &d.GoalID, &d.GoalType,
			&d.SelectedAction, &d.Explanation,
			&candidatesJSON, &d.PlannedAt, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}
		if err := json.Unmarshal(candidatesJSON, &d.Candidates); err != nil {
			d.Candidates = nil
		}
		results = append(results, d)
	}
	return results, rows.Err()
}

// ListRecentByActionType returns recent decisions where a specific action was selected.
func (j *DecisionJournal) ListRecentByActionType(ctx context.Context, actionType string, limit int) ([]StoredDecision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	const q = `
SELECT id, cycle_id, goal_id, goal_type, selected_action, explanation,
       candidates, planned_at, created_at
FROM agent_planning_decisions
WHERE selected_action = $1
ORDER BY planned_at DESC
LIMIT $2`

	rows, err := j.db.Query(ctx, q, actionType, limit)
	if err != nil {
		return nil, fmt.Errorf("query decisions by action: %w", err)
	}
	defer rows.Close()

	var results []StoredDecision
	for rows.Next() {
		var d StoredDecision
		var candidatesJSON []byte
		if err := rows.Scan(
			&d.ID, &d.CycleID, &d.GoalID, &d.GoalType,
			&d.SelectedAction, &d.Explanation,
			&candidatesJSON, &d.PlannedAt, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}
		if err := json.Unmarshal(candidatesJSON, &d.Candidates); err != nil {
			d.Candidates = nil
		}
		results = append(results, d)
	}
	return results, rows.Err()
}
