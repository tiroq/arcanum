package goal_planning

import (
	"context"
	"testing"
	"time"

	"github.com/tiroq/arcanum/internal/agent/goals"
)

// --- Decomposer Tests ---

func TestDecomposeGoal_IncomeType(t *testing.T) {
	rules := DefaultDecompositionRules()
	goal := goals.SystemGoal{
		ID:       "monthly_income_growth",
		Type:     "income",
		Priority: 0.95,
		Horizon:  "monthly",
	}

	subgoals := DecomposeGoal(goal, rules)
	if len(subgoals) != 3 {
		t.Fatalf("expected 3 subgoals for income, got %d", len(subgoals))
	}

	for i, sg := range subgoals {
		expectedID := deterministicID("monthly_income_growth", i)
		if sg.ID != expectedID {
			t.Errorf("subgoal %d: expected ID %s, got %s", i, expectedID, sg.ID)
		}
		if sg.GoalID != "monthly_income_growth" {
			t.Errorf("subgoal %d: wrong goal_id: %s", i, sg.GoalID)
		}
		if sg.Status != SubgoalNotStarted {
			t.Errorf("subgoal %d: expected not_started, got %s", i, sg.Status)
		}
		if sg.Horizon != HorizonMonthly {
			t.Errorf("subgoal %d: expected monthly horizon, got %s", i, sg.Horizon)
		}
	}

	if subgoals[0].Priority < subgoals[1].Priority || subgoals[1].Priority < subgoals[2].Priority {
		t.Error("priorities should be decreasing with offsets")
	}
}

func TestDecomposeGoal_UnknownType(t *testing.T) {
	rules := DefaultDecompositionRules()
	goal := goals.SystemGoal{ID: "unknown", Type: "fantasy", Priority: 0.5}
	subgoals := DecomposeGoal(goal, rules)
	if len(subgoals) != 0 {
		t.Fatalf("expected 0 subgoals for unknown type, got %d", len(subgoals))
	}
}

func TestDecomposeGoal_InvalidHorizon(t *testing.T) {
	rules := DefaultDecompositionRules()
	goal := goals.SystemGoal{ID: "g1", Type: "safety", Priority: 1.0, Horizon: "invalid"}
	subgoals := DecomposeGoal(goal, rules)
	for _, sg := range subgoals {
		if sg.Horizon != HorizonContinuous {
			t.Errorf("invalid horizon should default to continuous, got %s", sg.Horizon)
		}
	}
}

func TestDecomposeGoal_OverridesTargetFromMetrics(t *testing.T) {
	rules := DefaultDecompositionRules()
	goal := goals.SystemGoal{
		ID:       "monthly_income_growth",
		Type:     "income",
		Priority: 0.90,
		Horizon:  "monthly",
		SuccessMetrics: []goals.Metric{
			{Name: "verified_monthly_income", Target: "20000"},
		},
	}
	subgoals := DecomposeGoal(goal, rules)
	if len(subgoals) < 1 {
		t.Fatal("expected at least 1 subgoal")
	}
	if subgoals[0].TargetValue != 20000 {
		t.Errorf("expected target value 20000, got %f", subgoals[0].TargetValue)
	}
}

func TestDecomposeGoal_InvalidMetricTarget(t *testing.T) {
	rules := DefaultDecompositionRules()
	goal := goals.SystemGoal{
		ID:       "monthly_income_growth",
		Type:     "income",
		Priority: 0.90,
		Horizon:  "monthly",
		SuccessMetrics: []goals.Metric{
			{Name: "verified_monthly_income", Target: "not_a_number"},
		},
	}
	subgoals := DecomposeGoal(goal, rules)
	if len(subgoals) < 1 {
		t.Fatal("expected at least 1 subgoal")
	}
	if subgoals[0].TargetValue != 13000 {
		t.Errorf("expected default target 13000, got %f", subgoals[0].TargetValue)
	}
}

func TestDecomposeGoal_DeterministicIDs(t *testing.T) {
	rules := DefaultDecompositionRules()
	goal := goals.SystemGoal{ID: "g1", Type: "safety", Priority: 1.0, Horizon: "weekly"}
	run1 := DecomposeGoal(goal, rules)
	run2 := DecomposeGoal(goal, rules)
	if len(run1) != len(run2) {
		t.Fatal("nondeterministic result count")
	}
	for i := range run1 {
		if run1[i].ID != run2[i].ID {
			t.Errorf("subgoal %d: IDs differ between runs: %s vs %s", i, run1[i].ID, run2[i].ID)
		}
	}
}

func TestDecomposeGoal_AllTypes(t *testing.T) {
	rules := DefaultDecompositionRules()
	types := []string{"safety", "income", "efficiency", "operational", "learning", "evolution"}
	for _, goalType := range types {
		goal := goals.SystemGoal{ID: "test_" + goalType, Type: goalType, Priority: 0.80, Horizon: "weekly"}
		subgoals := DecomposeGoal(goal, rules)
		if len(subgoals) == 0 {
			t.Errorf("type %s produced no subgoals", goalType)
		}
		if len(subgoals) > MaxSubgoalsPerGoal {
			t.Errorf("type %s: %d subgoals exceed max %d", goalType, len(subgoals), MaxSubgoalsPerGoal)
		}
	}
}

func TestDecomposeGoal_PriorityClamp(t *testing.T) {
	rules := DefaultDecompositionRules()
	goal := goals.SystemGoal{ID: "g1", Type: "income", Priority: 1.0, Horizon: "monthly"}
	subgoals := DecomposeGoal(goal, rules)
	for _, sg := range subgoals {
		if sg.Priority < 0 || sg.Priority > 1 {
			t.Errorf("priority out of bounds: %f", sg.Priority)
		}
	}
}

// --- Progress Tests ---

func TestMeasureProgress_Standard(t *testing.T) {
	sg := Subgoal{TargetValue: 100, CurrentValue: 50}
	p := MeasureProgress(sg)
	if p != 0.5 {
		t.Errorf("expected 0.5, got %f", p)
	}
}

func TestMeasureProgress_Completed(t *testing.T) {
	sg := Subgoal{TargetValue: 100, CurrentValue: 100}
	p := MeasureProgress(sg)
	if p != 1.0 {
		t.Errorf("expected 1.0, got %f", p)
	}
}

func TestMeasureProgress_Exceeds(t *testing.T) {
	sg := Subgoal{TargetValue: 100, CurrentValue: 200}
	p := MeasureProgress(sg)
	if p != 1.0 {
		t.Errorf("expected 1.0 (clamped), got %f", p)
	}
}

func TestMeasureProgress_ZeroTarget(t *testing.T) {
	sg := Subgoal{TargetValue: 0, CurrentValue: 0}
	p := MeasureProgress(sg)
	if p != 1.0 {
		t.Errorf("expected 1.0 for zero at zero target, got %f", p)
	}
}

func TestMeasureProgress_ZeroTargetWithViolations(t *testing.T) {
	sg := Subgoal{TargetValue: 0, CurrentValue: 3}
	p := MeasureProgress(sg)
	if p < 0 || p > 1 {
		t.Errorf("progress out of bounds: %f", p)
	}
	if p >= 1.0 {
		t.Error("violations should reduce progress below 1.0")
	}
}

