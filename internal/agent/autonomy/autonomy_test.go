package autonomy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Mock implementations ---

type mockAuditor struct {
	mu     sync.Mutex
	events []auditedEvent
}

type auditedEvent struct {
	entityType string
	eventType  string
	payload    any
}

func (m *mockAuditor) RecordEvent(_ context.Context, entityType string, _ uuid.UUID, eventType, _, _ string, payload any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, auditedEvent{entityType: entityType, eventType: eventType, payload: payload})
	return nil
}

func (m *mockAuditor) getEvents() []auditedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]auditedEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

func (m *mockAuditor) hasEvent(eventType string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.events {
		if e.eventType == eventType {
			return true
		}
	}
	return false
}

type mockReflection struct {
	mu     sync.Mutex
	called int
	result ReflectionResult
	err    error
}

func (m *mockReflection) RunReflection(_ context.Context, _ bool) (ReflectionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called++
	return m.result, m.err
}

func (m *mockReflection) getCalled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

type mockObjective struct {
	mu     sync.Mutex
	called int
	result ObjectiveResult
	err    error
}

func (m *mockObjective) Recompute(_ context.Context) (ObjectiveResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called++
	return m.result, m.err
}

func (m *mockObjective) getCalled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

type mockActuation struct {
	mu        sync.Mutex
	runCalled int
	result    ActuationResult
	decisions []ActuationDecisionInfo
	err       error
}

func (m *mockActuation) Run(_ context.Context) (ActuationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCalled++
	return m.result, m.err
}

func (m *mockActuation) ListDecisions(_ context.Context, _ int) ([]ActuationDecisionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decisions, nil
}

type mockScheduling struct {
	mu     sync.Mutex
	called int
}

func (m *mockScheduling) RecomputeSlots(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called++
	return nil
}

type mockPortfolio struct {
	mu     sync.Mutex
	called int
}

func (m *mockPortfolio) Rebalance(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called++
	return nil
}

type mockDiscovery struct {
	mu     sync.Mutex
	called int
}

func (m *mockDiscovery) Run(_ context.Context) (DiscoveryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called++
	return DiscoveryResult{CandidatesFound: 2}, nil
}

type mockSelfExt struct {
	proposals []SelfExtProposalInfo
}

func (m *mockSelfExt) ListProposals(_ context.Context, _ int) ([]SelfExtProposalInfo, error) {
	return m.proposals, nil
}

type mockPressure struct {
	score   float64
	urgency string
}

func (m *mockPressure) GetPressure(_ context.Context) (float64, string) {
	return m.score, m.urgency
}

type mockCapacity struct {
	load float64
}

func (m *mockCapacity) GetOwnerLoadScore(_ context.Context) float64 {
	return m.load
}

type mockGovernance struct {
	mu   sync.Mutex
	mode string
	err  error
}

func (m *mockGovernance) GetMode(_ context.Context) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mode
}

func (m *mockGovernance) SetMode(_ context.Context, mode, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
	return m.err
}

// --- Test helpers ---

func testLogger() *zap.Logger {
	return zap.NewNop()
}

