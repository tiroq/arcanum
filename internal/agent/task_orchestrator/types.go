package taskorchestrator

import (
	"context"
	"fmt"
	"time"
)

// --- Task status state machine ---

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusPaused    TaskStatus = "paused"
)

var validTaskTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusPending:   {TaskStatusQueued, TaskStatusPaused},
	TaskStatusQueued:    {TaskStatusRunning, TaskStatusPaused},
	TaskStatusRunning:   {TaskStatusCompleted, TaskStatusFailed, TaskStatusPaused},
	TaskStatusCompleted: {},
	TaskStatusFailed:    {},
	TaskStatusPaused:    {TaskStatusQueued},
}

// ValidateTransition checks whether a task status transition is allowed.
func ValidateTransition(from, to TaskStatus) bool {
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

// IsTerminal returns true if the status is a terminal state.
func (s TaskStatus) IsTerminal() bool {
	return s == TaskStatusCompleted || s == TaskStatusFailed
}

// --- Task source ---

type TaskSource string

const (
	SourceActuation     TaskSource = "actuation"
	SourceExecutionLoop TaskSource = "execution_loop"
	SourceManual        TaskSource = "manual"
)

// --- Priority scoring weights ---

const (
	WeightObjective float64 = 0.30
	WeightValue     float64 = 0.25
	WeightUrgency   float64 = 0.20
	WeightRecency   float64 = 0.10
	WeightRisk      float64 = 0.15
)

// --- Hard limits ---

const (
	MaxTasksInQueue   = 50
	MaxTasksPerCycle  = 3
	MaxRunningTasks   = 2
	TaskTTLHours      = 24
	StarvationHours   = 6
	HighRiskThreshold = 0.70
	BlockedRisk       = 0.90
	HighRiskMaxPrio   = 0.50
	CooldownMinutes   = 5
	OverloadThreshold = 0.75
)

// --- Entities ---

// OrchestratedTask represents a task managed by the multi-task orchestrator.
type OrchestratedTask struct {
	ID                  string     `json:"id"`
	Source              string     `json:"source"`
	Goal                string     `json:"goal"`
	PriorityScore       float64    `json:"priority_score"`
	Status              TaskStatus `json:"status"`
	Urgency             float64    `json:"urgency"`
	ExpectedValue       float64    `json:"expected_value"`
	RiskLevel           float64    `json:"risk_level"`
	StrategyType        string     `json:"strategy_type"`
	ActuationDecisionID string     `json:"actuation_decision_id,omitempty"`
	ExecutionTaskID     string     `json:"execution_task_id,omitempty"`
	OutcomeType         string     `json:"outcome_type,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	AttemptCount        int        `json:"attempt_count"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// TaskQueueEntry represents a scored entry in the priority queue.
type TaskQueueEntry struct {
	TaskID        string    `json:"task_id"`
	PriorityScore float64   `json:"priority_score"`
	InsertedAt    time.Time `json:"inserted_at"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

// DispatchResult captures the outcome of a dispatch cycle.
type DispatchResult struct {
	Dispatched []string `json:"dispatched"`
	Skipped    []string `json:"skipped"`
	Blocked    []string `json:"blocked"`
}

// ScoringInput holds the external signals used for priority scoring.
type ScoringInput struct {
	ObjectiveSignalType     string
	ObjectiveSignalStrength float64
	CapacityLoad            float64
}

// --- Provider interfaces (local, to avoid import cycles) ---

// ObjectiveProvider retrieves objective function signals.
type ObjectiveProvider interface {
	GetSignalType(ctx context.Context) string
	GetSignalStrength(ctx context.Context) float64
}

// GovernanceProvider checks governance state.
type GovernanceProvider interface {
	GetMode(ctx context.Context) string
}

// CapacityProvider returns current capacity load.
type CapacityProvider interface {
	GetLoad(ctx context.Context) float64
}

// PortfolioProvider returns strategy boost for a given strategy type.
type PortfolioProvider interface {
	GetStrategyBoost(ctx context.Context, strategyType string) float64
}

// ExecutionLoopProvider dispatches a task to the execution loop.
type ExecutionLoopProvider interface {
	CreateAndRun(ctx context.Context, goal string) (string, error)
}

// --- Errors ---

var (
	ErrTaskNotFound      = fmt.Errorf("orchestrated task not found")
	ErrInvalidTransition = fmt.Errorf("invalid task status transition")
	ErrQueueFull         = fmt.Errorf("task queue is full")
	ErrTaskExpired       = fmt.Errorf("task has exceeded TTL")
	ErrRiskBlocked       = fmt.Errorf("task blocked: risk too high")
	ErrGovernanceFrozen  = fmt.Errorf("dispatch blocked: governance is frozen")
	ErrMaxRunning        = fmt.Errorf("max concurrent running tasks reached")
	ErrCooldownActive    = fmt.Errorf("task is in cooldown period")
)

// nowUTC is a function variable for deterministic testing.
var nowUTC = func() time.Time { return time.Now().UTC() }
