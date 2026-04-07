package actionmemory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UpdateProviderContext increments counters and recomputes rates for a
// provider-contextual outcome. Uses UPSERT to atomically create or update.
func (s *Store) UpdateProviderContext(ctx context.Context, o ProviderContextOutcomeInput) error {
	now := time.Now().UTC()
	succInc, failInc, neutralInc := outcomeIncrements(o.OutcomeStatus)

	const q = `
INSERT INTO agent_action_memory_provider_context
(id, action_type, goal_type, job_type, provider_name, model_role,
 failure_bucket, backlog_bucket,
 total_runs, success_runs, failure_runs, neutral_runs,
 success_rate, failure_rate, last_updated)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $10, $11, $9::float, $10::float, $12)
ON CONFLICT (action_type, goal_type, job_type, provider_name, model_role, failure_bucket, backlog_bucket) DO UPDATE SET
total_runs   = agent_action_memory_provider_context.total_runs + 1,
success_runs = agent_action_memory_provider_context.success_runs + $9,
failure_runs = agent_action_memory_provider_context.failure_runs + $10,
neutral_runs = agent_action_memory_provider_context.neutral_runs + $11,
success_rate = (agent_action_memory_provider_context.success_runs + $9)::float
             / (agent_action_memory_provider_context.total_runs + 1)::float,
failure_rate = (agent_action_memory_provider_context.failure_runs + $10)::float
             / (agent_action_memory_provider_context.total_runs + 1)::float,
last_updated = $12`

	_, err := s.db.Exec(ctx, q,
		uuid.New(), o.ActionType, o.GoalType, o.JobType,
		o.ProviderName, o.ModelRole,
		o.FailureBucket, o.BacklogBucket,
		succInc, failInc, neutralInc, now,
	)
	if err != nil {
		return fmt.Errorf("upsert provider context memory: %w", err)
	}
	return nil
}

// ListProviderContextRecords returns all provider-context memory records.
// Optional filters: providerName and actionType (empty string = no filter).
func (s *Store) ListProviderContextRecords(ctx context.Context, providerName, actionType string) ([]ProviderContextMemoryRecord, error) {
	q := `
SELECT id, action_type, goal_type, job_type, provider_name, model_role,
       failure_bucket, backlog_bucket,
       total_runs, success_runs, failure_runs, neutral_runs,
       success_rate, failure_rate, last_updated
FROM agent_action_memory_provider_context
WHERE 1=1`
	args := []any{}
	argIdx := 1

	if providerName != "" {
		q += fmt.Sprintf(" AND provider_name = $%d", argIdx)
		args = append(args, providerName)
		argIdx++
	}
	if actionType != "" {
		q += fmt.Sprintf(" AND action_type = $%d", argIdx)
		args = append(args, actionType)
		argIdx++
	}
	_ = argIdx // suppress unused

	q += " ORDER BY total_runs DESC LIMIT 10000"

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query provider context memory: %w", err)
	}
	defer rows.Close()

	var records []ProviderContextMemoryRecord
	for rows.Next() {
		var r ProviderContextMemoryRecord
		if err := rows.Scan(&r.ID, &r.ActionType, &r.GoalType, &r.JobType,
			&r.ProviderName, &r.ModelRole,
			&r.FailureBucket, &r.BacklogBucket,
			&r.TotalRuns, &r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
			&r.SuccessRate, &r.FailureRate, &r.LastUpdated); err != nil {
			return nil, fmt.Errorf("scan provider context memory: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetProviderContextRecord returns the exact-match provider-context record, or nil.
func (s *Store) GetProviderContextRecord(ctx context.Context, actionType, goalType, providerName, modelRole, failureBucket, backlogBucket string) (*ProviderContextMemoryRecord, error) {
	const q = `
SELECT id, action_type, goal_type, job_type, provider_name, model_role,
       failure_bucket, backlog_bucket,
       total_runs, success_runs, failure_runs, neutral_runs,
       success_rate, failure_rate, last_updated
FROM agent_action_memory_provider_context
WHERE action_type = $1 AND goal_type = $2
  AND provider_name = $3 AND model_role = $4
  AND failure_bucket = $5 AND backlog_bucket = $6
LIMIT 1`

	var r ProviderContextMemoryRecord
	err := s.db.QueryRow(ctx, q, actionType, goalType, providerName, modelRole, failureBucket, backlogBucket).Scan(
		&r.ID, &r.ActionType, &r.GoalType, &r.JobType,
		&r.ProviderName, &r.ModelRole,
		&r.FailureBucket, &r.BacklogBucket,
		&r.TotalRuns, &r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
		&r.SuccessRate, &r.FailureRate, &r.LastUpdated)
	if err != nil {
		return nil, nil
	}
	return &r, nil
}

// GetPartialProviderContextRecord returns aggregated stats for action_type +
// goal_type + provider_name (across all other dimensions).
func (s *Store) GetPartialProviderContextRecord(ctx context.Context, actionType, goalType, providerName string) (*ProviderContextMemoryRecord, error) {
	const q = `
SELECT action_type, goal_type, provider_name,
       SUM(total_runs) AS total_runs,
       SUM(success_runs) AS success_runs,
       SUM(failure_runs) AS failure_runs,
       SUM(neutral_runs) AS neutral_runs
FROM agent_action_memory_provider_context
WHERE action_type = $1 AND goal_type = $2 AND provider_name = $3
GROUP BY action_type, goal_type, provider_name
HAVING SUM(total_runs) > 0
LIMIT 1`

	var r ProviderContextMemoryRecord
	err := s.db.QueryRow(ctx, q, actionType, goalType, providerName).Scan(
		&r.ActionType, &r.GoalType, &r.ProviderName,
		&r.TotalRuns, &r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns)
	if err != nil {
		return nil, nil
	}
	if r.TotalRuns > 0 {
		r.SuccessRate = float64(r.SuccessRuns) / float64(r.TotalRuns)
		r.FailureRate = float64(r.FailureRuns) / float64(r.TotalRuns)
	}
	return &r, nil
}