func minimalConfig() *AutonomyConfig {
	return &AutonomyConfig{
		Mode: ModeSupervisedAutonomy,
		Governance: GovernanceCfg{
			StartupMode:              "supervised_autonomy",
			AllowReflection:          true,
			AllowObjectiveRecompute:  true,
			AllowActuation:           true,
			AllowPortfolioRebalance:  true,
			AllowDiscovery:           true,
			AllowSchedulingRecompute: true,
			AllowSelfExtension:       true,
		},
		Scheduler: SchedulerCfg{
			Enabled:     true,
			TickSeconds: 1,
			Cycles: CyclesCfg{
				ReflectionHours: 1,
				ObjectiveHours:  1,
				ActuationHours:  1,
				SchedulingHours: 1,
				PortfolioHours:  1,
				DiscoveryHours:  1,
				SelfExtHours:    1,
				ReportingHours:  1,
			},
			Bootstrap: BootstrapCfg{},
		},
		ExecWindow: ExecutionWindow{
			Enabled: false, // no window => always active
		},
		Limits: LimitsCfg{
			MaxActionsPerCycle:          5,
			MaxHeavyActionsPerCycle:     2,
			MaxReviewRequiredPerCycle:   3,
			MaxSelfExtPerDay:            2,
			MaxExtActionsPerDay:         5,
			MaxReportsPerDay:            12,
			MaxConsecutiveFailedCycles:  3,
			MaxDuplicateDecisionRepeats: 1,
			Dedupe: DedupeCfg{
				Actuation:     24,
				SelfExtension: 72,
				ExternalDraft: 24,
			},
		},
		Risk: RiskCfg{
			AutoDowngrade: true,
			DowngradeMode: "supervised_autonomy",
			Thresholds: RiskThresh{
				FailureRate: 0.40,
				Overload:    0.75,
				Pressure:    0.80,
				ReviewQueue: 15,
			},
			Recovery: RecoveryCfg{
				RequireConsecutiveHealthy: 3,
				AutoRestorePrevMode:       false,
			},
		},
		Actuation: ActuationCfg{
			Enabled:       true,
			AllowRun:      true,
			AllowRouting:  true,
			AllowExecSafe: true,
		},
		Scheduling: SchedulingCfg{
			Enabled:        true,
			AllowRecompute: true,
		},
		SelfExt: SelfExtCfg{
			Enabled: true,
			AutoDeploy: struct {
				Enabled                       bool `yaml:"enabled"`
				OnlyLowRisk                   bool `yaml:"only_low_risk"`
				RequireAllTestsPass           bool `yaml:"require_all_tests_pass"`
				RequireNoExternalEffects      bool `yaml:"require_no_external_effects"`
				RequireNoCorePipelineMutation bool `yaml:"require_no_core_pipeline_mutation"`
			}{
				Enabled:     false,
				OnlyLowRisk: true,
			},
		},
		Reporting: ReportingCfg{
			Enabled: true,
		},
		Observe: ObservabilityCfg{
			EmitAuditEvents: true,
			LogCycleSummary: true,
			LogSuppressed:   true,
			LogDowngrades:   true,
		},
	}
}

// --- Config tests ---

func TestLoadAutonomyConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autonomy.yaml")
	content := `
mode: supervised_autonomy
governance:
  startup_mode: supervised_autonomy
  allow_reflection: true
  allow_objective_recompute: true
scheduler:
  enabled: true
  tick_seconds: 60
  cycles:
    reflection_hours: 4
    objective_hours: 4
    actuation_hours: 4
execution_window:
  enabled: true
  start: "09:00"
  end: "18:00"
  timezone: local
limits:
  max_actions_per_cycle: 5
  max_consecutive_failed_cycles: 3
risk:
  auto_downgrade: true
  thresholds:
    failure_rate_threshold: 0.4
    overload_threshold: 0.75
    pressure_threshold: 0.8
`
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)

	cfg, err := LoadAutonomyConfig(path)
	require.NoError(t, err)
	assert.Equal(t, ModeSupervisedAutonomy, cfg.Mode)
	assert.True(t, cfg.Scheduler.Enabled)
	assert.Equal(t, 60, cfg.Scheduler.TickSeconds)
	assert.Equal(t, 4, cfg.Scheduler.Cycles.ReflectionHours)
	assert.True(t, cfg.ExecWindow.Enabled)
	assert.Equal(t, "09:00", cfg.ExecWindow.Start)
	assert.Equal(t, 5, cfg.Limits.MaxActionsPerCycle)
}

func TestLoadAutonomyConfig_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	content := `
mode: yolo
scheduler:
  tick_seconds: 60
  cycles:
    reflection_hours: 4
    objective_hours: 4
limits:
  max_actions_per_cycle: 5
  max_consecutive_failed_cycles: 3
risk:
  thresholds:
    failure_rate_threshold: 0.4
    overload_threshold: 0.75
    pressure_threshold: 0.8
`
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)

	_, err = LoadAutonomyConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode must be one of")
}

