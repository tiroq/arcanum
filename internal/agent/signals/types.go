package signals

import "time"

// --- Signal types ---

const (
	SignalFailedJobs       = "failed_jobs"
	SignalDeadLetterSpike  = "dead_letter_spike"
	SignalPendingTasks     = "pending_tasks"
	SignalOverdueTasks     = "overdue_tasks"
	SignalCostSpike        = "cost_spike"
	SignalIncomeGap        = "income_gap"
	SignalNewOpportunity   = "new_opportunity"
	SignalHighCognitiveLoad = "high_cognitive_load"
)

// ValidSignalTypes enumerates all recognised signal types.
var ValidSignalTypes = map[string]bool{
	SignalFailedJobs:        true,
	SignalDeadLetterSpike:   true,
	SignalPendingTasks:      true,
	SignalOverdueTasks:      true,
	SignalCostSpike:         true,
	SignalIncomeGap:         true,
	SignalNewOpportunity:    true,
	SignalHighCognitiveLoad: true,
}

// --- Severity levels ---

const (
	SeverityLow    = "low"
	SeverityMedium = "medium"
	SeverityHigh   = "high"
)

// ValidSeverities enumerates allowed severity values.
var ValidSeverities = map[string]bool{
	SeverityLow:    true,
	SeverityMedium: true,
	SeverityHigh:   true,
}

// --- Derived state keys ---

const (
	DerivedFailureRate       = "failure_rate"
	DerivedDeadLetterRate    = "dead_letter_rate"
	DerivedOwnerLoadScore    = "owner_load_score"
	DerivedIncomePressure    = "income_pressure"
	DerivedInfraCostPressure = "infra_cost_pressure"
)

// --- Entities ---

// RawEvent represents an ingested raw event before normalisation.
type RawEvent struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	EventType  string            `json:"event_type"`
	Payload    map[string]any    `json:"payload"`
	ObservedAt time.Time         `json:"observed_at"`
	CreatedAt  time.Time         `json:"created_at"`
}

// Signal represents a normalised signal derived from a raw event.
type Signal struct {
	ID          string    `json:"id"`
	SignalType  string    `json:"signal_type"`
	Severity    string    `json:"severity"`
	Confidence  float64   `json:"confidence"`
	Value       float64   `json:"value"`
	Source      string    `json:"source"`
	ContextTags []string  `json:"context_tags"`
	ObservedAt  time.Time `json:"observed_at"`
	RawEventID  string    `json:"raw_event_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// DerivedState represents a computed state metric.
type DerivedState struct {
	Key       string    `json:"key"`
	Value     float64   `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// --- Goal impact mapping ---

// GoalImpact maps a signal type to the system goals it affects.
var GoalImpact = map[string][]string{
	SignalFailedJobs:        {"system_reliability"},
	SignalDeadLetterSpike:   {"system_reliability"},
	SignalIncomeGap:         {"monthly_income_growth"},
	SignalNewOpportunity:    {"monthly_income_growth"},
	SignalHighCognitiveLoad: {"owner_load_reduction"},
	SignalPendingTasks:      {"system_reliability"},
	SignalOverdueTasks:      {"system_reliability"},
	SignalCostSpike:         {"monthly_income_growth"},
}

// --- Planner export ---

// ActiveSignals is the set of current signals exposed to the planner.
type ActiveSignals struct {
	Signals []Signal          `json:"signals"`
	Derived map[string]float64 `json:"derived"`
}

// --- Signal boost constants ---

const (
	// SignalBoostMax is the maximum additive score boost from active signals.
	SignalBoostMax = 0.10

	// SignalBoostPerMatch is the boost per matching signal–goal pair.
	SignalBoostPerMatch = 0.03
)
