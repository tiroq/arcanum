package planning

import (
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// PlanningContext is a point-in-time snapshot of system state and learned
// action priors. Collected once per planning cycle — scoring functions
// never query the database directly.
type PlanningContext struct {
	QueueBacklog         int                                    `json:"queue_backlog"`
	RetryScheduledCount  int                                    `json:"retry_scheduled_count"`
	LeasedCount          int                                    `json:"leased_count"`
	FailureRate          float64                                `json:"failure_rate"`
	AcceptanceRate       float64                                `json:"acceptance_rate"`
	RecentActionFeedback map[string]actionmemory.ActionFeedback `json:"recent_action_feedback"`
	Timestamp            time.Time                              `json:"timestamp"`

	// Contextual policy adaptation fields (Iteration 12).
	// ContextRecords holds all contextual memory records, loaded once per cycle.
	ContextRecords []actionmemory.ContextMemoryRecord `json:"context_records,omitempty"`
	// FailureBucket is the deterministic bucket for the current system failure rate.
	FailureBucket string `json:"failure_bucket,omitempty"`
	// BacklogBucket is the deterministic bucket for the current queue backlog.
	BacklogBucket string `json:"backlog_bucket,omitempty"`

	// Provider-aware reasoning fields (Iteration 13).
	// ProviderContextRecords holds all provider-context memory records, loaded once per cycle.
	ProviderContextRecords []actionmemory.ProviderContextMemoryRecord `json:"provider_context_records,omitempty"`
	// ProviderName is the current provider (if known from job context).
	ProviderName string `json:"provider_name,omitempty"`
	// ModelRole is the current model role (if known from job context).
	ModelRole string `json:"model_role,omitempty"`
}

// PlannedActionCandidate represents one possible action for a goal,
// with its computed score, confidence, and reasoning trail.
type PlannedActionCandidate struct {
	ActionType   string         `json:"action_type"`
	GoalType     string         `json:"goal_type"`
	Score        float64        `json:"score"`
	Confidence   float64        `json:"confidence"`
	Reasoning    []string       `json:"reasoning"`
	Rejected     bool           `json:"rejected"`
	RejectReason string         `json:"reject_reason,omitempty"`
	Params       map[string]any `json:"params,omitempty"`
}

// PlanningDecision captures the full deliberation for a single goal:
// which candidates were considered, which was selected, and why.
type PlanningDecision struct {
	GoalID             string                   `json:"goal_id"`
	GoalType           string                   `json:"goal_type"`
	Candidates         []PlannedActionCandidate `json:"candidates"`
	SelectedActionType string                   `json:"selected_action_type"`
	Explanation        string                   `json:"explanation"`
	PlannedAt          time.Time                `json:"planned_at"`
}

// goalCandidateMap defines the explicit set of action types each goal type
// may produce. Every goal MUST have noop as a fallback.
var goalCandidateMap = map[string][]string{
	"reduce_retry_rate": {
		"retry_job",
		"log_recommendation",
		"noop",
	},
	"investigate_failed_jobs": {
		"retry_job",
		"log_recommendation",
		"noop",
	},
	"resolve_queue_backlog": {
		"trigger_resync",
		"log_recommendation",
		"noop",
	},
	"increase_reliability": {
		"log_recommendation",
		"noop",
	},
	"increase_model_quality": {
		"log_recommendation",
		"noop",
	},
	"reduce_latency": {
		"log_recommendation",
		"noop",
	},
}

// CandidatesForGoal returns the explicit candidate action types for a goal.
// Returns ["noop"] if the goal type is unknown.
func CandidatesForGoal(goalType string) []string {
	if candidates, ok := goalCandidateMap[goalType]; ok {
		return candidates
	}
	return []string{"noop"}
}
