package executionloop

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Planner generates and adapts execution plans.
// It is deterministic: given the same input, it produces the same output.
// LLM integration is intentionally deferred — the planner uses rule-based
// step generation for safety and determinism.
type Planner struct {
	allowedTools []string
}

// NewPlanner creates a Planner with the given allowed tools.
func NewPlanner(allowedTools []string) *Planner {
	if len(allowedTools) == 0 {
		allowedTools = []string{"external_action"}
	}
	return &Planner{allowedTools: allowedTools}
}

// GeneratePlan produces a plan from the given input.
// Validates constraints: max steps, allowed tools only.
func (p *Planner) GeneratePlan(input PlannerInput) ([]ExecutionStep, error) {
	if input.Goal == "" {
		return nil, fmt.Errorf("planner: goal is required")
	}
	maxSteps := input.Constraints.MaxSteps
	if maxSteps <= 0 || maxSteps > MaxStepsPerPlan {
		maxSteps = MaxStepsPerPlan
	}

	// Default deterministic plan: single step executing the goal via external_action.
	tool := "external_action"
	if len(p.allowedTools) > 0 {
		tool = p.allowedTools[0]
	}

	payload, _ := json.Marshal(map[string]string{
		"goal":           input.Goal,
		"opportunity_id": input.Context.OpportunityID,
	})

	steps := []ExecutionStep{
		{
			ID:          uuid.New().String(),
			Description: fmt.Sprintf("Execute goal: %s", input.Goal),
			Tool:        tool,
			Payload:     payload,
			Status:      StepStatusPending,
		},
	}

	return steps, nil
}

// GeneratePlanFromOutput validates and converts a PlannerOutput to ExecutionSteps.
// Used when an LLM or external system provides the plan.
func (p *Planner) GeneratePlanFromOutput(output PlannerOutput) ([]ExecutionStep, error) {
	if len(output.Steps) == 0 {
		return nil, ErrEmptyPlan
	}
	if len(output.Steps) > MaxStepsPerPlan {
		return nil, ErrPlanTooLong
	}

	var steps []ExecutionStep
	for _, s := range output.Steps {
		if !p.isToolAllowed(s.Tool) {
			return nil, fmt.Errorf("planner: tool %q is not allowed", s.Tool)
		}
		steps = append(steps, ExecutionStep{
			ID:          uuid.New().String(),
			Description: s.Description,
			Tool:        s.Tool,
			Payload:     s.Payload,
			Status:      StepStatusPending,
		})
	}

	return steps, nil
}

// AdaptPlan adjusts the plan after a step failure.
// Returns updated steps. If the failed step has been retried too many times, it is blocked.
// If no more actionable steps remain, returns the plan unchanged (engine will detect completion).
func (p *Planner) AdaptPlan(steps []ExecutionStep, failedStepID string, errMsg string) []ExecutionStep {
	for i := range steps {
		if steps[i].ID == failedStepID {
			steps[i].AttemptCount++
			if steps[i].AttemptCount >= MaxRetriesPerStep {
				steps[i].Status = StepStatusBlocked
			} else {
				// Reset to pending for retry.
				steps[i].Status = StepStatusPending
			}
		}
	}
	return steps
}

// isToolAllowed checks if a tool is in the allowed list.
func (p *Planner) isToolAllowed(tool string) bool {
	for _, t := range p.allowedTools {
		if t == tool {
			return true
		}
	}
	return false
}
