package reflection

import (
	"time"
)

// --- Iteration 49: Reflection & Meta-Learning Layer ---

// ReflectionSignalType classifies meta-learning signals for the decision graph.
type ReflectionSignalType string

const (
	SignalLowEfficiency         ReflectionSignalType = "low_efficiency"
	SignalPricingMisalignment   ReflectionSignalType = "pricing_misalignment"
	SignalOverloadRisk          ReflectionSignalType = "overload_risk"
	SignalIncomeInstability     ReflectionSignalType = "income_instability"
	SignalAutomationOpportunity ReflectionSignalType = "automation_opportunity"
)

// ReflectionSignal carries a scored insight for the decision graph.
type ReflectionSignal struct {
	SignalType  ReflectionSignalType `json:"signal_type"`
	Strength    float64              `json:"strength"` // [0,1]
	ContextTags []string             `json:"context_tags"`
}

// MetaReflectionReport is the full output of a meta-learning reflection run.
type MetaReflectionReport struct {
	ID                 string             `json:"id"`
	PeriodStart        time.Time          `json:"period_start"`
	PeriodEnd          time.Time          `json:"period_end"`
	ActionsCount       int                `json:"actions_count"`
	OpportunitiesCount int                `json:"opportunities_count"`
	IncomeEstimated    float64            `json:"income_estimated"`
	IncomeVerified     float64            `json:"income_verified"`
	SuccessRate        float64            `json:"success_rate"`
	AvgAccuracy        float64            `json:"avg_accuracy"`
	AvgValuePerHour    float64            `json:"avg_value_per_hour"`
	FailureCount       int                `json:"failure_count"`
	SignalsSummary     map[string]float64 `json:"signals_summary"`
	Inefficiencies     []Inefficiency     `json:"inefficiencies"`
	Improvements       []Improvement      `json:"improvements"`
	RiskFlags          []RiskFlag         `json:"risk_flags"`
	CreatedAt          time.Time          `json:"created_at"`
}

// Inefficiency describes a detected system inefficiency.
type Inefficiency struct {
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Severity    float64 `json:"severity"` // [0,1]
}

// Improvement suggests a concrete improvement opportunity.
type Improvement struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	ActionType  string `json:"action_type,omitempty"`
}

// RiskFlag identifies a system-level risk.
type RiskFlag struct {
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Severity    float64 `json:"severity"` // [0,1]
}

// AggregatedData is the pre-computed data from all data sources for a time window.
type AggregatedData struct {
	PeriodStart        time.Time
	PeriodEnd          time.Time
	ActionsCount       int
	OpportunitiesCount int
	IncomeEstimated    float64
	IncomeVerified     float64
	SuccessRate        float64
	AvgAccuracy        float64
	TotalEffortHours   float64
	ValuePerHour       float64
	FailureCount       int
	OwnerLoadScore     float64
	SignalsSummary     map[string]float64
	ManualActionCounts map[string]int // action_type → count of manual repeats
}

// ReflectionInsights is the output of deterministic analysis on AggregatedData.
type ReflectionInsights struct {
	Inefficiencies []Inefficiency
	Improvements   []Improvement
	RiskFlags      []RiskFlag
	Signals        []ReflectionSignal
}

// TriggerConfig configures when meta-reflection runs are triggered.
type TriggerConfig struct {
	IntervalHours      float64 `json:"interval_hours"`       // time-based: run every N hours
	IncomeChangeThresh float64 `json:"income_change_thresh"` // event-based: income delta
	PressureThreshold  float64 `json:"pressure_threshold"`   // event-based: pressure level
	FailureSpikeCount  int     `json:"failure_spike_count"`  // event-based: failure count
	AccumActionCount   int     `json:"accum_action_count"`   // accumulative: action count
	AccumDeltaScore    float64 `json:"accum_delta_score"`    // accumulative: score delta
}

// DefaultTriggerConfig returns sensible defaults for trigger configuration.
func DefaultTriggerConfig() TriggerConfig {
	return TriggerConfig{
		IntervalHours:      24,
		IncomeChangeThresh: 500,
		PressureThreshold:  0.7,
		FailureSpikeCount:  5,
		AccumActionCount:   20,
		AccumDeltaScore:    0.3,
	}
}

// TriggerState tracks mutable state for trigger evaluation.
type TriggerState struct {
	LastRunAt          time.Time
	ActionsSinceRun    int
	FailuresSinceRun   int
	LastIncomeVerified float64
	LastPressure       float64
}
