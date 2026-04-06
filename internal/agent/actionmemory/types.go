package actionmemory

import (
	"time"

	"github.com/google/uuid"
)

// ActionMemoryRecord holds aggregate statistics for a specific action_type + target_type pair.
type ActionMemoryRecord struct {
	ID          uuid.UUID `json:"id"`
	ActionType  string    `json:"action_type"`
	TargetType  string    `json:"target_type"`
	TotalRuns   int       `json:"total_runs"`
	SuccessRuns int       `json:"success_runs"`
	FailureRuns int       `json:"failure_runs"`
	NeutralRuns int       `json:"neutral_runs"`
	SuccessRate float64   `json:"success_rate"`
	FailureRate float64   `json:"failure_rate"`
	LastUpdated time.Time `json:"last_updated"`
}

// TargetMemoryRecord holds per-target aggregate statistics.
type TargetMemoryRecord struct {
	ID          uuid.UUID `json:"id"`
	ActionType  string    `json:"action_type"`
	TargetType  string    `json:"target_type"`
	TargetID    uuid.UUID `json:"target_id"`
	TotalRuns   int       `json:"total_runs"`
	SuccessRuns int       `json:"success_runs"`
	FailureRuns int       `json:"failure_runs"`
	NeutralRuns int       `json:"neutral_runs"`
	SuccessRate float64   `json:"success_rate"`
	FailureRate float64   `json:"failure_rate"`
	LastUpdated time.Time `json:"last_updated"`
}
