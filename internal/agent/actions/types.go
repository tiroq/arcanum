package actions

import (
	"context"
	"time"

	"github.com/tiroq/arcanum/internal/agent/goals"
)

// ActionType enumerates the kinds of actions the engine can produce.
type ActionType string

const (
	ActionRetryJob          ActionType = "retry_job"
	ActionTriggerResync     ActionType = "trigger_resync"
	ActionLogRecommendation ActionType = "log_recommendation"
)

// ActionStatus tracks outcome of an action through the engine pipeline.
type ActionStatus string

const (
	StatusPlanned  ActionStatus = "planned"
	StatusRejected ActionStatus = "rejected"
	StatusExecuted ActionStatus = "executed"
	StatusFailed   ActionStatus = "failed"
)

// Action represents a single executable step derived from a goal.
type Action struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Priority    float64        `json:"priority"`
	Confidence  float64        `json:"confidence"`
	GoalID      string         `json:"goal_id"`
	Description string         `json:"description"`
	Params      map[string]any `json:"params"`
	Safe        bool           `json:"safe"`
	CreatedAt   time.Time      `json:"created_at"`
}

// ActionResult captures the outcome of executing (or rejecting) an action.
type ActionResult struct {
	ActionID string        `json:"action_id"`
	Status   ActionStatus  `json:"status"`
	Reason   string        `json:"reason,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration_ms,omitempty"`
}

// CycleReport is the output of a single RunCycle invocation.
type CycleReport struct {
	CycleID   string         `json:"cycle_id"`
	Planned   []Action       `json:"planned"`
	Rejected  []ActionResult `json:"rejected"`
	Executed  []ActionResult `json:"executed"`
	Failed    []ActionResult `json:"failed"`
	Timestamp time.Time      `json:"timestamp"`
}

// TargetResolver finds concrete targets for action types that need specific
// parameters (e.g., retry_job needs job IDs, trigger_resync needs task IDs).
// Implemented by the static Planner.
type TargetResolver interface {
	FindRetryTargets(ctx context.Context, g goals.Goal) ([]Action, error)
	FindResyncTargets(ctx context.Context, g goals.Goal) ([]Action, error)
}