func TestLoadAutonomyConfig_MissingFile(t *testing.T) {
	_, err := LoadAutonomyConfig("/nonexistent/path.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read autonomy config")
}

func TestLoadAutonomyConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	err := os.WriteFile(path, []byte(":::not yaml"), 0o644)
	require.NoError(t, err)

	_, err = LoadAutonomyConfig(path)
	require.Error(t, err)
}

// --- Execution window tests ---

func TestIsInsideWindow(t *testing.T) {
	cfg := &AutonomyConfig{
		ExecWindow: ExecutionWindow{
			Enabled: true,
			Start:   "09:00",
			End:     "18:00",
		},
	}

	assert.True(t, cfg.IsInsideWindow(time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)))
	assert.True(t, cfg.IsInsideWindow(time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)))
	assert.True(t, cfg.IsInsideWindow(time.Date(2026, 4, 10, 17, 59, 0, 0, time.UTC)))
	assert.False(t, cfg.IsInsideWindow(time.Date(2026, 4, 10, 18, 0, 0, 0, time.UTC)))
	assert.False(t, cfg.IsInsideWindow(time.Date(2026, 4, 10, 8, 59, 0, 0, time.UTC)))
	assert.False(t, cfg.IsInsideWindow(time.Date(2026, 4, 10, 22, 0, 0, 0, time.UTC)))
}

func TestIsInsideWindow_Disabled(t *testing.T) {
	cfg := &AutonomyConfig{
		ExecWindow: ExecutionWindow{Enabled: false},
	}
	assert.True(t, cfg.IsInsideWindow(time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)))
}

// --- Orchestrator lifecycle tests ---

func TestOrchestrator_StartStop(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60 // slow to avoid tick during test
	cfg.Scheduler.Bootstrap = BootstrapCfg{}
	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	state := orch.GetState()
	assert.True(t, state.Running)
	assert.Equal(t, ModeSupervisedAutonomy, state.Mode)
	assert.NotNil(t, state.StartedAt)
	assert.True(t, auditor.hasEvent("autonomy.started"))

	// Double start should fail.
	err = orch.Start(ctx)
	assert.Error(t, err)

	orch.Stop(ctx)
	state = orch.GetState()
	assert.False(t, state.Running)
	assert.True(t, auditor.hasEvent("autonomy.stopped"))
}

func TestOrchestrator_FrozenMode_NoWork(t *testing.T) {
	cfg := minimalConfig()
	cfg.Mode = ModeFrozen
	cfg.Scheduler.TickSeconds = 60
	auditor := &mockAuditor{}
	ref := &mockReflection{result: ReflectionResult{SignalCount: 3}}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithReflection(ref)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	// Manually trigger tick - should do nothing in frozen mode.
	orch.tick()
	assert.Equal(t, 0, ref.getCalled())

	orch.Stop(ctx)
}

// --- Cycle execution tests ---

func TestOrchestrator_ReflectionCycleFires(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	auditor := &mockAuditor{}
	ref := &mockReflection{result: ReflectionResult{SignalCount: 2, RiskFlags: 1}}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithReflection(ref)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	// Manually trigger cycle.
	err = orch.runCycle(ctx, "reflection", time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, ref.getCalled())
	assert.True(t, auditor.hasEvent("autonomy.cycle_started"))
	assert.True(t, auditor.hasEvent("autonomy.cycle_completed"))

	orch.Stop(ctx)
}

func TestOrchestrator_ObjectiveCycleFires(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	auditor := &mockAuditor{}
	obj := &mockObjective{result: ObjectiveResult{NetUtility: 0.65, RiskScore: 0.30}}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithObjective(obj)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	err = orch.runCycle(ctx, "objective", time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, obj.getCalled())

	orch.Stop(ctx)
}