func TestIsStale(t *testing.T) {
	now := time.Now().UTC()
	sg := Subgoal{UpdatedAt: now.Add(-25 * time.Hour)}
	if !IsStale(sg, now) {
		t.Error("25h old should be stale")
	}
	sgFresh := Subgoal{UpdatedAt: now.Add(-1 * time.Hour)}
	if IsStale(sgFresh, now) {
		t.Error("1h old should not be stale")
	}
}

func TestShouldAutoComplete(t *testing.T) {
	sg := Subgoal{Status: SubgoalActive, ProgressScore: 0.95}
	if !ShouldAutoComplete(sg) {
		t.Error("95% active subgoal should auto-complete")
	}
	sg2 := Subgoal{Status: SubgoalActive, ProgressScore: 0.80}
	if ShouldAutoComplete(sg2) {
		t.Error("80% should not auto-complete")
	}
	sg3 := Subgoal{Status: SubgoalNotStarted, ProgressScore: 1.0}
	if ShouldAutoComplete(sg3) {
		t.Error("not_started should not auto-complete")
	}
}

func TestShouldBlock(t *testing.T) {
	now := time.Now().UTC()
	sg := Subgoal{
		Status:        SubgoalActive,
		ProgressScore: 0.05,
		UpdatedAt:     now.Add(-25 * time.Hour),
	}
	if !ShouldBlock(sg, now) {
		t.Error("stale + low progress active should block")
	}
	sgOK := Subgoal{
		Status:        SubgoalActive,
		ProgressScore: 0.50,
		UpdatedAt:     now.Add(-25 * time.Hour),
	}
	if ShouldBlock(sgOK, now) {
		t.Error("stale but good progress should not block")
	}
}

func TestIsDependencyMet(t *testing.T) {
	all := []Subgoal{
		{ID: "a", Status: SubgoalCompleted},
		{ID: "b", DependsOn: "a", Status: SubgoalNotStarted},
		{ID: "c", DependsOn: "missing", Status: SubgoalNotStarted},
		{ID: "d", Status: SubgoalNotStarted},
	}
	if !IsDependencyMet(all[1], all) {
		t.Error("b's dependency on completed a should be met")
	}
	if IsDependencyMet(all[2], all) {
		t.Error("c's dependency on missing should not be met")
	}
	if !IsDependencyMet(all[3], all) {
		t.Error("d with no dependency should be met")
	}
}

func TestComputeOverallProgress(t *testing.T) {
	subgoals := []Subgoal{
		{Priority: 1.0, ProgressScore: 0.80},
		{Priority: 0.5, ProgressScore: 0.40},
	}
	p := ComputeOverallProgress(subgoals)
	if p < 0.66 || p > 0.67 {
		t.Errorf("expected ~0.667, got %f", p)
	}
}

func TestComputeOverallProgress_Empty(t *testing.T) {
	p := ComputeOverallProgress(nil)
	if p != 0 {
		t.Errorf("expected 0 for empty, got %f", p)
	}
}

func TestComputeTaskUrgency(t *testing.T) {
	now := time.Now().UTC()
	sg := Subgoal{
		Horizon:       HorizonWeekly,
		CreatedAt:     now.Add(-3 * 24 * time.Hour),
		ProgressScore: 0.20,
	}
	urgency := ComputeTaskUrgency(sg, now)
	if urgency < 0 || urgency > 1 {
		t.Errorf("urgency out of bounds: %f", urgency)
	}
	if urgency < 0.3 {
		t.Errorf("expected meaningful urgency for mid-horizon low-progress, got %f", urgency)
	}
}

func TestComputeTaskPriority(t *testing.T) {
	priority := ComputeTaskPriority(0.70, 0.95, 0.20)
	if priority < 0 || priority > 1 {
		t.Errorf("priority out of bounds: %f", priority)
	}
	if priority < 0.75 || priority > 0.85 {
		t.Errorf("expected ~0.82, got %f", priority)
	}
}

// --- Planner Tests ---

func TestPlanTasks_ActiveSubgoals(t *testing.T) {
	now := time.Now().UTC()
	subgoals := []Subgoal{
		{
			ID: "s1", GoalID: "g1", Status: SubgoalActive,
			ProgressScore: 0.30, Priority: 0.90,
			PreferredAction: "analyze_opportunity",
			Horizon:         HorizonWeekly, CreatedAt: now.Add(-2 * 24 * time.Hour),
		},
		{
			ID: "s2", GoalID: "g1", Status: SubgoalActive,
			ProgressScore: 0.10, Priority: 0.80,
			PreferredAction: "propose_income_action",
			Horizon:         HorizonMonthly, CreatedAt: now.Add(-5 * 24 * time.Hour),
		},
	}
	emissions := PlanTasks(subgoals, now)
	if len(emissions) != 2 {
		t.Fatalf("expected 2 emissions, got %d", len(emissions))
	}
	if emissions[0].Priority < emissions[1].Priority {
		t.Error("emissions should be sorted by priority descending")
	}
}

