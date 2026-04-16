package goal_planning

import (
	"crypto/sha1"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/tiroq/arcanum/internal/agent/goals"
)

// --- Deterministic decomposition rules ---

// DefaultDecompositionRules returns the built-in rules for each goal type.
// Each goal type maps to a set of subgoal templates. This is a pure function.
func DefaultDecompositionRules() map[string][]SubgoalTemplate {
	return map[string][]SubgoalTemplate{
		"safety": {
			{
				TitlePattern:    "Protect family time blocks",
				TargetMetric:    "blocked_hours_today",
				TargetValue:     4.0,
				PreferredAction: "schedule_work",
				PriorityOffset:  0.0,
			},
			{
				TitlePattern:    "Maintain governance constraints",
				TargetMetric:    "governance_violations",
				TargetValue:     0,
				PreferredAction: "log_recommendation",
				PriorityOffset:  -0.05,
			},
		},
		"income": {
			{
				TitlePattern:    "Pursue high-value opportunities",
				TargetMetric:    "verified_monthly_income",
				TargetValue:     13000,
				PreferredAction: "analyze_opportunity",
				PriorityOffset:  0.0,
			},
			{
				TitlePattern:    "Convert pipeline to revenue",
				TargetMetric:    "pipeline_conversion_rate",
				TargetValue:     0.30,
				PreferredAction: "propose_income_action",
				PriorityOffset:  -0.05,
			},
			{
				TitlePattern:    "Schedule revenue-generating work",
				TargetMetric:    "scheduled_revenue_hours",
				TargetValue:     20,
				PreferredAction: "schedule_work",
				PriorityOffset:  -0.10,
			},
		},
		"efficiency": {
			{
				TitlePattern:    "Reduce owner cognitive load",
				TargetMetric:    "owner_load_score",
				TargetValue:     0.50,
				PreferredAction: "prioritize_tasks",
				PriorityOffset:  0.0,
			},
			{
				TitlePattern:    "Automate repetitive tasks",
				TargetMetric:    "automation_candidate_count",
				TargetValue:     0,
				PreferredAction: "create_task",
				PriorityOffset:  -0.05,
			},
		},
		"operational": {
			{
				TitlePattern:    "Maintain system uptime",
				TargetMetric:    "failure_rate",
				TargetValue:     0.05,
				PreferredAction: "retry_job",
				PriorityOffset:  0.0,
			},
			{
				TitlePattern:    "Clear processing backlog",
				TargetMetric:    "queued_jobs",
				TargetValue:     10,
				PreferredAction: "trigger_resync",
				PriorityOffset:  -0.05,
			},
		},
		"learning": {
			{
				TitlePattern:    "Record and learn from outcomes",
				TargetMetric:    "outcomes_recorded",
				TargetValue:     10,
				PreferredAction: "record_outcome",
				PriorityOffset:  0.0,
			},
			{
				TitlePattern:    "Update strategy based on feedback",
				TargetMetric:    "strategy_adjustments",
				TargetValue:     3,
				PreferredAction: "adjust_strategy",
				PriorityOffset:  -0.10,
			},
		},
		"evolution": {
			{
				TitlePattern:    "Identify improvement candidates",
				TargetMetric:    "improvement_candidates",
				TargetValue:     3,
				PreferredAction: "log_recommendation",
				PriorityOffset:  0.0,
			},
		},
	}
}

// DecomposeGoal generates subgoals from a strategic goal using decomposition rules.
// This is a deterministic, pure function. IDs are generated from goal_id + template index
// using SHA1-based UUIDs for idempotency.
func DecomposeGoal(goal goals.SystemGoal, rules map[string][]SubgoalTemplate) []Subgoal {
	templates, ok := rules[goal.Type]
	if !ok {
		return nil
	}

	now := time.Now().UTC()
	horizon := Horizon(goal.Horizon)
	if _, valid := HorizonDays[horizon]; !valid {
		horizon = HorizonContinuous
	}

	var subgoals []Subgoal
	for i, tmpl := range templates {
		if len(subgoals) >= MaxSubgoalsPerGoal {
			break
		}

		// Deterministic ID: SHA1(goal_id + index).
		idSeed := fmt.Sprintf("%s:%d", goal.ID, i)
		sgID := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(idSeed)).String()

		// Override target from goal's success_metrics if available.
		targetValue := tmpl.TargetValue
		for _, m := range goal.SuccessMetrics {
			if m.Name == tmpl.TargetMetric {
				if parsed, err := strconv.ParseFloat(m.Target, 64); err == nil {
					targetValue = parsed
				}
				break
			}
		}

		priority := clamp01(goal.Priority + tmpl.PriorityOffset)

		subgoals = append(subgoals, Subgoal{
			ID:              sgID,
			GoalID:          goal.ID,
			Title:           tmpl.TitlePattern,
			Description:     fmt.Sprintf("Subgoal for %s: %s", goal.ID, tmpl.TitlePattern),
			Status:          SubgoalNotStarted,
			ProgressScore:   0,
			TargetMetric:    tmpl.TargetMetric,
			TargetValue:     targetValue,
			CurrentValue:    0,
			PreferredAction: tmpl.PreferredAction,
			Horizon:         horizon,
			Priority:        priority,
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	}

	return subgoals
}

// deterministic ID generation helper (used for tests).
func deterministicID(goalID string, index int) string {
	idSeed := fmt.Sprintf("%s:%d", goalID, index)
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(idSeed)).String()
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// silence the sha1 import for deterministic UUID generation.
var _ = sha1.New