func TestOrchestrator_CycleDoesNotRunOutsideWindow(t *testing.T) {
	cfg := minimalConfig()
	cfg.ExecWindow.Enabled = true
	cfg.ExecWindow.Start = "09:00"
	cfg.ExecWindow.End = "18:00"
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	sched := &mockScheduling{}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithScheduling(sched)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	// Set last cycle times to past to make them "due".
	orch.state.mu.Lock()
	orch.state.LastCycleTimes["scheduling"] = time.Now().Add(-48 * time.Hour)
	orch.state.mu.Unlock()

	// Check dueCycles outside window.
	outsideTime := time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)
	due := orch.dueCycles(outsideTime, false)
	// Scheduling requires window and is not allowed outside.
	hasScheduling := false
	for _, c := range due {
		if c == "scheduling" {
			hasScheduling = true
		}
	}
	assert.False(t, hasScheduling, "scheduling should not run outside window")

	orch.Stop(ctx)
}

func TestOrchestrator_ForceManualTriggerWorks(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	auditor := &mockAuditor{}
	port := &mockPortfolio{}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithPortfolio(port)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	// Manually trigger a cycle directly.
	err = orch.runCycle(ctx, "portfolio", time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, port.called)

	orch.Stop(ctx)
}

// --- Safety / downgrade tests ---

func TestOrchestrator_HighFailureRateDowngrades(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	cfg.Limits.MaxConsecutiveFailedCycles = 2
	cfg.Risk.AutoDowngrade = true
	cfg.Risk.DowngradeMode = "supervised_autonomy"

	auditor := &mockAuditor{}
	// Wire a provider that always fails so cycles will fail.
	ref := &mockReflection{err: fmt.Errorf("simulated failure")}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithReflection(ref)
	orch.state.Mode = ModeAutonomous
	orch.state.OriginalMode = ModeAutonomous

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	// Force cycles to be due by setting last run to distant past.
	orch.state.mu.Lock()
	orch.state.LastCycleTimes["reflection"] = time.Now().Add(-48 * time.Hour)
	orch.state.mu.Unlock()

	// Two ticks with failure → should trigger downgrade.
	orch.tick()
	orch.tick()

	state := orch.GetState()
	assert.True(t, state.Downgraded)
	assert.Equal(t, ModeSupervisedAutonomy, state.Mode)
	assert.True(t, state.HeavyActionsDisabled)
	assert.True(t, state.SelfExtDisabled)
	assert.True(t, auditor.hasEvent("autonomy.downgraded"))

	orch.Stop(ctx)
}

func TestOrchestrator_OverloadDisablesHeavyActions(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	cfg.Risk.AutoDowngrade = true
	cfg.Risk.Thresholds.Overload = 0.75

	auditor := &mockAuditor{}
	cap := &mockCapacity{load: 0.85}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithCapacity(cap)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	orch.runSafetyChecks(ctx)

	state := orch.GetState()
	assert.True(t, state.HeavyActionsDisabled)

	orch.Stop(ctx)
}

func TestOrchestrator_PressureSpikeDowngrades(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	cfg.Risk.AutoDowngrade = true
	cfg.Risk.Thresholds.Pressure = 0.80

	auditor := &mockAuditor{}
	pressure := &mockPressure{score: 0.90, urgency: "critical"}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithPressure(pressure)
	orch.state.Mode = ModeAutonomous

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	orch.runSafetyChecks(ctx)

	state := orch.GetState()
	assert.True(t, state.Downgraded)
	assert.Contains(t, state.DowngradeReason, "pressure_threshold_exceeded")
	assert.True(t, auditor.hasEvent("autonomy.downgraded"))

	orch.Stop(ctx)
}

func TestOrchestrator_DowngradeIsAudited(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	ctx := context.Background()
	orch.downgrade(ctx, "test_downgrade")

	assert.True(t, auditor.hasEvent("autonomy.downgraded"))
	state := orch.GetState()
	assert.True(t, state.Downgraded)
	assert.Equal(t, "test_downgrade", state.DowngradeReason)
}

