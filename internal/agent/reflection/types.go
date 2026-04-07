package reflection

import (
	"time"

	"github.com/google/uuid"
)

// Severity classifies how important a finding is.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
)

// Rule identifies which reflection rule produced a finding.
type Rule string

const (
	RuleRepeatedLowValue       Rule = "repeated_low_value_action"
	RulePlannerIgnoresFeedback Rule = "planner_ignores_feedback"
	RulePlannerStalling        Rule = "planner_stalling"
	RuleUnstableEffectiveness  Rule = "unstable_action_effectiveness"
	RuleEffectivePattern       Rule = "effective_action_pattern"
)

// Finding is a single observation produced by a reflection rule.
type Finding struct {
	ID         uuid.UUID      `json:"id"`
	CycleID    string         `json:"cycle_id"`
	Rule       Rule           `json:"rule"`
	Severity   Severity       `json:"severity"`
	ActionType string         `json:"action_type"`
	Summary    string         `json:"summary"`
	Detail     map[string]any `json:"detail"`
	CreatedAt  time.Time      `json:"created_at"`
}

// Report is the complete output of a reflection cycle.
type Report struct {
	CycleID   string    `json:"cycle_id"`
	Findings  []Finding `json:"findings"`
	CreatedAt time.Time `json:"created_at"`
}