func TestPlanTasks_SkipsCompleted(t *testing.T) {
	now := time.Now().UTC()
	subgoals := []Subgoal{
		{ID: "s1", Status: SubgoalActive, ProgressScore: 0.95, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
	}
	emissions := PlanTasks(subgoals, now)
	if len(emissions) != 0 {
		t.Fatalf("expected 0 emissions for completed subgoal, got %d", len(emissions))
	}
}

func TestPlanTasks_SkipsNotActive(t *testing.T) {
	now := time.Now().UTC()
	subgoals := []Subgoal{
		{ID: "s1", Status: SubgoalNotStarted, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
		{ID: "s2", Status: SubgoalBlocked, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
		{ID: "s3", Status: SubgoalCompleted, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
		{ID: "s4", Status: SubgoalFailed, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
	}
	emissions := PlanTasks(subgoals, now)
	if len(emissions) != 0 {
		t.Fatalf("expected 0 emissions for non-active subgoals, got %d", len(emissions))
	}
}

func TestPlanTasks_RespectsCooldown(t *testing.T) {
	now := time.Now().UTC()
	subgoals := []Subgoal{
		{
			ID: "s1", Status: SubgoalActive, ProgressScore: 0.30, Priority: 0.90,
			Horizon: HorizonWeekly, CreatedAt: now.Add(-24 * time.Hour),
			LastTaskEmitted: now.Add(-10 * time.Minute),
		},
	}
	emissions := PlanTasks(subgoals, now)
	if len(emissions) != 0 {
		t.Fatalf("expected 0 emissions during cooldown, got %d", len(emissions))
	}
}

func TestPlanTasks_SkipsDependencyNotMet(t *testing.T) {
	now := time.Now().UTC()
	subgoals := []Subgoal{
		{ID: "dep", GoalID: "g1", Status: SubgoalActive, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
		{ID: "child", GoalID: "g1", DependsOn: "dep", Status: SubgoalActive, Priority: 0.80, Horizon: HorizonWeekly, CreatedAt: now},
	}
	emissions := PlanTasks(subgoals, now)
	for _, e := range emissions {
		if e.SubgoalID == "child" {
			t.Error("child with unmet dependency should not emit")
		}
	}
}

// --- State Machine Tests ---

func TestValidateSubgoalTransition(t *testing.T) {
	tests := []struct {
		from, to SubgoalStatus
		valid    bool
	}{
		{SubgoalNotStarted, SubgoalActive, true},
		{SubgoalActive, SubgoalCompleted, true},
		{SubgoalActive, SubgoalFailed, true},
		{SubgoalActive, SubgoalBlocked, true},
		{SubgoalBlocked, SubgoalActive, true},
		{SubgoalBlocked, SubgoalFailed, true},
		{SubgoalNotStarted, SubgoalCompleted, false},
		{SubgoalNotStarted, SubgoalFailed, false},
		{SubgoalCompleted, SubgoalActive, false},
		{SubgoalFailed, SubgoalActive, false},
	}
	for _, tt := range tests {
		got := ValidateSubgoalTransition(tt.from, tt.to)
		if got != tt.valid {
			t.Errorf("%s->%s: expected %v, got %v", tt.from, tt.to, tt.valid, got)
		}
	}
}

// --- Engine Tests ---

func TestEngine_DecomposeGoals(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	sysGoals := []goals.SystemGoal{
		{ID: "g1", Type: "income", Priority: 0.95, Horizon: "monthly"},
		{ID: "g2", Type: "safety", Priority: 1.0, Horizon: "daily"},
	}

	created, err := engine.DecomposeGoals(ctx, sysGoals)
	if err != nil {
		t.Fatal(err)
	}
	if created != 5 {
		t.Errorf("expected 5 created, got %d", created)
	}

	created2, err := engine.DecomposeGoals(ctx, sysGoals)
	if err != nil {
		t.Fatal(err)
	}
	if created2 != 0 {
		t.Errorf("expected 0 on second decompose, got %d", created2)
	}
}

func TestEngine_ActivateSubgoals(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	for i := 0; i < 3; i++ {
		_ = store.Insert(ctx, Subgoal{
			ID: deterministicID("g1", i), GoalID: "g1",
			Status: SubgoalNotStarted, Priority: 0.90 - float64(i)*0.05,
			Horizon: HorizonWeekly,
		})
	}

	activated, err := engine.ActivateSubgoals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if activated != 3 {
		t.Errorf("expected 3 activated, got %d", activated)
	}

	all, _ := store.ListAll(ctx)
	for _, sg := range all {
		if sg.Status != SubgoalActive {
			t.Errorf("subgoal %s should be active, got %s", sg.ID, sg.Status)
		}
	}
}

func TestEngine_ActivateSubgoals_RespectsMaxActive(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	for i := 0; i < MaxActiveSubgoals+5; i++ {
		_ = store.Insert(ctx, Subgoal{
			ID: deterministicID("g1", i), GoalID: "g1",
			Status: SubgoalNotStarted, Priority: 0.90, Horizon: HorizonWeekly,
		})
	}

	activated, err := engine.ActivateSubgoals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if activated > MaxActiveSubgoals {
		t.Errorf("activated %d exceeds max %d", activated, MaxActiveSubgoals)
	}
}

func TestEngine_ActivateSubgoals_DependencyBlocking(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	_ = store.Insert(ctx, Subgoal{ID: "parent", GoalID: "g1", Status: SubgoalNotStarted})
	_ = store.Insert(ctx, Subgoal{ID: "child", GoalID: "g1", DependsOn: "parent", Status: SubgoalNotStarted})

	activated, err := engine.ActivateSubgoals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = activated

	parentSG, _ := store.Get(ctx, "parent")
	if parentSG.Status != SubgoalActive {
		t.Error("parent should be active")
	}
	childSG, _ := store.Get(ctx, "child")
	if childSG.Status == SubgoalActive {
		t.Error("child should NOT be active (parent not completed)")
	}
}

func TestEngine_UpdateProgress_AutoComplete(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	_ = store.Insert(ctx, Subgoal{
		ID: "s1", GoalID: "g1", Status: SubgoalActive,
		TargetValue: 100, CurrentValue: 95, UpdatedAt: time.Now().UTC(),
	})

	err := engine.UpdateProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}

	sg, _ := store.Get(ctx, "s1")
	if sg.Status != SubgoalCompleted {
		t.Errorf("expected completed, got %s", sg.Status)
	}
}

func TestEngine_UpdateProgress_Block(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	_ = store.Insert(ctx, Subgoal{
		ID: "s1", GoalID: "g1", Status: SubgoalActive,
		TargetValue: 100, CurrentValue: 5,
		UpdatedAt: time.Now().UTC().Add(-30 * time.Hour),
	})

	err := engine.UpdateProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}

	sg, _ := store.Get(ctx, "s1")
	if sg.Status != SubgoalBlocked {
		t.Errorf("expected blocked, got %s", sg.Status)
	}
}

func TestEngine_PlanAndEmitTasks_DryRun(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	_ = store.Insert(ctx, Subgoal{
		ID: "s1", GoalID: "g1", Status: SubgoalActive,
		ProgressScore: 0.30, Priority: 0.90,
		PreferredAction: "analyze_opportunity",
		Horizon:         HorizonWeekly, CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
	})

	emitted, err := engine.PlanAndEmitTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if emitted != 1 {
		t.Errorf("expected 1 emission (dry run), got %d", emitted)
	}
}

type mockEmitter struct {
	emitted []TaskEmission
}

func (m *mockEmitter) EmitTask(subgoalID, goalID, actionType string, urgency, expectedValue, riskLevel float64, strategyType string) error {
	m.emitted = append(m.emitted, TaskEmission{
		SubgoalID: subgoalID, GoalID: goalID, ActionType: actionType,
		Urgency: urgency, ExpectedValue: expectedValue,
		RiskLevel: riskLevel, StrategyType: strategyType,
	})
	return nil
}

func TestEngine_PlanAndEmitTasks_WithEmitter(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	emitter := &mockEmitter{}
	engine := NewEngine(store, progress, nil, nil).WithEmitter(emitter)

	now := time.Now().UTC()
	_ = store.Insert(ctx, Subgoal{
		ID: "s1", GoalID: "g1", Status: SubgoalActive,
		ProgressScore: 0.30, Priority: 0.90,
		PreferredAction: "analyze_opportunity",
		Horizon:         HorizonWeekly, CreatedAt: now.Add(-48 * time.Hour),
	})

	emitted, err := engine.PlanAndEmitTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if emitted != 1 {
		t.Fatalf("expected 1 emitted, got %d", emitted)
	}
	if len(emitter.emitted) != 1 {
		t.Fatalf("expected 1 in mock emitter, got %d", len(emitter.emitted))
	}
	if emitter.emitted[0].SubgoalID != "s1" {
		t.Errorf("wrong subgoal emitted: %s", emitter.emitted[0].SubgoalID)
	}

	sg, _ := store.Get(ctx, "s1")
	if sg.LastTaskEmitted.IsZero() {
		t.Error("last_task_emitted should be set after emission")
	}
}

func TestEngine_RunCycle(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	emitter := &mockEmitter{}
	engine := NewEngine(store, progress, nil, nil).WithEmitter(emitter)

	sysGoals := []goals.SystemGoal{
		{ID: "g1", Type: "income", Priority: 0.95, Horizon: "monthly"},
	}

	err := engine.RunCycle(ctx, sysGoals)
	if err != nil {
		t.Fatal(err)
	}

	all, _ := store.ListAll(ctx)
	if len(all) != 3 {
		t.Fatalf("expected 3 subgoals, got %d", len(all))
	}

	activeCount := 0
	for _, sg := range all {
		if sg.Status == SubgoalActive {
			activeCount++
		}
	}
	if activeCount != 3 {
		t.Errorf("expected 3 active, got %d", activeCount)
	}

	if len(emitter.emitted) == 0 {
		t.Error("expected at least one task emission")
	}
}

func TestEngine_TransitionSubgoal(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	_ = store.Insert(ctx, Subgoal{ID: "s1", GoalID: "g1", Status: SubgoalActive})

	err := engine.TransitionSubgoal(ctx, "s1", SubgoalCompleted, "done")
	if err != nil {
		t.Fatal(err)
	}
	sg, _ := store.Get(ctx, "s1")
	if sg.Status != SubgoalCompleted {
		t.Errorf("expected completed, got %s", sg.Status)
	}
}

func TestEngine_TransitionSubgoal_Invalid(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	_ = store.Insert(ctx, Subgoal{ID: "s1", GoalID: "g1", Status: SubgoalCompleted})

	err := engine.TransitionSubgoal(ctx, "s1", SubgoalActive, "reopen")
	if err == nil {
		t.Error("expected error for invalid transition")
	}
}

func TestEngine_GetPlanSummary(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)

	_ = store.Insert(ctx, Subgoal{ID: "s1", GoalID: "g1", Status: SubgoalActive, Priority: 0.9, ProgressScore: 0.50})
	_ = store.Insert(ctx, Subgoal{ID: "s2", GoalID: "g1", Status: SubgoalCompleted, Priority: 0.8, ProgressScore: 1.0})
	_ = store.Insert(ctx, Subgoal{ID: "s3", GoalID: "g1", Status: SubgoalBlocked, Priority: 0.7, ProgressScore: 0.05})

	summary, err := engine.GetPlanSummary(ctx, "g1", "income", 0.95, "monthly")
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalSubgoals != 3 {
		t.Errorf("expected 3 total, got %d", summary.TotalSubgoals)
	}
	if summary.ActiveSubgoals != 1 {
		t.Errorf("expected 1 active, got %d", summary.ActiveSubgoals)
	}
	if summary.CompletedSubgoals != 1 {
		t.Errorf("expected 1 completed, got %d", summary.CompletedSubgoals)
	}
	if summary.BlockedSubgoals != 1 {
		t.Errorf("expected 1 blocked, got %d", summary.BlockedSubgoals)
	}
	if summary.OverallProgress <= 0 {
		t.Error("overall progress should be > 0")
	}
}

// --- Adapter Tests ---

func TestGraphAdapter_NilSafe(t *testing.T) {
	var adapter *GraphAdapter
	ctx := context.Background()

	sg := adapter.GetSubgoal(ctx, "any")
	if sg.ID != "" {
		t.Error("nil adapter should return zero subgoal")
	}
	sgs := adapter.ListSubgoals(ctx, "any")
	if sgs != nil {
		t.Error("nil adapter should return nil list")
	}
	all := adapter.ListAllSubgoals(ctx)
	if all != nil {
		t.Error("nil adapter should return nil all list")
	}
	p := adapter.GetOverallProgress(ctx, "any")
	if p != 0 {
		t.Error("nil adapter should return 0 progress")
	}
}

func TestGraphAdapter_WithEngine(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySubgoalStore()
	progress := NewInMemoryProgressStore()
	engine := NewEngine(store, progress, nil, nil)
	adapter := NewGraphAdapter(engine, nil)

	_ = store.Insert(ctx, Subgoal{
		ID: "s1", GoalID: "g1", Status: SubgoalActive,
		Priority: 0.90, ProgressScore: 0.50,
	})

	sg := adapter.GetSubgoal(ctx, "s1")
	if sg.ID != "s1" {
		t.Error("adapter should return subgoal")
	}
	sgs := adapter.ListSubgoals(ctx, "g1")
	if len(sgs) != 1 {
		t.Errorf("expected 1, got %d", len(sgs))
	}
	all := adapter.ListAllSubgoals(ctx)
	if len(all) != 1 {
		t.Errorf("expected 1, got %d", len(all))
	}
	p := adapter.GetOverallProgress(ctx, "g1")
	if p != 0.50 {
		t.Errorf("expected 0.50, got %f", p)
	}
}

// --- DefaultDecompositionRules Sanity ---

func TestDefaultDecompositionRules_Coverage(t *testing.T) {
	rules := DefaultDecompositionRules()
	expectedTypes := []string{"safety", "income", "efficiency", "operational", "learning", "evolution"}
	for _, typ := range expectedTypes {
		templates, ok := rules[typ]
		if !ok {
			t.Errorf("missing rules for type %s", typ)
			continue
		}
		if len(templates) == 0 {
			t.Errorf("empty templates for type %s", typ)
		}
		for i, tmpl := range templates {
			if tmpl.TitlePattern == "" {
				t.Errorf("type %s template %d: empty title", typ, i)
			}
			if tmpl.TargetMetric == "" {
				t.Errorf("type %s template %d: empty target metric", typ, i)
			}
			if tmpl.PreferredAction == "" {
				t.Errorf("type %s template %d: empty preferred action", typ, i)
			}
		}
	}
}

// --- Clamp tests ---

func TestClamp01(t *testing.T) {
	tests := []struct{ in, out float64 }{
		{-0.5, 0}, {0, 0}, {0.5, 0.5}, {1.0, 1.0}, {1.5, 1.0},
	}
	for _, tt := range tests {
		got := clamp01(tt.in)
		if got != tt.out {
			t.Errorf("clamp01(%f) = %f, want %f", tt.in, got, tt.out)
		}
	}
}

// --- Plan Status Transitions ---

func TestValidatePlanTransition(t *testing.T) {
	cases := []struct {
		from  PlanStatus
		to    PlanStatus
		valid bool
	}{
		{PlanDraft, PlanActive, true},
		{PlanActive, PlanCompleted, true},
		{PlanActive, PlanAbandoned, true},
		{PlanActive, PlanReplanning, true},
		{PlanReplanning, PlanActive, true},
		{PlanReplanning, PlanAbandoned, true},
		{PlanDraft, PlanCompleted, false},
		{PlanCompleted, PlanActive, false},
		{PlanAbandoned, PlanActive, false},
	}
	for _, tc := range cases {
		got := ValidatePlanTransition(tc.from, tc.to)
		if got != tc.valid {
			t.Errorf("%s→%s: expected %v, got %v", tc.from, tc.to, tc.valid, got)
		}
	}
}

// --- Strategy Tests ---

func TestSelectStrategy_Default(t *testing.T) {
	sg := Subgoal{FailureCount: 0, SuccessCount: 0}
	s := SelectStrategy(sg, 0)
	if s != StrategyExploitSuccess {
		t.Errorf("expected exploit_success_path, got %s", s)
	}
}

func TestSelectStrategy_RepeatedFailure(t *testing.T) {
	sg := Subgoal{FailureCount: 3, SuccessCount: 0}
	s := SelectStrategy(sg, 0)
	if s != StrategyReduceFailure {
		t.Errorf("expected reduce_failure_path, got %s", s)
	}
}

func TestSelectStrategy_ObjectivePenalty(t *testing.T) {
	sg := Subgoal{FailureCount: 0, SuccessCount: 0}
	s := SelectStrategy(sg, -0.10)
	if s != StrategyDeferHighRisk {
		t.Errorf("expected defer_high_risk, got %s", s)
	}
}

func TestSelectStrategy_Diversify(t *testing.T) {
	sg := Subgoal{FailureCount: 1, SuccessCount: 2}
	s := SelectStrategy(sg, 0)
	if s != StrategyDiversify {
		t.Errorf("expected diversify_attempts, got %s", s)
	}
}

func TestApplyStrategyToEmission_DeferHighRisk(t *testing.T) {
	em := TaskEmission{Priority: 0.80, RiskLevel: 0.10, ExpectedValue: 0.50}
	result := ApplyStrategyToEmission(em, StrategyDeferHighRisk)
	if result.Priority >= 0.80 {
		t.Error("defer should reduce priority")
	}
	if result.RiskLevel <= 0.10 {
		t.Error("defer should increase risk level")
	}
	if result.StrategyType != string(StrategyDeferHighRisk) {
		t.Errorf("expected strategy type defer_high_risk, got %s", result.StrategyType)
	}
}

func TestApplyStrategyToEmission_ExploitSuccess(t *testing.T) {
	em := TaskEmission{Priority: 0.80, RiskLevel: 0.10}
	result := ApplyStrategyToEmission(em, StrategyExploitSuccess)
	if result.Priority <= 0.80 {
		t.Error("exploit should boost priority")
	}
}

func TestShouldReplan_ObjectivePenalty(t *testing.T) {
	sg := Subgoal{}
	replan, trigger := ShouldReplan(sg, -0.10)
	if !replan {
		t.Error("expected replan for objective penalty")
	}
	if trigger != TriggerObjectivePenalty {
		t.Errorf("expected objective_penalty trigger, got %s", trigger)
	}
}

func TestShouldReplan_RepeatedFailure(t *testing.T) {
	sg := Subgoal{FailureCount: 3}
	replan, trigger := ShouldReplan(sg, 0)
	if !replan {
		t.Error("expected replan for repeated failure")
	}
	if trigger != TriggerRepeatedFailure {
		t.Errorf("expected repeated_failure trigger, got %s", trigger)
	}
}

func TestShouldReplan_SingleFailure(t *testing.T) {
	sg := Subgoal{FailureCount: 1, SuccessCount: 0}
	replan, trigger := ShouldReplan(sg, 0)
	if !replan {
		t.Error("expected replan for single failure")
	}
	if trigger != TriggerExecFailure {
		t.Errorf("expected execution_failure trigger, got %s", trigger)
	}
}

func TestShouldReplan_Reinforcement(t *testing.T) {
	sg := Subgoal{SuccessCount: 3, FailureCount: 0}
	replan, trigger := ShouldReplan(sg, 0)
	if !replan {
		t.Error("expected replan signal for reinforcement")
	}
	if trigger != TriggerReinforcement {
		t.Errorf("expected positive_reinforcement trigger, got %s", trigger)
	}
}

func TestShouldReplan_NoAction(t *testing.T) {
	sg := Subgoal{FailureCount: 0, SuccessCount: 1}
	replan, _ := ShouldReplan(sg, 0)
	if replan {
		t.Error("expected no replan for stable subgoal")
	}
}

// --- Dependency Graph Tests ---

func TestDependencyGraph_NoCycle(t *testing.T) {
	deps := []GoalDependency{
		{FromSubgoalID: "A", ToSubgoalID: "B"},
		{FromSubgoalID: "B", ToSubgoalID: "C"},
	}
	g := NewDependencyGraph(deps)
	if g.HasCycle() {
		t.Error("expected no cycle in linear chain")
	}
}

func TestDependencyGraph_WithCycle(t *testing.T) {
	deps := []GoalDependency{
		{FromSubgoalID: "A", ToSubgoalID: "B"},
		{FromSubgoalID: "B", ToSubgoalID: "C"},
		{FromSubgoalID: "C", ToSubgoalID: "A"},
	}
	g := NewDependencyGraph(deps)
	if !g.HasCycle() {
		t.Error("expected cycle in circular chain")
	}
}

func TestDependencyGraph_TopologicalSort(t *testing.T) {
	deps := []GoalDependency{
		{FromSubgoalID: "A", ToSubgoalID: "B"},
		{FromSubgoalID: "A", ToSubgoalID: "C"},
	}
	g := NewDependencyGraph(deps)
	order := g.TopologicalSort([]string{"A", "B", "C"})
	if order == nil {
		t.Fatal("expected non-nil order")
	}
	// B and C should come before A (they are dependencies OF A).
	aIdx := -1
	for i, id := range order {
		if id == "A" {
			aIdx = i
		}
	}
	if aIdx == -1 {
		t.Fatal("A not found in order")
	}
}

func TestDependencyGraph_TopologicalSort_Cycle(t *testing.T) {
	deps := []GoalDependency{
		{FromSubgoalID: "A", ToSubgoalID: "B"},
		{FromSubgoalID: "B", ToSubgoalID: "A"},
	}
	g := NewDependencyGraph(deps)
	order := g.TopologicalSort([]string{"A", "B"})
	if order != nil {
		t.Error("expected nil order for cyclic graph")
	}
}

func TestDependencyGraph_Depth(t *testing.T) {
	deps := []GoalDependency{
		{FromSubgoalID: "A", ToSubgoalID: "B"},
		{FromSubgoalID: "B", ToSubgoalID: "C"},
	}
	g := NewDependencyGraph(deps)
	depth := g.Depth("A")
	if depth != 3 {
		t.Errorf("expected depth 3 for A→B→C, got %d", depth)
	}
}

func TestDependencyGraph_MaxDepthValidation(t *testing.T) {
	// Depth 3 chain: A→B→C (depth=3), should pass MaxDepth=3.
	deps := []GoalDependency{
		{FromSubgoalID: "A", ToSubgoalID: "B"},
		{FromSubgoalID: "B", ToSubgoalID: "C"},
	}
	g := NewDependencyGraph(deps)
	if !g.ValidateMaxDepth([]string{"A", "B", "C"}) {
		t.Error("depth 3 should pass MaxDepth=3")
	}

	// Depth 4 chain: A→B→C→D (depth=4), should fail MaxDepth=3.
	deps = append(deps, GoalDependency{FromSubgoalID: "C", ToSubgoalID: "D"})
	g = NewDependencyGraph(deps)
	if g.ValidateMaxDepth([]string{"A", "B", "C", "D"}) {
		t.Error("depth 4 should fail MaxDepth=3")
	}
}

func TestDependencyGraph_Prerequisites(t *testing.T) {
	deps := []GoalDependency{
		{FromSubgoalID: "A", ToSubgoalID: "C"},
		{FromSubgoalID: "B", ToSubgoalID: "C"},
	}
	g := NewDependencyGraph(deps)
	prereqs := g.Prerequisites("C")
	if len(prereqs) != 2 {
		t.Errorf("expected 2 prerequisites for C, got %d", len(prereqs))
	}
}

// --- Plan Store Tests ---

func TestInMemoryPlanStore(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPlanStore()

	plan := GoalPlan{
		ID:       "plan-1",
		GoalID:   "goal-1",
		Version:  1,
		Strategy: StrategyExploitSuccess,
		Status:   PlanDraft,
	}
	if err := store.Insert(ctx, plan); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalID != "goal-1" {
		t.Errorf("expected goal-1, got %s", got.GoalID)
	}

	if err := store.UpdateStatus(ctx, "plan-1", PlanActive); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(ctx, "plan-1")
	if got.Status != PlanActive {
		t.Errorf("expected active, got %s", got.Status)
	}

	if err := store.IncrementVersion(ctx, "plan-1"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(ctx, "plan-1")
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}

	if err := store.IncrementReplanCount(ctx, "plan-1"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(ctx, "plan-1")
	if got.ReplanCount != 1 {
		t.Errorf("expected replan count 1, got %d", got.ReplanCount)
	}

	// GetByGoal
	byGoal, err := store.GetByGoal(ctx, "goal-1")
	if err != nil {
		t.Fatal(err)
	}
	if byGoal.ID != "plan-1" {
		t.Errorf("expected plan-1, got %s", byGoal.ID)
	}

	// ListAll
	all, err := store.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 plan, got %d", len(all))
	}
}

// --- Dependency Store Tests ---

func TestInMemoryDependencyStore(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryDependencyStore()

	dep := GoalDependency{
		ID:            "dep-1",
		PlanID:        "plan-1",
		FromSubgoalID: "sg-1",
		ToSubgoalID:   "sg-2",
	}
	if err := store.Insert(ctx, dep); err != nil {
		t.Fatal(err)
	}

	deps, err := store.ListByPlan(ctx, "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}

	if err := store.DeleteByPlan(ctx, "plan-1"); err != nil {
		t.Fatal(err)
	}
	deps, _ = store.ListByPlan(ctx, "plan-1")
	if len(deps) != 0 {
		t.Errorf("expected 0 deps after delete, got %d", len(deps))
	}
}

// --- Replanner Tests ---

type mockReflectionProvider struct {
	signals []ReflectionSignalInput
}

func (m *mockReflectionProvider) GetReflectionSignals(_ context.Context) []ReflectionSignalInput {
	return m.signals
}

type mockExecFeedbackProvider struct {
	feedback map[string][3]int // goalID → [successes, failures, consecutive]
}

func (m *mockExecFeedbackProvider) GetFeedbackForGoal(_ context.Context, goalID string) (int, int, int) {
	fb, ok := m.feedback[goalID]
	if !ok {
		return 0, 0, 0
	}
	return fb[0], fb[1], fb[2]
}

type mockObjectiveProvider struct {
	netUtility float64
}

func (m *mockObjectiveProvider) GetNetUtility() float64   { return m.netUtility }
func (m *mockObjectiveProvider) GetUtilityScore() float64 { return m.netUtility }
func (m *mockObjectiveProvider) GetRiskScore() float64    { return 0 }

func TestReplanner_RepeatedFailure(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	planStore := NewInMemoryPlanStore()

	sg := Subgoal{
		ID:       "sg-1",
		GoalID:   "goal-1",
		Status:   SubgoalActive,
		Strategy: StrategyExploitSuccess,
	}
	_ = sgStore.Insert(ctx, sg)

	replanner := NewReplanner(sgStore, planStore, nil, nil).
		WithExecutionFeedback(&mockExecFeedbackProvider{
			feedback: map[string][3]int{"goal-1": {0, 3, 3}},
		}).
		WithObjective(&mockObjectiveProvider{netUtility: 0.50})

	count, err := replanner.RunReplanCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 replanned, got %d", count)
	}

	// Check strategy changed.
	updated, _ := sgStore.Get(ctx, "sg-1")
	if updated.Strategy != StrategyReduceFailure {
		t.Errorf("expected reduce_failure_path, got %s", updated.Strategy)
	}
	// Should be blocked due to repeated failure.
	if updated.Status != SubgoalBlocked {
		t.Errorf("expected blocked status, got %s", updated.Status)
	}
}

func TestReplanner_ObjectivePenalty(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	planStore := NewInMemoryPlanStore()

	sg := Subgoal{
		ID:       "sg-1",
		GoalID:   "goal-1",
		Status:   SubgoalActive,
		Strategy: StrategyExploitSuccess,
	}
	_ = sgStore.Insert(ctx, sg)

	replanner := NewReplanner(sgStore, planStore, nil, nil).
		WithObjective(&mockObjectiveProvider{netUtility: 0.30}) // delta = -0.20

	count, err := replanner.RunReplanCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 replanned, got %d", count)
	}

	updated, _ := sgStore.Get(ctx, "sg-1")
	if updated.Strategy != StrategyDeferHighRisk {
		t.Errorf("expected defer_high_risk, got %s", updated.Strategy)
	}
}

func TestReplanner_PositiveReinforcement(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	planStore := NewInMemoryPlanStore()

	sg := Subgoal{
		ID:       "sg-1",
		GoalID:   "goal-1",
		Status:   SubgoalActive,
		Strategy: StrategyExploitSuccess,
		Priority: 0.70,
	}
	_ = sgStore.Insert(ctx, sg)

	replanner := NewReplanner(sgStore, planStore, nil, nil).
		WithExecutionFeedback(&mockExecFeedbackProvider{
			feedback: map[string][3]int{"goal-1": {3, 0, 0}},
		}).
		WithObjective(&mockObjectiveProvider{netUtility: 0.60})

	count, err := replanner.RunReplanCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 replanned (reinforcement), got %d", count)
	}

	updated, _ := sgStore.Get(ctx, "sg-1")
	if updated.Strategy != StrategyExploitSuccess {
		t.Errorf("expected exploit_success_path after reinforcement, got %s", updated.Strategy)
	}
}

func TestReplanner_NoAction(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	planStore := NewInMemoryPlanStore()

	sg := Subgoal{
		ID:       "sg-1",
		GoalID:   "goal-1",
		Status:   SubgoalActive,
		Strategy: StrategyExploitSuccess,
	}
	_ = sgStore.Insert(ctx, sg)

	replanner := NewReplanner(sgStore, planStore, nil, nil).
		WithObjective(&mockObjectiveProvider{netUtility: 0.50})

	count, err := replanner.RunReplanCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 replanned for stable state, got %d", count)
	}
}