// --- Dedupe tests ---

func TestDedupe_DuplicateActuationSuppressed(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	act := &mockActuation{
		result: ActuationResult{DecisionCount: 1},
		decisions: []ActuationDecisionInfo{
			{
				ID:             "d1",
				Type:           "shift_scheduling",
				Status:         "proposed",
				RequiresReview: false,
				Priority:       0.8,
				ProposedAt:     time.Now(),
			},
		},
	}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithActuation(act)

	ctx := context.Background()

	// First run - should route.
	err := orch.cycleActuation(ctx)
	require.NoError(t, err)
	state := orch.GetState()
	assert.Equal(t, 1, state.SafeActionsRouted)

	// Second run - should suppress (dedupe).
	err = orch.cycleActuation(ctx)
	require.NoError(t, err)
	state = orch.GetState()
	assert.Equal(t, 1, state.SafeActionsRouted) // still 1, not 2
	assert.Equal(t, 1, state.SuppressedDecisions)
}

func TestDedupe_SelfExtSuppressed(t *testing.T) {
	tracker := newDedupeTracker()
	assert.False(t, tracker.IsDuplicate("selfext:p1", 72*time.Hour))
	assert.True(t, tracker.IsDuplicate("selfext:p1", 72*time.Hour))
}

// --- Safe execution policy tests ---

func TestOrchestrator_SafeInternalActionAllowed(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	act := &mockActuation{
		result: ActuationResult{DecisionCount: 1},
		decisions: []ActuationDecisionInfo{
			{
				ID:             "d-safe",
				Type:           "shift_scheduling",
				Status:         "proposed",
				RequiresReview: false,
				Priority:       0.7,
				ProposedAt:     time.Now(),
			},
		},
	}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithActuation(act)

	ctx := context.Background()
	err := orch.cycleActuation(ctx)
	require.NoError(t, err)

	state := orch.GetState()
	assert.Equal(t, 1, state.SafeActionsRouted)
	assert.True(t, auditor.hasEvent("autonomy.safe_action_routed"))
}

func TestOrchestrator_ReviewRequiredActionQueued(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	act := &mockActuation{
		result: ActuationResult{DecisionCount: 1},
		decisions: []ActuationDecisionInfo{
			{
				ID:             "d-review",
				Type:           "adjust_pricing",
				Status:         "proposed",
				RequiresReview: true,
				Priority:       0.9,
				ProposedAt:     time.Now(),
			},
		},
	}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithActuation(act)

	ctx := context.Background()
	err := orch.cycleActuation(ctx)
	require.NoError(t, err)

	state := orch.GetState()
	assert.Equal(t, 0, state.SafeActionsRouted)
	assert.Equal(t, 1, state.ReviewActionsQueued)
	assert.True(t, auditor.hasEvent("autonomy.review_action_queued"))
}

func TestOrchestrator_HeavyActionBlockedWhenDisabled(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	act := &mockActuation{
		result: ActuationResult{DecisionCount: 1},
		decisions: []ActuationDecisionInfo{
			{
				ID:             "d-heavy",
				Type:           "rebalance_portfolio",
				Status:         "proposed",
				RequiresReview: false,
				Priority:       0.8,
				ProposedAt:     time.Now(),
			},
		},
	}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithActuation(act)

	// Disable heavy actions.
	orch.state.mu.Lock()
	orch.state.HeavyActionsDisabled = true
	orch.state.mu.Unlock()

	ctx := context.Background()
	err := orch.cycleActuation(ctx)
	require.NoError(t, err)

	state := orch.GetState()
	assert.Equal(t, 0, state.SafeActionsRouted)
	assert.Equal(t, 1, state.SuppressedDecisions)
}

