package actionmemory

import (
	"time"

	"github.com/google/uuid"
)

// ProviderContext captures the provider/backend dimensions for learning.
type ProviderContext struct {
	ProviderName string `json:"provider_name"` // ollama, openrouter, ollama-cloud
	ModelRole    string `json:"model_role"`    // fast, default, planner, review
}

// ProviderContextMemoryRecord holds aggregate statistics for a specific
// provider-context combination.
type ProviderContextMemoryRecord struct {
	ID            uuid.UUID `json:"id"`
	ActionType    string    `json:"action_type"`
	GoalType      string    `json:"goal_type"`
	JobType       string    `json:"job_type"`
	ProviderName  string    `json:"provider_name"`
	ModelRole     string    `json:"model_role"`
	FailureBucket string    `json:"failure_bucket"`
	BacklogBucket string    `json:"backlog_bucket"`
	TotalRuns     int       `json:"total_runs"`
	SuccessRuns   int       `json:"success_runs"`
	FailureRuns   int       `json:"failure_runs"`
	NeutralRuns   int       `json:"neutral_runs"`
	SuccessRate   float64   `json:"success_rate"`
	FailureRate   float64   `json:"failure_rate"`
	LastUpdated   time.Time `json:"last_updated"`
}

// ProviderContextOutcomeInput holds the fields needed to update provider-context memory.
type ProviderContextOutcomeInput struct {
	ActionType    string
	GoalType      string
	JobType       string
	ProviderName  string
	ModelRole     string
	FailureBucket string
	BacklogBucket string
	OutcomeStatus string // "success", "neutral", "failure"
}