func TestReplanner_WithReflectionSignals(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	planStore := NewInMemoryPlanStore()

	sg := Subgoal{
		ID:       "sg-1",
		GoalID:   "goal-1",
		Status:   SubgoalActive,
		Strategy: StrategyExploitSuccess,
	}
	_ = sgStore.Insert(ctx, sg)

	replanner := NewReplanner(sgStore, planStore, nil, nil).
		WithReflection(&mockReflectionProvider{
			signals: []ReflectionSignalInput{
				{SignalType: "execution_failure", Strength: 0.8, GoalID: "goal-1"},
			},
		}).
		WithObjective(&mockObjectiveProvider{netUtility: 0.50})

	count, err := replanner.RunReplanCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Reflection signal adds 1 failure → single failure → replan.
	if count != 1 {
		t.Errorf("expected 1 replanned, got %d", count)
	}
}

func TestReplanner_SkipsCompletedSubgoals(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	planStore := NewInMemoryPlanStore()

	sg := Subgoal{
		ID:     "sg-1",
		GoalID: "goal-1",
		Status: SubgoalCompleted,
	}
	_ = sgStore.Insert(ctx, sg)

	replanner := NewReplanner(sgStore, planStore, nil, nil).
		WithExecutionFeedback(&mockExecFeedbackProvider{
			feedback: map[string][3]int{"goal-1": {0, 5, 5}},
		}).
		WithObjective(&mockObjectiveProvider{netUtility: 0.50})

	count, _ := replanner.RunReplanCycle(ctx)
	if count != 0 {
		t.Errorf("expected 0 replanned for completed subgoal, got %d", count)
	}
}

