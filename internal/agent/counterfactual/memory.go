package counterfactual

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- SimulationStore ---

// SimulationStore persists and retrieves counterfactual simulation results.
type SimulationStore struct {
	db *pgxpool.Pool
}

// NewSimulationStore creates a SimulationStore backed by PostgreSQL.
func NewSimulationStore(db *pgxpool.Pool) *SimulationStore {
	return &SimulationStore{db: db}
}

// SaveSimulation persists a simulation result.
func (s *SimulationStore) SaveSimulation(ctx context.Context, sim SimulationResult) error {
	predictionsJSON, err := json.Marshal(sim.Predictions)
	if err != nil {
		return err
	}

	const q = `
		INSERT INTO agent_counterfactual_simulations
			(id, decision_id, goal_type, predictions, prediction_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (decision_id) DO NOTHING`

	_, err = s.db.Exec(ctx, q,
		uuid.New(), sim.DecisionID, sim.GoalType,
		predictionsJSON, len(sim.Predictions), sim.CreatedAt,
	)
	return err
}

// GetSimulation retrieves the simulation result for a given decision ID.
// Returns nil if not found.
func (s *SimulationStore) GetSimulation(ctx context.Context, decisionID string) (*SimulationResult, error) {
	const q = `
		SELECT decision_id, goal_type, predictions, created_at
		FROM agent_counterfactual_simulations
		WHERE decision_id = $1`

	var sim SimulationResult
	var predictionsJSON []byte
	err := s.db.QueryRow(ctx, q, decisionID).Scan(
		&sim.DecisionID, &sim.GoalType,
		&predictionsJSON, &sim.CreatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(predictionsJSON, &sim.Predictions); err != nil {
		return nil, err
	}
	return &sim, nil
}

// ListSimulations returns recent simulation results.
func (s *SimulationStore) ListSimulations(ctx context.Context, limit, offset int) ([]SimulationResult, error) {
	const q = `
		SELECT decision_id, goal_type, predictions, created_at
		FROM agent_counterfactual_simulations
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.db.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SimulationResult
	for rows.Next() {
		var sim SimulationResult
		var predictionsJSON []byte
		if err := rows.Scan(
			&sim.DecisionID, &sim.GoalType,
			&predictionsJSON, &sim.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(predictionsJSON, &sim.Predictions); err != nil {
			return nil, err
		}
		results = append(results, sim)
	}
	return results, nil
}

// ListSimulationsByGoalType returns simulation results filtered by goal type.
func (s *SimulationStore) ListSimulationsByGoalType(ctx context.Context, goalType string, limit, offset int) ([]SimulationResult, error) {
	const q = `
		SELECT decision_id, goal_type, predictions, created_at
		FROM agent_counterfactual_simulations
		WHERE goal_type = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.db.Query(ctx, q, goalType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SimulationResult
	for rows.Next() {
		var sim SimulationResult
		var predictionsJSON []byte
		if err := rows.Scan(
			&sim.DecisionID, &sim.GoalType,
			&predictionsJSON, &sim.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(predictionsJSON, &sim.Predictions); err != nil {
			return nil, err
		}
		results = append(results, sim)
	}
	return results, nil
}

// --- PredictionOutcomeStore ---

// PredictionOutcomeStore persists and retrieves prediction outcomes (errors).
type PredictionOutcomeStore struct {
	db *pgxpool.Pool
}

// NewPredictionOutcomeStore creates a PredictionOutcomeStore backed by PostgreSQL.
func NewPredictionOutcomeStore(db *pgxpool.Pool) *PredictionOutcomeStore {
	return &PredictionOutcomeStore{db: db}
}

// SaveOutcome persists a prediction outcome.
func (s *PredictionOutcomeStore) SaveOutcome(ctx context.Context, o PredictionOutcome) error {
	const q = `
		INSERT INTO agent_counterfactual_prediction_outcomes
			(id, decision_id, path_signature, goal_type,
			 predicted_value, actual_value, absolute_error, direction_correct,
			 created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (decision_id) DO NOTHING`

	_, err := s.db.Exec(ctx, q,
		uuid.New(), o.DecisionID, o.PathSignature, o.GoalType,
		o.PredictedValue, o.ActualValue, o.AbsoluteError, o.DirectionCorrect,
		o.CreatedAt,
	)
	return err
}

// ListOutcomes returns recent prediction outcomes.
func (s *PredictionOutcomeStore) ListOutcomes(ctx context.Context, limit, offset int) ([]PredictionOutcome, error) {
	const q = `
		SELECT decision_id, path_signature, goal_type,
		       predicted_value, actual_value, absolute_error, direction_correct,
		       created_at
		FROM agent_counterfactual_prediction_outcomes
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.db.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []PredictionOutcome
	for rows.Next() {
		var o PredictionOutcome
		if err := rows.Scan(
			&o.DecisionID, &o.PathSignature, &o.GoalType,
			&o.PredictedValue, &o.ActualValue, &o.AbsoluteError, &o.DirectionCorrect,
			&o.CreatedAt,
		); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

// ListOutcomesByGoalType returns prediction outcomes filtered by goal type.
func (s *PredictionOutcomeStore) ListOutcomesByGoalType(ctx context.Context, goalType string, limit, offset int) ([]PredictionOutcome, error) {
	const q = `
		SELECT decision_id, path_signature, goal_type,
		       predicted_value, actual_value, absolute_error, direction_correct,
		       created_at
		FROM agent_counterfactual_prediction_outcomes
		WHERE goal_type = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.db.Query(ctx, q, goalType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []PredictionOutcome
	for rows.Next() {
		var o PredictionOutcome
		if err := rows.Scan(
			&o.DecisionID, &o.PathSignature, &o.GoalType,
			&o.PredictedValue, &o.ActualValue, &o.AbsoluteError, &o.DirectionCorrect,
			&o.CreatedAt,
		); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

// --- PredictionMemoryStore ---

// PredictionMemoryStore persists and retrieves accumulated prediction accuracy.
type PredictionMemoryStore struct {
	db *pgxpool.Pool
}

// NewPredictionMemoryStore creates a PredictionMemoryStore backed by PostgreSQL.
func NewPredictionMemoryStore(db *pgxpool.Pool) *PredictionMemoryStore {
	return &PredictionMemoryStore{db: db}
}

// RecordPrediction updates prediction memory for a path + goal.
func (s *PredictionMemoryStore) RecordPrediction(ctx context.Context, pathSignature, goalType string, absoluteError float64, directionCorrect bool) error {
	now := time.Now().UTC()
	dirCorrectInc := 0
	if directionCorrect {
		dirCorrectInc = 1
	}

	const q = `
		INSERT INTO agent_counterfactual_prediction_memory
			(id, path_signature, goal_type, total_predictions, total_error,
			 avg_error, direction_correct_count, direction_accuracy, last_updated)
		VALUES ($1, $2, $3, 1, $4, $4, $5, $5::float, $6)
		ON CONFLICT (path_signature, goal_type) DO UPDATE SET
			total_predictions = agent_counterfactual_prediction_memory.total_predictions + 1,
			total_error       = agent_counterfactual_prediction_memory.total_error + $4,
			avg_error         = (agent_counterfactual_prediction_memory.total_error + $4)
			                  / (agent_counterfactual_prediction_memory.total_predictions + 1)::float,
			direction_correct_count = agent_counterfactual_prediction_memory.direction_correct_count + $5,
			direction_accuracy = (agent_counterfactual_prediction_memory.direction_correct_count + $5)::float
			                   / (agent_counterfactual_prediction_memory.total_predictions + 1)::float,
			last_updated       = $6`

	_, err := s.db.Exec(ctx, q,
		uuid.New(), pathSignature, goalType,
		absoluteError, dirCorrectInc, now,
	)
	return err
}

// GetMemory retrieves prediction memory for a path + goal pair.
// Returns nil if not found.
func (s *PredictionMemoryStore) GetMemory(ctx context.Context, pathSignature, goalType string) (*PredictionMemoryRecord, error) {
	const q = `
		SELECT path_signature, goal_type, total_predictions, total_error,
		       avg_error, direction_correct_count, direction_accuracy, last_updated
		FROM agent_counterfactual_prediction_memory
		WHERE path_signature = $1 AND goal_type = $2`

	var r PredictionMemoryRecord
	err := s.db.QueryRow(ctx, q, pathSignature, goalType).Scan(
		&r.PathSignature, &r.GoalType,
		&r.TotalPredictions, &r.TotalError,
		&r.AvgError, &r.DirectionCorrectCount, &r.DirectionAccuracy,
		&r.LastUpdated,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ListMemory returns all prediction memory records.
func (s *PredictionMemoryStore) ListMemory(ctx context.Context) ([]PredictionMemoryRecord, error) {
	const q = `
		SELECT path_signature, goal_type, total_predictions, total_error,
		       avg_error, direction_correct_count, direction_accuracy, last_updated
		FROM agent_counterfactual_prediction_memory
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PredictionMemoryRecord
	for rows.Next() {
		var r PredictionMemoryRecord
		if err := rows.Scan(
			&r.PathSignature, &r.GoalType,
			&r.TotalPredictions, &r.TotalError,
			&r.AvgError, &r.DirectionCorrectCount, &r.DirectionAccuracy,
			&r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// ListMemoryByGoalType returns prediction memory records filtered by goal type.
func (s *PredictionMemoryStore) ListMemoryByGoalType(ctx context.Context, goalType string) ([]PredictionMemoryRecord, error) {
	const q = `
		SELECT path_signature, goal_type, total_predictions, total_error,
		       avg_error, direction_correct_count, direction_accuracy, last_updated
		FROM agent_counterfactual_prediction_memory
		WHERE goal_type = $1
		ORDER BY last_updated DESC`

	rows, err := s.db.Query(ctx, q, goalType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PredictionMemoryRecord
	for rows.Next() {
		var r PredictionMemoryRecord
		if err := rows.Scan(
			&r.PathSignature, &r.GoalType,
			&r.TotalPredictions, &r.TotalError,
			&r.AvgError, &r.DirectionCorrectCount, &r.DirectionAccuracy,
			&r.LastUpdated,
		); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}