func TestOrchestrator_SelfExtBlockedWhenDisabled(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	cfg.SelfExt.AutoDeploy.Enabled = true

	auditor := &mockAuditor{}
	selfExt := &mockSelfExt{
		proposals: []SelfExtProposalInfo{{ID: "p1", Status: "validated", Title: "Test"}},
	}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithSelfExtension(selfExt)

	// Disable self-extension.
	orch.state.mu.Lock()
	orch.state.SelfExtDisabled = true
	orch.state.mu.Unlock()

	ctx := context.Background()
	err := orch.cycleSelfExtension(ctx)
	require.NoError(t, err)

	state := orch.GetState()
	assert.Equal(t, 1, state.SelfExtBlocked)
	assert.True(t, auditor.hasEvent("autonomy.self_extension_blocked"))
}

// --- Reporting tests ---

func TestOrchestrator_OperationalReportCreated(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	ctx := context.Background()
	err := orch.cycleReporting(ctx)
	require.NoError(t, err)

	reports := orch.GetReports(10)
	require.Len(t, reports, 1)
	assert.Equal(t, "operational", reports[0].Type)
	assert.True(t, auditor.hasEvent("autonomy.report_created"))
}

func TestOrchestrator_ExceptionReportTriggered(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	ctx := context.Background()
	orch.CreateExceptionReport(ctx, "failure_spike")

	reports := orch.GetReports(10)
	require.Len(t, reports, 1)
	assert.Equal(t, "exception", reports[0].Type)
	assert.Equal(t, "failure_spike", reports[0].ExceptionTrigger)
}

func TestOrchestrator_DailyReportCreated(t *testing.T) {
	// Daily report is essentially the operational report, tested same way.
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	ctx := context.Background()
	err := orch.cycleReporting(ctx)
	require.NoError(t, err)
	err = orch.cycleReporting(ctx)
	require.NoError(t, err)

	reports := orch.GetReports(10)
	assert.Len(t, reports, 2)
}

// --- Config reload tests ---

func TestOrchestrator_ReloadConfig(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	ctx := context.Background()
	newCfg := minimalConfig()
	newCfg.Mode = ModeAutonomous
	err := orch.ReloadConfig(ctx, newCfg)
	require.NoError(t, err)

	state := orch.GetState()
	assert.Equal(t, ModeAutonomous, state.Mode)
	assert.False(t, state.Downgraded)
	assert.True(t, auditor.hasEvent("autonomy.config_reloaded"))
}

// --- SetMode tests ---

func TestOrchestrator_SetMode(t *testing.T) {
	cfg := minimalConfig()
	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	ctx := context.Background()
	err := orch.SetMode(ctx, ModeFrozen)
	require.NoError(t, err)

	state := orch.GetState()
	assert.Equal(t, ModeFrozen, state.Mode)
}

func TestOrchestrator_SetMode_Invalid(t *testing.T) {
	cfg := minimalConfig()
	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())

	err := orch.SetMode(context.Background(), "yolo")
	assert.Error(t, err)
}

// --- Bootstrap tests ---

func TestOrchestrator_BootstrapRunsCycles(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	cfg.Scheduler.Bootstrap = BootstrapCfg{
		RunReflectionOnce: true,
		RunObjectiveOnce:  true,
		RunActuationOnce:  true,
		RunReportingOnce:  true,
	}

	auditor := &mockAuditor{}
	ref := &mockReflection{result: ReflectionResult{SignalCount: 1}}
	obj := &mockObjective{result: ObjectiveResult{NetUtility: 0.5}}
	act := &mockActuation{result: ActuationResult{DecisionCount: 0}}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithReflection(ref).
		WithObjective(obj).
		WithActuation(act)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	// Bootstrap should have run cycles.
	assert.Equal(t, 1, ref.getCalled())
	// obj called twice: once for bootstrap objective, once for reporting snapshot.
	assert.Equal(t, 2, obj.getCalled())

	state := orch.GetState()
	assert.Equal(t, 1, state.CyclesRun["reflection"])
	assert.Equal(t, 1, state.CyclesRun["objective"])
	assert.Equal(t, 1, state.CyclesRun["reporting"])

	orch.Stop(ctx)
}