// --- Engine Plan Integration Tests ---

func TestEngine_CreatePlan(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	pStore := NewInMemoryProgressStore()
	planStore := NewInMemoryPlanStore()

	engine := NewEngine(sgStore, pStore, nil, nil).
		WithPlanStore(planStore)

	plan, err := engine.CreatePlan(ctx, "goal-1", HorizonMedium, StrategyExploitSuccess)
	if err != nil {
		t.Fatal(err)
	}
	if plan.GoalID != "goal-1" {
		t.Errorf("expected goal-1, got %s", plan.GoalID)
	}
	if plan.Status != PlanDraft {
		t.Errorf("expected draft, got %s", plan.Status)
	}
	if plan.Version != 1 {
		t.Errorf("expected version 1, got %d", plan.Version)
	}
}

func TestEngine_ListPlans(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	pStore := NewInMemoryProgressStore()
	planStore := NewInMemoryPlanStore()

	engine := NewEngine(sgStore, pStore, nil, nil).
		WithPlanStore(planStore)

	_, _ = engine.CreatePlan(ctx, "goal-1", HorizonMedium, StrategyExploitSuccess)
	_, _ = engine.CreatePlan(ctx, "goal-2", HorizonLong, StrategyDiversify)

	plans, err := engine.ListPlans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Errorf("expected 2 plans, got %d", len(plans))
	}
}

