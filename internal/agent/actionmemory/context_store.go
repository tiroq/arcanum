package actionmemory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UpdateContext increments counters and recomputes rates for a contextual outcome.
// Uses UPSERT to atomically create or update the record.
func (s *Store) UpdateContext(ctx context.Context, o ContextOutcomeInput) error {
	now := time.Now().UTC()
	succInc, failInc, neutralInc := outcomeIncrements(o.OutcomeStatus)

	const q = `
INSERT INTO agent_action_memory_context
(id, action_type, goal_type, job_type, failure_bucket, backlog_bucket,
 total_runs, success_runs, failure_runs, neutral_runs, success_rate, failure_rate, last_updated)
VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9, $7::float, $8::float, $10)
ON CONFLICT (action_type, goal_type, job_type, failure_bucket, backlog_bucket) DO UPDATE SET
total_runs   = agent_action_memory_context.total_runs + 1,
success_runs = agent_action_memory_context.success_runs + $7,
failure_runs = agent_action_memory_context.failure_runs + $8,
neutral_runs = agent_action_memory_context.neutral_runs + $9,
success_rate = (agent_action_memory_context.success_runs + $7)::float / (agent_action_memory_context.total_runs + 1)::float,
failure_rate = (agent_action_memory_context.failure_runs + $8)::float / (agent_action_memory_context.total_runs + 1)::float,
last_updated = $10`

	_, err := s.db.Exec(ctx, q,
		uuid.New(), o.ActionType, o.GoalType, o.JobType,
		o.FailureBucket, o.BacklogBucket,
		succInc, failInc, neutralInc, now,
	)
	if err != nil {
		return fmt.Errorf("upsert context memory: %w", err)
	}
	return nil
}

// ListContextRecords returns all contextual memory records.
func (s *Store) ListContextRecords(ctx context.Context) ([]ContextMemoryRecord, error) {
	const q = `
SELECT id, action_type, goal_type, job_type, failure_bucket, backlog_bucket,
       total_runs, success_runs, failure_runs, neutral_runs, success_rate, failure_rate, last_updated
FROM agent_action_memory_context
ORDER BY total_runs DESC
LIMIT 10000`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query context memory: %w", err)
	}
	defer rows.Close()

	var records []ContextMemoryRecord
	for rows.Next() {
		var r ContextMemoryRecord
		if err := rows.Scan(&r.ID, &r.ActionType, &r.GoalType, &r.JobType,
			&r.FailureBucket, &r.BacklogBucket,
			&r.TotalRuns, &r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
			&r.SuccessRate, &r.FailureRate, &r.LastUpdated); err != nil {
			return nil, fmt.Errorf("scan context memory: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetContextRecord returns the exact-match contextual record, or nil.
func (s *Store) GetContextRecord(ctx context.Context, actionType, goalType, jobType, failureBucket, backlogBucket string) (*ContextMemoryRecord, error) {
	const q = `
SELECT id, action_type, goal_type, job_type, failure_bucket, backlog_bucket,
       total_runs, success_runs, failure_runs, neutral_runs, success_rate, failure_rate, last_updated
FROM agent_action_memory_context
WHERE action_type = $1 AND goal_type = $2 AND job_type = $3
  AND failure_bucket = $4 AND backlog_bucket = $5
LIMIT 1`

	var r ContextMemoryRecord
	err := s.db.QueryRow(ctx, q, actionType, goalType, jobType, failureBucket, backlogBucket).Scan(
		&r.ID, &r.ActionType, &r.GoalType, &r.JobType,
		&r.FailureBucket, &r.BacklogBucket,
		&r.TotalRuns, &r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns,
		&r.SuccessRate, &r.FailureRate, &r.LastUpdated)
	if err != nil {
		return nil, nil // no record
	}
	return &r, nil
}

// GetPartialContextRecord returns a partial-match record (action_type + goal_type only).
func (s *Store) GetPartialContextRecord(ctx context.Context, actionType, goalType string) (*ContextMemoryRecord, error) {
	const q = `
SELECT action_type, goal_type,
       SUM(total_runs) AS total_runs,
       SUM(success_runs) AS success_runs,
       SUM(failure_runs) AS failure_runs,
       SUM(neutral_runs) AS neutral_runs
FROM agent_action_memory_context
WHERE action_type = $1 AND goal_type = $2
GROUP BY action_type, goal_type
HAVING SUM(total_runs) > 0
LIMIT 1`

	var r ContextMemoryRecord
	err := s.db.QueryRow(ctx, q, actionType, goalType).Scan(
		&r.ActionType, &r.GoalType,
		&r.TotalRuns, &r.SuccessRuns, &r.FailureRuns, &r.NeutralRuns)
	if err != nil {
		return nil, nil // no data
	}
	if r.TotalRuns > 0 {
		r.SuccessRate = float64(r.SuccessRuns) / float64(r.TotalRuns)
		r.FailureRate = float64(r.FailureRuns) / float64(r.TotalRuns)
	}
	return &r, nil
}