// --- Governance activation tests ---

func TestOrchestrator_SetsGovernanceModeOnStart(t *testing.T) {
	cfg := minimalConfig()
	cfg.Governance.StartupMode = "supervised_autonomy"
	cfg.Scheduler.TickSeconds = 60

	auditor := &mockAuditor{}
	gov := &mockGovernance{mode: "normal"}
	orch := NewOrchestrator(cfg, auditor, testLogger()).
		WithGovernance(gov)

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	assert.Equal(t, "supervised_autonomy", gov.GetMode(ctx))

	orch.Stop(ctx)
}

// --- CycleDuration tests ---

func TestCycleDuration(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.Cycles.ReflectionHours = 4

	assert.Equal(t, 4*time.Hour, cfg.CycleDuration("reflection"))
	assert.Equal(t, 24*time.Hour, cfg.CycleDuration("unknown_cycle")) // default
}

// --- Dedupe cleanup tests ---

func TestDedupe_Cleanup(t *testing.T) {
	tracker := newDedupeTracker()
	tracker.seen["old"] = time.Now().Add(-100 * time.Hour)
	tracker.seen["new"] = time.Now()

	tracker.Cleanup(48 * time.Hour)

	assert.Contains(t, tracker.seen, "new")
	assert.NotContains(t, tracker.seen, "old")
}

// --- Nil provider fail-open tests ---

func TestOrchestrator_NilProviders_FailOpen(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scheduler.TickSeconds = 60
	auditor := &mockAuditor{}
	orch := NewOrchestrator(cfg, auditor, testLogger())
	// No providers wired.

	ctx := context.Background()
	err := orch.Start(ctx)
	require.NoError(t, err)

	// All cycles should succeed with no providers.
	assert.NoError(t, orch.cycleReflection(ctx))
	assert.NoError(t, orch.cycleObjective(ctx))
	assert.NoError(t, orch.cycleActuation(ctx))
	assert.NoError(t, orch.cycleScheduling(ctx))
	assert.NoError(t, orch.cyclePortfolio(ctx))
	assert.NoError(t, orch.cycleDiscovery(ctx))
	assert.NoError(t, orch.cycleSelfExtension(ctx))
	assert.NoError(t, orch.cycleReporting(ctx))

	orch.Stop(ctx)
}

// --- Production config loads correctly ---

func TestLoadProductionAutonomyConfig(t *testing.T) {
	// This test validates the actual configs/autonomy.yaml in the root.
	prodPath := "../../configs/autonomy.yaml"
	if _, err := os.Stat(prodPath); os.IsNotExist(err) {
		// Try from test runner root.
		prodPath = "configs/autonomy.yaml"
	}
	if _, err := os.Stat(prodPath); os.IsNotExist(err) {
		t.Skip("production autonomy.yaml not found")
	}

	cfg, err := LoadAutonomyConfig(prodPath)
	require.NoError(t, err)
	assert.NotEmpty(t, string(cfg.Mode))
	assert.True(t, cfg.Scheduler.Enabled)
}

// --- Validation edge cases ---

func TestValidation_BadExecutionWindow(t *testing.T) {
	cfg := minimalConfig()
	cfg.ExecWindow.Enabled = true
	cfg.ExecWindow.Start = "25:00"
	cfg.ExecWindow.End = "18:00"

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start must be HH:MM")
}

func TestValidation_ZeroActions(t *testing.T) {
	cfg := minimalConfig()
	cfg.Limits.MaxActionsPerCycle = 0

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_actions_per_cycle must be > 0")
}

func TestValidation_BadRiskThresholds(t *testing.T) {
	cfg := minimalConfig()
	cfg.Risk.Thresholds.FailureRate = 1.5

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failure_rate_threshold must be in (0,1]")
}
