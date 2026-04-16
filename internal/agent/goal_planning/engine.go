package goal_planning

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/goals"
	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates goal decomposition, progress tracking, and task emission.
type Engine struct {
	subgoalStore  SubgoalStoreInterface
	progressStore ProgressStoreInterface
	planStore     PlanStoreInterface
	depStore      DependencyStoreInterface
	auditor       audit.AuditRecorder
	logger        *zap.Logger

	rules        map[string][]SubgoalTemplate
	objective    ObjectiveProvider
	capacity     CapacityProvider
	emitter      TaskEmitter
	reflection   ReflectionProvider
	execFeedback ExecutionFeedbackProvider
	replanner    *Replanner
	vector       VectorProvider
}

// NewEngine creates a new goal planning engine.
func NewEngine(
	subgoalStore SubgoalStoreInterface,
	progressStore ProgressStoreInterface,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		subgoalStore:  subgoalStore,
		progressStore: progressStore,
		auditor:       auditor,
		logger:        logger,
		rules:         DefaultDecompositionRules(),
	}
}

// WithObjective sets the objective provider.
func (e *Engine) WithObjective(p ObjectiveProvider) *Engine {
	e.objective = p
	return e
}

// WithCapacity sets the capacity provider.
func (e *Engine) WithCapacity(p CapacityProvider) *Engine {
	e.capacity = p
	return e
}

// WithEmitter sets the task emitter.
func (e *Engine) WithEmitter(p TaskEmitter) *Engine {
	e.emitter = p
	return e
}

// WithRules overrides the decomposition rules (primarily for testing).
func (e *Engine) WithRules(rules map[string][]SubgoalTemplate) *Engine {
	e.rules = rules
	return e
}

// WithPlanStore sets the plan store.
func (e *Engine) WithPlanStore(ps PlanStoreInterface) *Engine {
	e.planStore = ps
	return e
}

// WithDependencyStore sets the dependency store.
func (e *Engine) WithDependencyStore(ds DependencyStoreInterface) *Engine {
	e.depStore = ds
	return e
}

// WithReflection sets the reflection provider for replanning.
func (e *Engine) WithReflection(p ReflectionProvider) *Engine {
	e.reflection = p
	return e
}

// WithExecutionFeedback sets the execution feedback provider for replanning.
func (e *Engine) WithExecutionFeedback(p ExecutionFeedbackProvider) *Engine {
	e.execFeedback = p
	return e
}

// WithReplanner sets the replanner for adaptive replanning.
func (e *Engine) WithReplanner(r *Replanner) *Engine {
	e.replanner = r
	return e
}

// WithVector sets the system vector provider for strategy and priority adjustment.
func (e *Engine) WithVector(p VectorProvider) *Engine {
	e.vector = p
	return e
}

