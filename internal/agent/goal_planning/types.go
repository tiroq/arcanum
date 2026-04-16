package goal_planning

import (
	"context"
	"time"
)

// --- Subgoal status machine ---
// not_started → active → completed
// not_started → active → blocked → active
// active → failed

// SubgoalStatus represents the lifecycle state of a subgoal.
type SubgoalStatus string

const (
	SubgoalNotStarted SubgoalStatus = "not_started"
	SubgoalActive     SubgoalStatus = "active"
	SubgoalBlocked    SubgoalStatus = "blocked"
	SubgoalCompleted  SubgoalStatus = "completed"
	SubgoalFailed     SubgoalStatus = "failed"
)

// ValidSubgoalTransitions defines allowed state transitions.
var ValidSubgoalTransitions = map[SubgoalStatus][]SubgoalStatus{
	SubgoalNotStarted: {SubgoalActive},
	SubgoalActive:     {SubgoalCompleted, SubgoalFailed, SubgoalBlocked},
	SubgoalBlocked:    {SubgoalActive, SubgoalFailed},
}

// ValidateSubgoalTransition checks if a transition is allowed.
func ValidateSubgoalTransition(from, to SubgoalStatus) bool {
	allowed, ok := ValidSubgoalTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// --- Plan status machine ---
// draft → active → completed
// draft → active → replanning → active
// active → abandoned

// PlanStatus represents the lifecycle state of a goal plan.
type PlanStatus string

const (
	PlanDraft      PlanStatus = "draft"
	PlanActive     PlanStatus = "active"
	PlanReplanning PlanStatus = "replanning"
	PlanCompleted  PlanStatus = "completed"
	PlanAbandoned  PlanStatus = "abandoned"
)

// ValidPlanTransitions defines allowed plan state transitions.
var ValidPlanTransitions = map[PlanStatus][]PlanStatus{
	PlanDraft:      {PlanActive},
	PlanActive:     {PlanCompleted, PlanAbandoned, PlanReplanning},
	PlanReplanning: {PlanActive, PlanAbandoned},
}

// ValidatePlanTransition checks if a plan transition is allowed.
func ValidatePlanTransition(from, to PlanStatus) bool {
	allowed, ok := ValidPlanTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// --- Strategy types ---

// Strategy defines the approach for pursuing a subgoal.
type Strategy string

const (
	StrategyExploitSuccess Strategy = "exploit_success_path"
	StrategyReduceFailure  Strategy = "reduce_failure_path"
	StrategyDiversify      Strategy = "diversify_attempts"
	StrategyDeferHighRisk  Strategy = "defer_high_risk"
)

// --- Replan triggers ---

// ReplanTrigger represents a feedback event that can trigger replanning.
type ReplanTrigger string

const (
	TriggerExecFailure      ReplanTrigger = "execution_failure"
	TriggerRepeatedFailure  ReplanTrigger = "repeated_failure"
	TriggerReinforcement    ReplanTrigger = "positive_reinforcement"
	TriggerObjectivePenalty ReplanTrigger = "objective_penalty"
)

// --- Horizon types ---

// Horizon represents the planning time horizon for a goal.
type Horizon string

const (
	HorizonShort      Horizon = "short"      // <1 day
	HorizonMedium     Horizon = "medium"     // 1-7 days
	HorizonLong       Horizon = "long"       // >7 days
	HorizonDaily      Horizon = "daily"      // alias for short
	HorizonWeekly     Horizon = "weekly"     // alias for medium
	HorizonMonthly    Horizon = "monthly"    // alias for long
	HorizonContinuous Horizon = "continuous" // alias for long
)

// HorizonDays maps horizon types to approximate day counts for planning.
var HorizonDays = map[Horizon]int{
	HorizonShort:      1,
	HorizonMedium:     7,
	HorizonLong:       30,
	HorizonDaily:      1,
	HorizonWeekly:     7,
	HorizonMonthly:    30,
	HorizonContinuous: 90,
}

// --- Constants ---

const (
	MaxSubgoalsPerGoal          = 5
	MaxActiveSubgoals           = 12
	MaxTasksPerPlan             = 10
	MaxDepth                    = 3
	MaxReplanCount              = 5
	TaskSourceGoalPlanning      = "goal_planning"
	ProgressStaleHours          = 24
	MinProgressToComplete       = 0.90
	BlockedProgressThreshold    = 0.10
	TaskEmissionCooldownMinutes = 30
	WeightUrgency               = 0.35
	WeightGoalPriority          = 0.35
	WeightProgressGap           = 0.30
	RepeatedFailureThreshold    = 3
	SuccessReinforcementBoost   = 0.10
	ObjectivePenaltyThreshold   = 0.05
)

// --- Entities ---

// GoalPlan is a versioned plan for achieving a system goal.
type GoalPlan struct {
	ID              string     `json:"id"`
	GoalID          string     `json:"goal_id"`
	Version         int        `json:"version"`
	Horizon         Horizon    `json:"horizon"`
	Strategy        Strategy   `json:"strategy"`
	Status          PlanStatus `json:"status"`
	ExpectedUtility float64    `json:"expected_utility"`
	RiskEstimate    float64    `json:"risk_estimate"`
	ReplanCount     int        `json:"replan_count"`
	LastReplanAt    time.Time  `json:"last_replan_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// GoalDependency represents an ordering constraint between subgoals.
type GoalDependency struct {
	ID            string    `json:"id"`
	PlanID        string    `json:"plan_id"`
	FromSubgoalID string    `json:"from_subgoal_id"`
	ToSubgoalID   string    `json:"to_subgoal_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// Subgoal is a decomposed element of a strategic goal, with measurable criteria.
type Subgoal struct {
	ID              string        `json:"id"`
	GoalID          string        `json:"goal_id"`
	PlanID          string        `json:"plan_id"`
	Title           string        `json:"title"`
	Description     string        `json:"description"`
	Status          SubgoalStatus `json:"status"`
	ProgressScore   float64       `json:"progress_score"`
	TargetMetric    string        `json:"target_metric"`
	TargetValue     float64       `json:"target_value"`
	CurrentValue    float64       `json:"current_value"`
	PreferredAction string        `json:"preferred_action"`
	Horizon         Horizon       `json:"horizon"`
	Priority        float64       `json:"priority"`
	DependsOn       string        `json:"depends_on"`
	BlockReason     string        `json:"block_reason"`
	Strategy        Strategy      `json:"strategy"`
	FailureCount    int           `json:"failure_count"`
	SuccessCount    int           `json:"success_count"`
	LastTaskEmitted time.Time     `json:"last_task_emitted"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// GoalProgress is a point-in-time measurement for a subgoal.
type GoalProgress struct {
	ID          string    `json:"id"`
	SubgoalID   string    `json:"subgoal_id"`
	GoalID      string    `json:"goal_id"`
	MetricName  string    `json:"metric_name"`
	MetricValue float64   `json:"metric_value"`
	ProgressPct float64   `json:"progress_pct"`
	MeasuredAt  time.Time `json:"measured_at"`
}

// GoalPlanSummary provides a read-only view of the planning state.
type GoalPlanSummary struct {
	GoalID            string    `json:"goal_id"`
	GoalType          string    `json:"goal_type"`
	GoalPriority      float64   `json:"goal_priority"`
	Horizon           Horizon   `json:"horizon"`
	TotalSubgoals     int       `json:"total_subgoals"`
	ActiveSubgoals    int       `json:"active_subgoals"`
	CompletedSubgoals int       `json:"completed_subgoals"`
	BlockedSubgoals   int       `json:"blocked_subgoals"`
	OverallProgress   float64   `json:"overall_progress"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// TaskEmission represents a task to be emitted to the task orchestrator.
type TaskEmission struct {
	SubgoalID     string  `json:"subgoal_id"`
	GoalID        string  `json:"goal_id"`
	ActionType    string  `json:"action_type"`
	Urgency       float64 `json:"urgency"`
	ExpectedValue float64 `json:"expected_value"`
	RiskLevel     float64 `json:"risk_level"`
	StrategyType  string  `json:"strategy_type"`
	Priority      float64 `json:"priority"`
}

// ReplanInput represents feedback for replanning decisions.
type ReplanInput struct {
	Trigger        ReplanTrigger `json:"trigger"`
	GoalID         string        `json:"goal_id"`
	SubgoalID      string        `json:"subgoal_id"`
	FailureCount   int           `json:"failure_count"`
	SuccessCount   int           `json:"success_count"`
	ObjectiveDelta float64       `json:"objective_delta"`
}

// --- Decomposition templates ---

// DecompositionRule maps a goal type to its subgoal templates.
type DecompositionRule struct {
	GoalType string
	Subgoals []SubgoalTemplate
}

// SubgoalTemplate defines a template for generating a subgoal.
type SubgoalTemplate struct {
	TitlePattern    string
	TargetMetric    string
	TargetValue     float64
	PreferredAction string
	PriorityOffset  float64
}

// --- Provider interfaces ---

// ObjectiveProvider reads objective state for progress measurement.
type ObjectiveProvider interface {
	GetNetUtility() float64
	GetUtilityScore() float64
	GetRiskScore() float64
}

// CapacityProvider reads capacity for urgency calculation.
type CapacityProvider interface {
	GetOwnerLoadScore() float64
	GetAvailableHoursToday() float64
}

// TaskEmitter sends tasks to the task orchestrator.
type TaskEmitter interface {
	EmitTask(subgoalID, goalID, actionType string, urgency, expectedValue, riskLevel float64, strategyType string) error
}

// ReflectionProvider reads reflection signals for adaptive replanning.
type ReflectionProvider interface {
	GetReflectionSignals(ctx context.Context) []ReflectionSignalInput
}

// ReflectionSignalInput is a simplified reflection signal for the planner.
type ReflectionSignalInput struct {
	SignalType string  `json:"signal_type"`
	Strength   float64 `json:"strength"`
	GoalID     string  `json:"goal_id"`
}

// ExecutionFeedbackProvider provides task execution outcomes for replanning.
type ExecutionFeedbackProvider interface {
	GetFeedbackForGoal(ctx context.Context, goalID string) (successes, failures, consecutiveFailures int)
}