func TestEngine_DecomposeGoals_CreatesPlan(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	pStore := NewInMemoryProgressStore()
	planStore := NewInMemoryPlanStore()
	depStore := NewInMemoryDependencyStore()

	engine := NewEngine(sgStore, pStore, nil, nil).
		WithPlanStore(planStore).
		WithDependencyStore(depStore)

	sysGoals := []goals.SystemGoal{
		{ID: "g1", Type: "safety", Priority: 0.90, Horizon: "daily"},
	}

	count, err := engine.DecomposeGoals(ctx, sysGoals)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 { // safety has 2 templates
		t.Errorf("expected 2 subgoals, got %d", count)
	}

	// Verify plan was created.
	plans, _ := planStore.ListAll(ctx)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].GoalID != "g1" {
		t.Errorf("expected plan for g1, got %s", plans[0].GoalID)
	}
	if plans[0].Status != PlanActive {
		t.Errorf("expected active plan, got %s", plans[0].Status)
	}

	// Verify dependencies were created (sequential).
	deps, _ := depStore.ListByPlan(ctx, plans[0].ID)
	if len(deps) != 1 {
		t.Errorf("expected 1 dependency (0→1), got %d", len(deps))
	}

	// Verify subgoals have plan_id set.
	sgs, _ := sgStore.ListAll(ctx)
	for _, sg := range sgs {
		if sg.PlanID != plans[0].ID {
			t.Errorf("subgoal %s missing plan_id", sg.ID)
		}
		if sg.Strategy != StrategyExploitSuccess {
			t.Errorf("subgoal %s expected exploit strategy, got %s", sg.ID, sg.Strategy)
		}
	}
}