// DecomposeGoals takes strategic goals and creates subgoals for any goal
// that doesn't already have subgoals. Returns the count of newly created subgoals.
func (e *Engine) DecomposeGoals(ctx context.Context, sysGoals []goals.SystemGoal) (int, error) {
	created := 0
	for _, goal := range sysGoals {
		existing, err := e.subgoalStore.ListByGoal(ctx, goal.ID)
		if err != nil {
			return created, fmt.Errorf("list subgoals for %s: %w", goal.ID, err)
		}
		if len(existing) > 0 {
			continue // already decomposed
		}

		// Create a plan for this goal.
		var planID string
		if e.planStore != nil {
			planID = uuid.New().String()
			horizon := Horizon(goal.Horizon)
			if _, valid := HorizonDays[horizon]; !valid {
				horizon = HorizonContinuous
			}
			now := time.Now().UTC()
			plan := GoalPlan{
				ID:              planID,
				GoalID:          goal.ID,
				Version:         1,
				Horizon:         horizon,
				Strategy:        StrategyExploitSuccess,
				Status:          PlanActive,
				ExpectedUtility: clamp01(goal.Priority * 0.80),
				RiskEstimate:    clamp01((1.0 - goal.Priority) * 0.30),
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			if err := e.planStore.Insert(ctx, plan); err != nil {
				return created, fmt.Errorf("create plan for %s: %w", goal.ID, err)
			}
		}

		subgoals := DecomposeGoal(goal, e.rules)
		for i, sg := range subgoals {
			sg.PlanID = planID
			sg.Strategy = StrategyExploitSuccess
			if err := e.subgoalStore.Insert(ctx, sg); err != nil {
				return created, fmt.Errorf("insert subgoal %s: %w", sg.ID, err)
			}
			created++

			// Create dependency if template has ordering (i > 0 depends on i-1).
			if i > 0 && e.depStore != nil && planID != "" {
				dep := GoalDependency{
					ID:            uuid.New().String(),
					PlanID:        planID,
					FromSubgoalID: subgoals[i-1].ID,
					ToSubgoalID:   sg.ID,
					CreatedAt:     time.Now().UTC(),
				}
				_ = e.depStore.Insert(ctx, dep)
			}

			e.auditEvent(ctx, "goal_planning.subgoal_created", map[string]any{
				"subgoal_id": sg.ID,
				"goal_id":    sg.GoalID,
				"plan_id":    planID,
				"title":      sg.Title,
				"priority":   sg.Priority,
				"horizon":    string(sg.Horizon),
				"strategy":   string(sg.Strategy),
			})
		}
	}
	return created, nil
}

// ActivateSubgoals transitions not_started subgoals to active, respecting
// MaxActiveSubgoals and dependency ordering.
func (e *Engine) ActivateSubgoals(ctx context.Context) (int, error) {
	all, err := e.subgoalStore.ListAll(ctx)
	if err != nil {
		return 0, err
	}

	// Count current active subgoals.
	activeCount := 0
	for _, sg := range all {
		if sg.Status == SubgoalActive {
			activeCount++
		}
	}

	activated := 0
	for _, sg := range all {
		if sg.Status != SubgoalNotStarted {
			continue
		}
		if activeCount >= MaxActiveSubgoals {
			break
		}
		if !IsDependencyMet(sg, all) {
			continue
		}

		if err := e.subgoalStore.UpdateStatus(ctx, sg.ID, SubgoalActive, ""); err != nil {
			return activated, err
		}
		activeCount++
		activated++
		e.auditEvent(ctx, "goal_planning.subgoal_activated", map[string]any{
			"subgoal_id": sg.ID,
			"goal_id":    sg.GoalID,
		})
	}
	return activated, nil
}

// UpdateProgress measures current progress for all active subgoals,
// auto-completes those that meet criteria, and blocks stale ones.
func (e *Engine) UpdateProgress(ctx context.Context) error {
	active, err := e.subgoalStore.ListActive(ctx)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	all, _ := e.subgoalStore.ListAll(ctx) // for dependency checking

	for _, sg := range active {
		// Compute progress from current value.
		progress := MeasureProgress(sg)
		if err := e.subgoalStore.UpdateProgress(ctx, sg.ID, progress, sg.CurrentValue); err != nil {
			return err
		}

		// Record progress measurement.
		_ = e.progressStore.Insert(ctx, GoalProgress{
			ID:          uuid.New().String(),
			SubgoalID:   sg.ID,
			GoalID:      sg.GoalID,
			MetricName:  sg.TargetMetric,
			MetricValue: sg.CurrentValue,
			ProgressPct: progress,
			MeasuredAt:  now,
		})

		// Update the in-memory copy for further checks.
		sg.ProgressScore = progress

		// Auto-complete?
		if ShouldAutoComplete(sg) {
			if err := e.subgoalStore.UpdateStatus(ctx, sg.ID, SubgoalCompleted, ""); err != nil {
				return err
			}
			e.auditEvent(ctx, "goal_planning.subgoal_completed", map[string]any{
				"subgoal_id":     sg.ID,
				"goal_id":        sg.GoalID,
				"progress_score": progress,
			})
			continue
		}

		// Should block?
		if ShouldBlock(sg, now) {
			reason := fmt.Sprintf("stale progress (%.1f%%) after %dh", progress*100, ProgressStaleHours)
			if !IsDependencyMet(sg, all) {
				reason = "dependency not met: " + sg.DependsOn
			}
			if err := e.subgoalStore.UpdateStatus(ctx, sg.ID, SubgoalBlocked, reason); err != nil {
				return err
			}
			e.auditEvent(ctx, "goal_planning.subgoal_blocked", map[string]any{
				"subgoal_id": sg.ID,
				"goal_id":    sg.GoalID,
				"reason":     reason,
			})
		}
	}
	return nil
}

// PlanAndEmitTasks plans tasks for active subgoals and emits them.
// Returns the number of tasks emitted.
func (e *Engine) PlanAndEmitTasks(ctx context.Context) (int, error) {
	active, err := e.subgoalStore.ListActive(ctx)
	if err != nil {
		return 0, err
	}

	// Include all subgoals for dependency checking.
	all, _ := e.subgoalStore.ListAll(ctx)
	// Replace active status from the active list into all.
	activeMap := make(map[string]Subgoal, len(active))
	for _, sg := range active {
		activeMap[sg.ID] = sg
	}
	// Use the full list for dependency checking in PlanTasks.
	for i, sg := range all {
		if a, ok := activeMap[sg.ID]; ok {
			all[i] = a
		}
	}

	// Gather objective delta for strategy application.
	objectiveDelta := 0.0
	if e.objective != nil {
		objectiveDelta = e.objective.GetNetUtility() - 0.50
	}

	// Gather vector fields for strategy selection and priority adjustment.
	var explorationLevel, riskTolerance, incomePriority float64
	if e.vector != nil {
		explorationLevel = e.vector.GetExplorationLevel()
		riskTolerance = e.vector.GetRiskTolerance()
		incomePriority = e.vector.GetIncomePriority()
	}

	now := time.Now().UTC()
	emissions := PlanTasks(all, now)

	// Cap at MaxTasksPerPlan.
	if len(emissions) > MaxTasksPerPlan {
		emissions = emissions[:MaxTasksPerPlan]
	}

	// Apply strategy adjustments with vector influence.
	for i, em := range emissions {
		sg := activeMap[em.SubgoalID]
		strategy := SelectStrategyWithVector(sg, objectiveDelta, explorationLevel, riskTolerance)
		emissions[i] = ApplyStrategyToEmission(em, strategy)

		// Vector priority adjustment: income-aligned subgoals get a boost
		// proportional to how far income priority is above baseline (0.70).
		if e.vector != nil {
			incomeBoost := clamp01((incomePriority - 0.70) * 0.30) // max +0.09
			emissions[i].Priority = clamp01(emissions[i].Priority + incomeBoost)
		}
	}

	if e.emitter == nil {
		return len(emissions), nil // dry run
	}

	emitted := 0
	for _, em := range emissions {
		err := e.emitter.EmitTask(em.SubgoalID, em.GoalID, em.ActionType,
			em.Urgency, em.ExpectedValue, em.RiskLevel, em.StrategyType)
		if err != nil {
			if e.logger != nil {
				e.logger.Warn("task emission failed",
					zap.String("subgoal_id", em.SubgoalID),
					zap.Error(err),
				)
			}
			continue
		}

		// Record emission time.
		_ = e.subgoalStore.UpdateLastTaskEmitted(ctx, em.SubgoalID, now)
		emitted++

		e.auditEvent(ctx, "goal_planning.task_emitted", map[string]any{
			"subgoal_id":  em.SubgoalID,
			"goal_id":     em.GoalID,
			"action_type": em.ActionType,
			"urgency":     em.Urgency,
			"priority":    em.Priority,
			"strategy":    em.StrategyType,
		})
	}
	return emitted, nil
}

// RunCycle executes a full goal planning cycle:
// 1. Decompose any new goals
// 2. Activate pending subgoals
// 3. Update progress
// 4. Plan and emit tasks
func (e *Engine) RunCycle(ctx context.Context, sysGoals []goals.SystemGoal) error {
	decomposed, err := e.DecomposeGoals(ctx, sysGoals)
	if err != nil {
		return fmt.Errorf("decompose: %w", err)
	}

	activated, err := e.ActivateSubgoals(ctx)
	if err != nil {
		return fmt.Errorf("activate: %w", err)
	}

	if err := e.UpdateProgress(ctx); err != nil {
		return fmt.Errorf("progress: %w", err)
	}

	emitted, err := e.PlanAndEmitTasks(ctx)
	if err != nil {
		return fmt.Errorf("emit: %w", err)
	}

	e.auditEvent(ctx, "goal_planning.cycle_completed", map[string]any{
		"decomposed": decomposed,
		"activated":  activated,
		"emitted":    emitted,
	})

	return nil
}

// RunReplanCycle executes adaptive replanning based on execution feedback.
// Returns the number of subgoals replanned.
func (e *Engine) RunReplanCycle(ctx context.Context) (int, error) {
	if e.replanner == nil {
		return 0, nil
	}
	return e.replanner.RunReplanCycle(ctx)
}

// GetSubgoal returns a single subgoal by ID.
func (e *Engine) GetSubgoal(ctx context.Context, id string) (Subgoal, error) {
	return e.subgoalStore.Get(ctx, id)
}

// ListSubgoals returns all subgoals for a goal.
func (e *Engine) ListSubgoals(ctx context.Context, goalID string) ([]Subgoal, error) {
	return e.subgoalStore.ListByGoal(ctx, goalID)
}

// ListAllSubgoals returns all subgoals.
func (e *Engine) ListAllSubgoals(ctx context.Context) ([]Subgoal, error) {
	return e.subgoalStore.ListAll(ctx)
}

// GetPlanSummary returns a summary of the planning state for a goal.
func (e *Engine) GetPlanSummary(ctx context.Context, goalID string, goalType string, goalPriority float64, horizon string) (GoalPlanSummary, error) {
	subgoals, err := e.subgoalStore.ListByGoal(ctx, goalID)
	if err != nil {
		return GoalPlanSummary{}, err
	}

	summary := GoalPlanSummary{
		GoalID:       goalID,
		GoalType:     goalType,
		GoalPriority: goalPriority,
		Horizon:      Horizon(horizon),
		UpdatedAt:    time.Now().UTC(),
	}

	for _, sg := range subgoals {
		summary.TotalSubgoals++
		switch sg.Status {
		case SubgoalActive:
			summary.ActiveSubgoals++
		case SubgoalCompleted:
			summary.CompletedSubgoals++
		case SubgoalBlocked:
			summary.BlockedSubgoals++
		}
	}

	summary.OverallProgress = ComputeOverallProgress(subgoals)
	return summary, nil
}

// TransitionSubgoal manually transitions a subgoal status with state machine validation.
func (e *Engine) TransitionSubgoal(ctx context.Context, id string, to SubgoalStatus, reason string) error {
	sg, err := e.subgoalStore.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ValidateSubgoalTransition(sg.Status, to) {
		return fmt.Errorf("invalid transition: %s → %s", sg.Status, to)
	}
	return e.subgoalStore.UpdateStatus(ctx, id, to, reason)
}

// CreatePlan creates a new goal plan. Returns the plan ID.
func (e *Engine) CreatePlan(ctx context.Context, goalID string, horizon Horizon, strategy Strategy) (GoalPlan, error) {
	if e.planStore == nil {
		return GoalPlan{}, fmt.Errorf("plan store not configured")
	}

	now := time.Now().UTC()
	plan := GoalPlan{
		ID:              uuid.New().String(),
		GoalID:          goalID,
		Version:         1,
		Horizon:         horizon,
		Strategy:        strategy,
		Status:          PlanDraft,
		ExpectedUtility: 0.50,
		RiskEstimate:    0.20,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := e.planStore.Insert(ctx, plan); err != nil {
		return GoalPlan{}, err
	}

	e.auditEvent(ctx, "goal_planning.plan_created", map[string]any{
		"plan_id":  plan.ID,
		"goal_id":  goalID,
		"horizon":  string(horizon),
		"strategy": string(strategy),
	})

	return plan, nil
}

// GetPlan returns a plan by ID.
func (e *Engine) GetPlan(ctx context.Context, id string) (GoalPlan, error) {
	if e.planStore == nil {
		return GoalPlan{}, fmt.Errorf("plan store not configured")
	}
	return e.planStore.Get(ctx, id)
}

// ListPlans returns all plans.
func (e *Engine) ListPlans(ctx context.Context) ([]GoalPlan, error) {
	if e.planStore == nil {
		return nil, nil
	}
	return e.planStore.ListAll(ctx)
}

// Replan triggers adaptive replanning for a specific goal.
// Returns the number of subgoals replanned.
func (e *Engine) Replan(ctx context.Context, goalID string) (int, error) {
	if e.replanner == nil {
		return 0, nil
	}
	return e.replanner.RunReplanCycle(ctx)
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	if err := e.auditor.RecordEvent(ctx, "goal_planning", uuid.Nil, eventType, "system", "goal_planning_engine", payload); err != nil && e.logger != nil {
		e.logger.Warn("audit event failed",
			zap.String("event", eventType),
			zap.Error(err),
		)
	}
}
