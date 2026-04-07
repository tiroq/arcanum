package actionmemory

import (
	"time"

	"github.com/google/uuid"
)

// ActionContext defines the bounded context dimensions for contextual learning.
// Max 2 dimensions used per decision (goal_type + failure_bucket or backlog_bucket).
type ActionContext struct {
	GoalType      string `json:"goal_type"`
	ActionType    string `json:"action_type"`
	JobType       string `json:"job_type"`
	FailureBucket string `json:"failure_bucket"` // low / medium / high
	BacklogBucket string `json:"backlog_bucket"` // low / medium / high
}

// ContextMemoryRecord holds aggregate statistics for a specific context.
type ContextMemoryRecord struct {
	ID            uuid.UUID `json:"id"`
	ActionType    string    `json:"action_type"`
	GoalType      string    `json:"goal_type"`
	JobType       string    `json:"job_type"`
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

// ContextualFeedback extends the global feedback with contextual awareness.
type ContextualFeedback struct {
	ActionFeedback
	ContextMatch string `json:"context_match"` // "exact", "partial", "global", "default"
}

// --- Deterministic Bucketization ---

// BucketFailureRate returns a deterministic bucket for a failure rate.
func BucketFailureRate(rate float64) string {
	switch {
	case rate < 0.1:
		return "low"
	case rate < 0.3:
		return "medium"
	default:
		return "high"
	}
}

// BucketBacklog returns a deterministic bucket for a queue backlog count.
func BucketBacklog(count int) string {
	switch {
	case count < 20:
		return "low"
	case count < 50:
		return "medium"
	default:
		return "high"
	}
}

// ContextOutcomeInput extends OutcomeInput with context dimensions.
type ContextOutcomeInput struct {
	ActionType    string
	GoalType      string
	JobType       string
	FailureBucket string
	BacklogBucket string
	OutcomeStatus string // "success", "neutral", "failure"
}