func TestEngine_RunReplanCycle(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	pStore := NewInMemoryProgressStore()
	planStore := NewInMemoryPlanStore()

	replanner := NewReplanner(sgStore, planStore, nil, nil).
		WithObjective(&mockObjectiveProvider{netUtility: 0.30})

	engine := NewEngine(sgStore, pStore, nil, nil).
		WithReplanner(replanner)

	sg := Subgoal{
		ID:       "sg-1",
		GoalID:   "goal-1",
		Status:   SubgoalActive,
		Strategy: StrategyExploitSuccess,
	}
	_ = sgStore.Insert(ctx, sg)

	count, err := engine.RunReplanCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 replanned, got %d", count)
	}
}

func TestEngine_RunReplanCycle_NilReplanner(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	pStore := NewInMemoryProgressStore()

	engine := NewEngine(sgStore, pStore, nil, nil)

	count, err := engine.RunReplanCycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 with nil replanner, got %d", count)
	}
}

// --- MaxSubgoalsPerGoal Test ---

func TestMaxSubgoalsPerGoal(t *testing.T) {
	if MaxSubgoalsPerGoal != 5 {
		t.Errorf("expected MaxSubgoalsPerGoal=5, got %d", MaxSubgoalsPerGoal)
	}
}

func TestMaxTasksPerPlan(t *testing.T) {
	if MaxTasksPerPlan != 10 {
		t.Errorf("expected MaxTasksPerPlan=10, got %d", MaxTasksPerPlan)
	}
}

func TestMaxDepth(t *testing.T) {
	if MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %d", MaxDepth)
	}
}

// --- Horizon Mapping Tests ---

func TestHorizonShortMediumLong(t *testing.T) {
	if HorizonDays[HorizonShort] != 1 {
		t.Errorf("short should be 1 day")
	}
	if HorizonDays[HorizonMedium] != 7 {
		t.Errorf("medium should be 7 days")
	}
	if HorizonDays[HorizonLong] != 30 {
		t.Errorf("long should be 30 days")
	}
}

// --- Adapter Plan Tests ---

func TestGraphAdapter_ListPlans_NilSafe(t *testing.T) {
	var a *GraphAdapter
	plans := a.ListPlans(context.Background())
	if plans != nil {
		t.Error("expected nil from nil adapter")
	}
}

func TestGraphAdapter_Replan_NilSafe(t *testing.T) {
	var a *GraphAdapter
	count := a.Replan(context.Background(), "goal-1")
	if count != 0 {
		t.Errorf("expected 0 from nil adapter, got %d", count)
	}
}

func TestGraphAdapter_RunReplanCycle_NilSafe(t *testing.T) {
	var a *GraphAdapter
	count := a.RunReplanCycle(context.Background())
	if count != 0 {
		t.Errorf("expected 0 from nil adapter, got %d", count)
	}
}

// --- Strategy with Engine PlanAndEmitTasks ---

