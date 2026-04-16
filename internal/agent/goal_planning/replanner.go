package goal_planning

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Replanner handles adaptive replanning based on execution feedback.
type Replanner struct {
	subgoalStore SubgoalStoreInterface
	planStore    PlanStoreInterface
	auditor      audit.AuditRecorder
	logger       *zap.Logger
	reflection   ReflectionProvider
	execFeedback ExecutionFeedbackProvider
	objective    ObjectiveProvider
}

// NewReplanner creates a new replanner.
func NewReplanner(
	subgoalStore SubgoalStoreInterface,
	planStore PlanStoreInterface,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Replanner {
	return &Replanner{
		subgoalStore: subgoalStore,
		planStore:    planStore,
		auditor:      auditor,
		logger:       logger,
	}
}

// WithReflection sets the reflection provider.
func (r *Replanner) WithReflection(p ReflectionProvider) *Replanner {
	r.reflection = p
	return r
}

// WithExecutionFeedback sets the execution feedback provider.
func (r *Replanner) WithExecutionFeedback(p ExecutionFeedbackProvider) *Replanner {
	r.execFeedback = p
	return r
}

// WithObjective sets the objective provider.
func (r *Replanner) WithObjective(p ObjectiveProvider) *Replanner {
	r.objective = p
	return r
}

// RunReplanCycle gathers feedback and replans subgoals that need attention.
// Returns the number of subgoals replanned.
func (r *Replanner) RunReplanCycle(ctx context.Context) (int, error) {
	all, err := r.subgoalStore.ListAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("list subgoals: %w", err)
	}

	// Gather objective delta.
	objectiveDelta := 0.0
	if r.objective != nil {
		netUtility := r.objective.GetNetUtility()
		objectiveDelta = netUtility - 0.50 // delta from neutral
	}

	// Gather execution feedback per goal.
	feedbackByGoal := make(map[string][3]int) // goalID → [successes, failures, consecutive]
	if r.execFeedback != nil {
		goalIDs := make(map[string]bool)
		for _, sg := range all {
			goalIDs[sg.GoalID] = true
		}
		for gid := range goalIDs {
			s, f, c := r.execFeedback.GetFeedbackForGoal(ctx, gid)
			feedbackByGoal[gid] = [3]int{s, f, c}
		}
	}

	// Gather reflection signals.
	var reflectionSignals []ReflectionSignalInput
	if r.reflection != nil {
		reflectionSignals = r.reflection.GetReflectionSignals(ctx)
	}

	replanned := 0
	now := time.Now().UTC()

	for _, sg := range all {
		if sg.Status != SubgoalActive && sg.Status != SubgoalBlocked {
			continue
		}

		// Update failure/success counts from execution feedback.
		if fb, ok := feedbackByGoal[sg.GoalID]; ok {
			sg.FailureCount = fb[1]
			sg.SuccessCount = fb[0]
		}

		// Apply reflection signal adjustments.
		for _, sig := range reflectionSignals {
			if sig.GoalID == sg.GoalID {
				r.applyReflectionSignal(&sg, sig)
			}
		}

		// Check if replanning is needed.
		needsReplan, trigger := ShouldReplan(sg, objectiveDelta)
		if !needsReplan {
			continue
		}

		// Apply replan action.
		if err := r.replanSubgoal(ctx, &sg, trigger, objectiveDelta, now); err != nil {
			if r.logger != nil {
				r.logger.Warn("replan failed",
					zap.String("subgoal_id", sg.ID),
					zap.Error(err),
				)
			}
			continue
		}
		replanned++
	}

	r.auditEvent(ctx, "goal_planning.replan_completed", map[string]any{
		"replanned":       replanned,
		"total_evaluated": len(all),
	})

	return replanned, nil
}

func (r *Replanner) replanSubgoal(ctx context.Context, sg *Subgoal, trigger ReplanTrigger, objectiveDelta float64, now time.Time) error {
	newStrategy := SelectStrategy(*sg, objectiveDelta)
	oldStrategy := sg.Strategy

	switch trigger {
	case TriggerExecFailure:
		// Single failure: update strategy, keep active.
		sg.Strategy = newStrategy
		if err := r.subgoalStore.UpdateStrategy(ctx, sg.ID, newStrategy, sg.FailureCount, sg.SuccessCount); err != nil {
			return err
		}

	case TriggerRepeatedFailure:
		// Repeated failure: block if still failing, switch strategy.
		sg.Strategy = newStrategy
		if sg.Status == SubgoalActive {
			if err := r.subgoalStore.UpdateStatus(ctx, sg.ID, SubgoalBlocked, "repeated failures"); err != nil {
				return err
			}
			sg.Status = SubgoalBlocked
		}
		if err := r.subgoalStore.UpdateStrategy(ctx, sg.ID, newStrategy, sg.FailureCount, sg.SuccessCount); err != nil {
			return err
		}

	case TriggerReinforcement:
		// Success reinforcement: boost priority.
		newPriority := clamp01(sg.Priority + SuccessReinforcementBoost)
		sg.Priority = newPriority
		sg.Strategy = StrategyExploitSuccess
		if err := r.subgoalStore.UpdateStrategy(ctx, sg.ID, StrategyExploitSuccess, sg.FailureCount, sg.SuccessCount); err != nil {
			return err
		}

	case TriggerObjectivePenalty:
		// Objective penalty: defer high-risk, reduce scope.
		sg.Strategy = StrategyDeferHighRisk
		if err := r.subgoalStore.UpdateStrategy(ctx, sg.ID, StrategyDeferHighRisk, sg.FailureCount, sg.SuccessCount); err != nil {
			return err
		}
	}

	// Update the plan's replan count if there's a plan.
	if sg.PlanID != "" && r.planStore != nil {
		_ = r.planStore.IncrementReplanCount(ctx, sg.PlanID)
	}

	r.auditEvent(ctx, "goal_planning.subgoal_replanned", map[string]any{
		"subgoal_id":   sg.ID,
		"goal_id":      sg.GoalID,
		"trigger":      string(trigger),
		"old_strategy": string(oldStrategy),
		"new_strategy": string(newStrategy),
	})

	return nil
}

func (r *Replanner) applyReflectionSignal(sg *Subgoal, sig ReflectionSignalInput) {
	switch sig.SignalType {
	case "execution_failure", "repeated_failure":
		sg.FailureCount++
	case "positive_reinforcement":
		sg.SuccessCount++
	}
}

func (r *Replanner) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if r.auditor == nil {
		return
	}
	if err := r.auditor.RecordEvent(ctx, "goal_planning", uuid.Nil, eventType, "system", "goal_replanner", payload); err != nil && r.logger != nil {
		r.logger.Warn("audit event failed",
			zap.String("event", eventType),
			zap.Error(err),
		)
	}
}
