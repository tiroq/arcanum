// Package goals implements the Goal Engine — a read-only advisory component
// that observes system state, derives goals, and produces action recommendations.
//
// SAFE MODE: The goal engine NEVER mutates system state. It performs only reads
// and returns deterministic, data-driven goals. No DB writes, no queue mutations,
// no provider calls, no side effects.
package goals

import "time"

// GoalType enumerates the categories of goals the engine can derive.
type GoalType string

const (
	GoalReduceRetryRate     GoalType = "reduce_retry_rate"
	GoalInvestigateFailures GoalType = "investigate_failed_jobs"
	GoalImproveModelQuality GoalType = "increase_model_quality"
	GoalReduceLatency       GoalType = "reduce_latency"
	GoalResolveBacklog      GoalType = "resolve_queue_backlog"
	GoalIncreaseReliability GoalType = "increase_reliability"
)

// Goal is a single advisory recommendation derived from system state.
// Goals are ephemeral — they are recomputed on every evaluation cycle and
// never persisted to the database.
type Goal struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Priority    float64        `json:"priority"`
	Confidence  float64        `json:"confidence"`
	Description string         `json:"description"`
	Evidence    map[string]any `json:"evidence"`
	CreatedAt   time.Time      `json:"created_at"`
}

// SystemSnapshot is a point-in-time read of all data sources the evaluator
// inspects. Collected once per evaluation cycle; individual rules never query
// the database directly.
type SystemSnapshot struct {
	// QueueStats maps status to count (queued, leased, retry_scheduled, failed, dead_letter).
	QueueStats map[string]int64

	// TotalJobsRecent is the total number of jobs completed or failed in the
	// observation window. Used to compute rates.
	TotalJobsRecent int64

	// FailedJobsRecent is the count of jobs that ended in failure (not dead_letter)
	// in the observation window.
	FailedJobsRecent int64

	// DeadLetterRecent is the count of jobs moved to dead_letter in the window.
	DeadLetterRecent int64

	// SucceededJobsRecent counts jobs with status succeeded in the window.
	SucceededJobsRecent int64

	// AcceptedProposals is the count of approved proposals in the window.
	AcceptedProposals int64

	// RejectedProposals is the count of rejected proposals in the window.
	RejectedProposals int64

	// TotalProposals is accepted + rejected (terminal proposals only).
	TotalProposals int64

	// AvgLatencyMS is the average processing run duration in the window.
	AvgLatencyMS float64
}