func TestEngine_PlanAndEmitTasks_AppliesStrategy(t *testing.T) {
	ctx := context.Background()
	sgStore := NewInMemorySubgoalStore()
	pStore := NewInMemoryProgressStore()

	now := time.Now().UTC()
	sg := Subgoal{
		ID:              "sg-1",
		GoalID:          "goal-1",
		Status:          SubgoalActive,
		Priority:        0.80,
		Horizon:         HorizonWeekly,
		PreferredAction: "analyze",
		ProgressScore:   0.20,
		FailureCount:    3, // will trigger reduce_failure strategy
		CreatedAt:       now.Add(-48 * time.Hour),
		UpdatedAt:       now,
	}
	_ = sgStore.Insert(ctx, sg)

	emitter := &mockEmitter{}

	engine := NewEngine(sgStore, pStore, nil, nil).
		WithEmitter(emitter)

	count, err := engine.PlanAndEmitTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 emitted, got %d", count)
	}
	if emitter.emitted[0].StrategyType != string(StrategyReduceFailure) {
		t.Errorf("expected reduce_failure strategy, got %s", emitter.emitted[0].StrategyType)
	}
}

// --- Causal proof: vector changes strategy selection and priorities ---

type mockVectorProvider struct {
	incomePriority   float64
	explorationLevel float64
	riskTolerance    float64
}

func (m *mockVectorProvider) GetIncomePriority() float64   { return m.incomePriority }
func (m *mockVectorProvider) GetExplorationLevel() float64 { return m.explorationLevel }
func (m *mockVectorProvider) GetRiskTolerance() float64    { return m.riskTolerance }

func TestCausalProof_VectorChangesStrategy(t *testing.T) {
	// A fresh subgoal with no failures and no successes.
	// Without vector: SelectStrategy → StrategyExploitSuccess
	// With high exploration (>0.50): SelectStrategyWithVector → StrategyDiversify
	sg := Subgoal{
		ID:            "sg-causal-1",
		GoalID:        "goal-1",
		Status:        SubgoalActive,
		Priority:      0.70,
		FailureCount:  0,
		SuccessCount:  0,
		ProgressScore: 0.30,
	}
	objectiveDelta := 0.0 // neutral

	// Baseline: no vector.
	baseline := SelectStrategy(sg, objectiveDelta)
	if baseline != StrategyExploitSuccess {
		t.Fatalf("expected exploit_success without vector, got %s", baseline)
	}

	// Low exploration (default level): should still exploit.
	lowExplore := SelectStrategyWithVector(sg, objectiveDelta, 0.30, 0.30)
	if lowExplore != StrategyExploitSuccess {
		t.Errorf("expected exploit_success with low exploration, got %s", lowExplore)
	}

	// High exploration: should diversify.
	highExplore := SelectStrategyWithVector(sg, objectiveDelta, 0.80, 0.30)
	if highExplore != StrategyDiversify {
		t.Errorf("CAUSAL FAILURE: expected diversify with high exploration, got %s", highExplore)
	}

	t.Logf("Causal proof: baseline=%s lowExplore=%s highExplore=%s", baseline, lowExplore, highExplore)
}

func TestCausalProof_VectorChangesDeferThreshold(t *testing.T) {
	// Subgoal with no failures but negative objective delta.
	sg := Subgoal{
		ID:            "sg-causal-2",
		GoalID:        "goal-1",
		Status:        SubgoalActive,
		Priority:      0.70,
		FailureCount:  0,
		SuccessCount:  1,
		ProgressScore: 0.40,
	}

	// Moderate penalty: objectiveDelta = -0.06 (just past ObjectivePenaltyThreshold=0.05).
	objectiveDelta := -0.06

	// Low risk tolerance (0.30): should defer (default behavior).
	lowRT := SelectStrategyWithVector(sg, objectiveDelta, 0.30, 0.30)
	if lowRT != StrategyDeferHighRisk {
		t.Errorf("expected defer with low risk tolerance, got %s", lowRT)
	}

	// High risk tolerance (0.90): threshold shifts from -0.05 to ≈-0.068, so -0.06 no longer triggers.
	highRT := SelectStrategyWithVector(sg, objectiveDelta, 0.30, 0.90)
	if highRT == StrategyDeferHighRisk {
		t.Errorf("CAUSAL FAILURE: high risk tolerance should prevent deferral at delta=%.3f, got %s", objectiveDelta, highRT)
	}

	t.Logf("Causal proof: lowRT=%s highRT=%s", lowRT, highRT)
}

func TestCausalProof_VectorChangesEmittedPriority(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	sgStore := NewInMemorySubgoalStore()
	pStore := NewInMemoryProgressStore()

	sg := Subgoal{
		ID:              "sg-vec-pri",
		GoalID:          "goal-1",
		Status:          SubgoalActive,
		Priority:        0.60,
		Horizon:         HorizonWeekly,
		PreferredAction: "discover",
		ProgressScore:   0.30,
		CreatedAt:       now.Add(-48 * time.Hour),
		UpdatedAt:       now,
	}
	_ = sgStore.Insert(ctx, sg)

	// Without vector: exploit_success adds +0.10 reinforcement boost to priority.
	emitterA := &mockEmitter{}
	engA := NewEngine(sgStore, pStore, nil, nil).
		WithObjective(&mockObjectiveProvider{netUtility: 0.50}).
		WithEmitter(emitterA)
	_, _ = engA.PlanAndEmitTasks(ctx)

	// Reset store timestamp so cooldown doesn't block second emission.
	_ = sgStore.UpdateLastTaskEmitted(ctx, "sg-vec-pri", time.Time{})

	// With high exploration vector: diversify_attempts adds +0.10 to RiskLevel
	// instead of priority boost. Also income boost adds to urgency-derived priority.
	emitterB := &mockEmitter{}
	engB := NewEngine(sgStore, pStore, nil, nil).
		WithObjective(&mockObjectiveProvider{netUtility: 0.50}).
		WithVector(&mockVectorProvider{incomePriority: 1.00, explorationLevel: 0.80, riskTolerance: 0.30}).
		WithEmitter(emitterB)
	_, _ = engB.PlanAndEmitTasks(ctx)

	if len(emitterA.emitted) == 0 || len(emitterB.emitted) == 0 {
		t.Fatal("expected emissions from both engines")
	}

	// With high exploration, the strategy should switch to diversify,
	// which increases risk level by 0.10.
	stratNoVec := emitterA.emitted[0].StrategyType
	stratWithVec := emitterB.emitted[0].StrategyType
	riskNoVec := emitterA.emitted[0].RiskLevel
	riskWithVec := emitterB.emitted[0].RiskLevel

	if stratNoVec == stratWithVec {
		t.Errorf("CAUSAL FAILURE: vector should change strategy: without=%s with=%s", stratNoVec, stratWithVec)
	}
	if riskWithVec <= riskNoVec {
		t.Errorf("CAUSAL FAILURE: diversify strategy should increase risk level: without=%.4f with=%.4f",
			riskNoVec, riskWithVec)
	}

	t.Logf("Causal proof: stratNoVec=%s stratWithVec=%s riskNoVec=%.4f riskWithVec=%.4f",
		stratNoVec, stratWithVec, riskNoVec, riskWithVec)
}
