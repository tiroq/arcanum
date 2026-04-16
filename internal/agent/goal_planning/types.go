package goal_planning

import "time"

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

// --- Horizon types ---

// Horizon represents the planning time horizon for a goal.
type Horizon string

const (
	HorizonDaily      Horizon = "daily"
	HorizonWeekly     Horizon = "weekly"
	HorizonMonthly    Horizon = "monthly"
	HorizonContinuous Horizon = "continuous"
)

// HorizonDays maps horizon types to approximate day counts for planning.
var HorizonDays = map[Horizon]int{
	HorizonDaily:      1,
	HorizonWeekly:     7,
	HorizonMonthly:    30,
	HorizonContinuous: 90,
}

// --- Constants ---

const (
	MaxSubgoalsPerGoal          = 8
	MaxActiveSubgoals           = 12
	TaskSourceGoalPlanning      = "goal_planning"
	ProgressStaleHours          = 24
	MinProgressToComplete       = 0.90
	BlockedProgressThreshold    = 0.10
	TaskEmissionCooldownMinutes = 30
	WeightUrgency               = 0.35
	WeightGoalPriority          = 0.35
	WeightProgressGap           = 0.30
)

// --- Entities ---

// Subgoal is a decomposed element of a strategic goal, with measurable criteria.
type Subgoal struct {
	ID              string        `json:"id"`
	GoalID          string        `json:"goal_id"`
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
