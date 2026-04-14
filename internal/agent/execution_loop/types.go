package executionloop

import (
	"encoding/json"
	"fmt"
	"time"
)

// --- Task status state machine ---

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusAborted   TaskStatus = "aborted"
)

var validTaskTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusPending:   {TaskStatusRunning, TaskStatusAborted},
	TaskStatusRunning:   {TaskStatusCompleted, TaskStatusFailed, TaskStatusAborted},
	TaskStatusCompleted: {},
	TaskStatusFailed:    {},
	TaskStatusAborted:   {},
}

// ValidateTaskTransition checks whether a task status transition is allowed.
func ValidateTaskTransition(from, to TaskStatus) bool {
	allowed, ok := validTaskTransitions[from]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}

// --- Step status state machine ---

type StepStatus string

const (
	StepStatusPending       StepStatus = "pending"
	StepStatusExecuted      StepStatus = "executed"
	StepStatusFailed        StepStatus = "failed"
	StepStatusSkipped       StepStatus = "skipped"
	StepStatusPendingReview StepStatus = "pending_review"
	StepStatusBlocked       StepStatus = "blocked"
)

// --- Hard limits ---

const (
	MaxIterations       = 5
	MaxStepsPerPlan     = 5
	MaxRetriesPerStep   = 2
	MaxExecutionTimeSec = 60
	MaxConsecFailures   = 3
)

// --- Entities ---

// ExecutionTask represents a bounded autonomous execution task.
type ExecutionTask struct {
	ID             string          `json:"id"`
	OpportunityID  string          `json:"opportunity_id"`
	Goal           string          `json:"goal"`
	Status         TaskStatus      `json:"status"`
	Plan           []ExecutionStep `json:"plan"`
	CurrentStep    int             `json:"current_step"`
	IterationCount int             `json:"iteration_count"`
	MaxIterations  int             `json:"max_iterations"`
	AbortReason    string          `json:"abort_reason,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ExecutionStep represents a single step in an execution plan.
type ExecutionStep struct {
	ID           string          `json:"id"`
	Description  string          `json:"description"`
	Tool         string          `json:"tool"`
	Payload      json.RawMessage `json:"payload"`
	Status       StepStatus      `json:"status"`
	ResultRef    string          `json:"result_ref,omitempty"`
	AttemptCount int             `json:"attempt_count"`
}

// ExecutionObservation records the result of executing a step.
type ExecutionObservation struct {
	StepID    string          `json:"step_id"`
	TaskID    string          `json:"task_id"`
	Success   bool            `json:"success"`
	Output    json.RawMessage `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// PlannerOutput is the strict JSON contract for planner responses.
type PlannerOutput struct {
	Steps []PlannerStep `json:"steps"`
}

// PlannerStep is a single step produced by the planner.
type PlannerStep struct {
	Description string          `json:"description"`
	Tool        string          `json:"tool"`
	Payload     json.RawMessage `json:"payload"`
}

// PlannerInput is the context provided to the planner.
type PlannerInput struct {
	Goal           string             `json:"goal"`
	Context        PlannerContext     `json:"context"`
	AvailableTools []string           `json:"available_tools"`
	Constraints    PlannerConstraints `json:"constraints"`
	PreviousErrors []string           `json:"previous_errors,omitempty"`
}

// PlannerContext provides situational context to the planner.
type PlannerContext struct {
	OpportunityID  string `json:"opportunity_id"`
	IterationCount int    `json:"iteration_count"`
}

// PlannerConstraints defines hard limits for the planner.
type PlannerConstraints struct {
	MaxSteps int `json:"max_steps"`
}

// ExecutorResult is the structured result from executing a step.
type ExecutorResult struct {
	Success        bool            `json:"success"`
	Output         json.RawMessage `json:"output,omitempty"`
	Error          string          `json:"error,omitempty"`
	RequiresReview bool            `json:"requires_review"`
	DryRun         bool            `json:"dry_run"`
	ActionID       string          `json:"action_id,omitempty"`
}

// --- Provider interfaces (local to avoid import cycles) ---

// GovernanceProvider checks governance state.
type GovernanceProvider interface {
	GetMode(ctx interface{}) string
}

// ObjectiveProvider checks objective signals.
type ObjectiveProvider interface {
	GetSignalType(ctx interface{}) string
	GetSignalStrength(ctx interface{}) float64
}

// ExternalActionsProvider executes actions through the external actions engine.
type ExternalActionsProvider interface {
	CreateAndExecute(ctx interface{}, actionType string, payload json.RawMessage, opportunityID string) (ExecutorResult, error)
}

// --- Errors ---

var (
	ErrTaskNotFound        = fmt.Errorf("execution task not found")
	ErrInvalidTransition   = fmt.Errorf("invalid task status transition")
	ErrMaxIterations       = fmt.Errorf("maximum iterations reached")
	ErrGovernanceFrozen    = fmt.Errorf("execution blocked: governance is frozen")
	ErrObjectivePenalty    = fmt.Errorf("execution aborted: objective penalty signal")
	ErrConsecutiveFailures = fmt.Errorf("execution aborted: consecutive failures exceeded")
	ErrEmptyPlan           = fmt.Errorf("planner generated empty plan")
	ErrPlanTooLong         = fmt.Errorf("plan exceeds maximum steps")
	ErrStepBlocked         = fmt.Errorf("step is blocked after repeated identical failures")
)
