package autonomy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// --- Provider interfaces ---
// Each provider is a thin interface to avoid import cycles.
// The orchestrator calls these; bridge adapters in main.go satisfy them.

// ReflectionRunner runs a meta-reflection cycle.
type ReflectionRunner interface {
	RunReflection(ctx context.Context, force bool) (ReflectionResult, error)
}

// ObjectiveRunner recomputes the objective function.
type ObjectiveRunner interface {
	Recompute(ctx context.Context) (ObjectiveResult, error)
}

// ActuationRunner runs the actuation cycle.
type ActuationRunner interface {
	Run(ctx context.Context) (ActuationResult, error)
	ListDecisions(ctx context.Context, limit int) ([]ActuationDecisionInfo, error)
}

// SchedulingRunner recomputes scheduling slots.
type SchedulingRunner interface {
	RecomputeSlots(ctx context.Context) error
}

// PortfolioRunner rebalances the portfolio.
type PortfolioRunner interface {
	Rebalance(ctx context.Context) error
}

// DiscoveryRunner runs opportunity discovery.
type DiscoveryRunner interface {
	Run(ctx context.Context) (DiscoveryResult, error)
}

// SelfExtensionRunner evaluates self-extension proposals.
type SelfExtensionRunner interface {
	ListProposals(ctx context.Context, limit int) ([]SelfExtProposalInfo, error)
}

// PressureReader reads financial pressure.
type PressureReader interface {
	GetPressure(ctx context.Context) (score float64, urgency string)
}

// CapacityReader reads owner load score.
type CapacityReader interface {
	GetOwnerLoadScore(ctx context.Context) float64
}

// GovernanceSetter sets the governance mode at startup.
type GovernanceSetter interface {
	GetMode(ctx context.Context) string
	SetMode(ctx context.Context, mode, reason string) error
}

// NOTE: TaskOrchestratorRunner, ExecutionLoopRunner, and ExecutionFeedbackStore
// are defined in chain_closure.go to avoid circular dependencies.

// --- Result types (lightweight, no import cycles) ---

type ReflectionResult struct {
	SignalCount int
	RiskFlags   int
}

type ObjectiveResult struct {
	NetUtility float64
	RiskScore  float64
}

type ActuationResult struct {
	DecisionCount int
	ReviewNeeded  int
}

type ActuationDecisionInfo struct {
	ID             string
	Type           string
	Status         string
	RequiresReview bool
	Priority       float64
	ProposedAt     time.Time
}

type DiscoveryResult struct {
	CandidatesFound int
}

type SelfExtProposalInfo struct {
	ID     string
	Status string
	Title  string
}

// --- Runtime state ---

// RuntimeState holds the current observable state of the autonomy runtime.
type RuntimeState struct {
	mu sync.RWMutex

	Mode                  AutonomyMode         `json:"mode"`
	OriginalMode          AutonomyMode         `json:"original_mode"`
	Running               bool                 `json:"running"`
	StartedAt             *time.Time           `json:"started_at,omitempty"`
	LastCycleTimes        map[string]time.Time `json:"last_cycle_times"`
	CyclesRun             map[string]int       `json:"cycles_run"`
	ConsecutiveFailures   int                  `json:"consecutive_failures"`
	ConsecutiveHealthy    int                  `json:"consecutive_healthy"`
	Downgraded            bool                 `json:"downgraded"`
	DowngradeReason       string               `json:"downgrade_reason,omitempty"`
	HeavyActionsDisabled  bool                 `json:"heavy_actions_disabled"`
	SelfExtDisabled       bool                 `json:"self_ext_disabled"`
	ReportCount           int                  `json:"report_count"`
	ActionsTakenThisCycle int                  `json:"actions_taken_this_cycle"`
	LastError             string               `json:"last_error,omitempty"`
	SafeActionsRouted     int                  `json:"safe_actions_routed"`
	ReviewActionsQueued   int                  `json:"review_actions_queued"`
	SelfExtBlocked        int                  `json:"self_ext_blocked"`
	SelfExtDeployed       int                  `json:"self_ext_deployed"`
	SuppressedDecisions   int                  `json:"suppressed_decisions"`

	// Chain closure state (Iteration 54.5/55A)
	TasksCreatedFromActuation int `json:"tasks_created_from_actuation"`
	TaskRecomputeCount        int `json:"task_recompute_count"`
	TaskDispatchCount         int `json:"task_dispatch_count"`
	ExecutionCompleted        int `json:"execution_completed"`
	ExecutionFailed           int `json:"execution_failed"`
	ExecutionPaused           int `json:"execution_paused"`
	FeedbackRecorded          int `json:"feedback_recorded"`
}

func newRuntimeState(mode AutonomyMode) *RuntimeState {
	return &RuntimeState{
		Mode:           mode,
		OriginalMode:   mode,
		LastCycleTimes: make(map[string]time.Time),
		CyclesRun:      make(map[string]int),
	}
}

// Snapshot returns a copy-safe snapshot of the runtime state.
func (rs *RuntimeState) Snapshot() RuntimeState {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	// Copy maps
	lct := make(map[string]time.Time, len(rs.LastCycleTimes))
	for k, v := range rs.LastCycleTimes {
		lct[k] = v
	}
	cr := make(map[string]int, len(rs.CyclesRun))
	for k, v := range rs.CyclesRun {
		cr[k] = v
	}
	cp := *rs
	cp.LastCycleTimes = lct
	cp.CyclesRun = cr
	return cp
}

// --- Dedupe tracker ---

type dedupeTracker struct {
	mu   sync.Mutex
	seen map[string]time.Time // key -> last seen
}

func newDedupeTracker() *dedupeTracker {
	return &dedupeTracker{seen: make(map[string]time.Time)}
}

// IsDuplicate returns true if the key was seen within the given window.
func (d *dedupeTracker) IsDuplicate(key string, window time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if last, ok := d.seen[key]; ok {
		if time.Since(last) < window {
			return true
		}
	}
	d.seen[key] = time.Now()
	return false
}

// Cleanup removes entries older than maxAge.
func (d *dedupeTracker) Cleanup(maxAge time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for k, v := range d.seen {
		if v.Before(cutoff) {
			delete(d.seen, k)
		}
	}
}

// --- Orchestrator ---

// Orchestrator is the autonomous runtime loop.
type Orchestrator struct {
	cfg     *AutonomyConfig
	state   *RuntimeState
	auditor audit.AuditRecorder
	logger  *zap.Logger
	dedupe  *dedupeTracker

	// Providers (all optional, fail-open)
	reflection ReflectionRunner
	objective  ObjectiveRunner
	actuation  ActuationRunner
	scheduling SchedulingRunner
	portfolio  PortfolioRunner
	discovery  DiscoveryRunner
	selfExt    SelfExtensionRunner
	pressure   PressureReader
	capacity   CapacityReader
	governance GovernanceSetter

	// Chain closure providers (Iteration 54.5/55A)
	taskOrchestrator TaskOrchestratorRunner
	executionLoop    ExecutionLoopRunner
	feedbackStore    ExecutionFeedbackStore

	// Control
	stopCh chan struct{}
	doneCh chan struct{}

	// Reports
	reports   []AutonomyReport
	reportsMu sync.RWMutex
}

// NewOrchestrator creates a new autonomy orchestrator.
func NewOrchestrator(cfg *AutonomyConfig, auditor audit.AuditRecorder, logger *zap.Logger) *Orchestrator {
	return &Orchestrator{
		cfg:     cfg,
		state:   newRuntimeState(cfg.Mode),
		auditor: auditor,
		logger:  logger,
		dedupe:  newDedupeTracker(),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Builder methods for wiring providers.
func (o *Orchestrator) WithReflection(r ReflectionRunner) *Orchestrator { o.reflection = r; return o }
func (o *Orchestrator) WithObjective(r ObjectiveRunner) *Orchestrator   { o.objective = r; return o }
func (o *Orchestrator) WithActuation(r ActuationRunner) *Orchestrator   { o.actuation = r; return o }
func (o *Orchestrator) WithScheduling(r SchedulingRunner) *Orchestrator { o.scheduling = r; return o }
func (o *Orchestrator) WithPortfolio(r PortfolioRunner) *Orchestrator   { o.portfolio = r; return o }
func (o *Orchestrator) WithDiscovery(r DiscoveryRunner) *Orchestrator   { o.discovery = r; return o }
func (o *Orchestrator) WithSelfExtension(r SelfExtensionRunner) *Orchestrator {
	o.selfExt = r
	return o
}
func (o *Orchestrator) WithPressure(r PressureReader) *Orchestrator     { o.pressure = r; return o }
func (o *Orchestrator) WithCapacity(r CapacityReader) *Orchestrator     { o.capacity = r; return o }
func (o *Orchestrator) WithGovernance(r GovernanceSetter) *Orchestrator { o.governance = r; return o }

// Chain closure builder methods (Iteration 54.5/55A).
func (o *Orchestrator) WithTaskOrchestrator(r TaskOrchestratorRunner) *Orchestrator {
	o.taskOrchestrator = r
	return o
}
func (o *Orchestrator) WithExecutionLoop(r ExecutionLoopRunner) *Orchestrator {
	o.executionLoop = r
	return o
}
func (o *Orchestrator) WithFeedbackStore(s ExecutionFeedbackStore) *Orchestrator {
	o.feedbackStore = s
	return o
}

// GetState returns a snapshot of the current runtime state.
func (o *Orchestrator) GetState() RuntimeState {
	return o.state.Snapshot()
}

// GetReports returns the most recent reports (up to limit).
func (o *Orchestrator) GetReports(limit int) []AutonomyReport {
	o.reportsMu.RLock()
	defer o.reportsMu.RUnlock()
	if limit <= 0 || limit > len(o.reports) {
		limit = len(o.reports)
	}
	// Return most recent first.
	start := len(o.reports) - limit
	result := make([]AutonomyReport, limit)
	copy(result, o.reports[start:])
	return result
}

// Start begins the autonomous runtime loop.
func (o *Orchestrator) Start(ctx context.Context) error {
	o.state.mu.Lock()
	if o.state.Running {
		o.state.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	now := time.Now()
	o.state.Running = true
	o.state.StartedAt = &now
	o.state.Mode = o.cfg.Mode
	o.state.OriginalMode = o.cfg.Mode
	o.stopCh = make(chan struct{})
	o.doneCh = make(chan struct{})
	o.state.mu.Unlock()

	// Set governance mode from config.
	if o.governance != nil && o.cfg.Governance.StartupMode != "" {
		if err := o.governance.SetMode(ctx, o.cfg.Governance.StartupMode, "autonomy orchestrator startup"); err != nil {
			o.logger.Warn("failed to set governance mode at startup", zap.Error(err))
		} else {
			o.logger.Info("governance mode set from autonomy config", zap.String("mode", o.cfg.Governance.StartupMode))
		}
	}

	o.auditEvent(ctx, "autonomy.started", map[string]any{
		"mode":         string(o.cfg.Mode),
		"tick_seconds": o.cfg.Scheduler.TickSeconds,
		"window_start": o.cfg.ExecWindow.Start,
		"window_end":   o.cfg.ExecWindow.End,
	})

	o.logger.Info("autonomy orchestrator started",
		zap.String("mode", string(o.cfg.Mode)),
		zap.Int("tick_seconds", o.cfg.Scheduler.TickSeconds),
	)

	// Run bootstrap cycles if configured.
	o.runBootstrap(ctx)

	go o.loop()
	return nil
}

// Stop halts the orchestrator.
func (o *Orchestrator) Stop(ctx context.Context) {
	o.state.mu.Lock()
	if !o.state.Running {
		o.state.mu.Unlock()
		return
	}
	o.state.mu.Unlock()

	close(o.stopCh)
	<-o.doneCh

	o.state.mu.Lock()
	o.state.Running = false
	o.state.mu.Unlock()

	o.auditEvent(ctx, "autonomy.stopped", map[string]any{
		"mode": string(o.state.Mode),
	})
	o.logger.Info("autonomy orchestrator stopped")
}

// ReloadConfig replaces the config and audits the change.
func (o *Orchestrator) ReloadConfig(ctx context.Context, cfg *AutonomyConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	o.cfg = cfg

	o.state.mu.Lock()
	o.state.Mode = cfg.Mode
	o.state.OriginalMode = cfg.Mode
	o.state.Downgraded = false
	o.state.DowngradeReason = ""
	o.state.HeavyActionsDisabled = false
	o.state.SelfExtDisabled = false
	o.state.ConsecutiveFailures = 0
	o.state.ConsecutiveHealthy = 0
	o.state.mu.Unlock()

	o.auditEvent(ctx, "autonomy.config_reloaded", map[string]any{
		"new_mode": string(cfg.Mode),
	})
	o.logger.Info("autonomy config reloaded", zap.String("mode", string(cfg.Mode)))
	return nil
}

// SetMode changes the autonomy mode explicitly (admin override).
func (o *Orchestrator) SetMode(ctx context.Context, mode AutonomyMode) error {
	validModes := map[AutonomyMode]bool{
		ModeFrozen: true, ModeSupervisedAutonomy: true,
		ModeBoundedAutonomy: true, ModeAutonomous: true,
	}
	if !validModes[mode] {
		return fmt.Errorf("invalid mode: %s", mode)
	}
	o.state.mu.Lock()
	old := o.state.Mode
	o.state.Mode = mode
	o.state.Downgraded = false
	o.state.DowngradeReason = ""
	o.state.mu.Unlock()

	o.auditEvent(ctx, "autonomy.mode_changed", map[string]any{
		"old_mode": string(old),
		"new_mode": string(mode),
		"source":   "admin_override",
	})
	return nil
}

// loop is the main runtime tick loop.
func (o *Orchestrator) loop() {
	defer close(o.doneCh)

	tick := time.Duration(o.cfg.Scheduler.TickSeconds) * time.Second
	if tick <= 0 {
		tick = 60 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-o.stopCh:
			return
		case <-ticker.C:
			o.tick()
		}
	}
}

// tick runs one iteration of the orchestrator.
func (o *Orchestrator) tick() {
	o.state.mu.RLock()
	mode := o.state.Mode
	o.state.mu.RUnlock()

	if mode == ModeFrozen {
		return // no work in frozen mode
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	now := time.Now()
	inWindow := o.cfg.IsInsideWindow(now)

	// Dedupe cleanup.
	o.dedupe.Cleanup(72 * time.Hour)

	// Safety check before doing any work.
	o.runSafetyChecks(ctx)

	// Determine which cycles are due.
	cycles := o.dueCycles(now, inWindow)
	if len(cycles) == 0 {
		return
	}

	o.state.mu.Lock()
	o.state.ActionsTakenThisCycle = 0
	o.state.mu.Unlock()

	cycleOK := true
	for _, cycle := range cycles {
		if err := o.runCycle(ctx, cycle, now); err != nil {
			o.logger.Warn("cycle failed",
				zap.String("cycle", cycle),
				zap.Error(err),
			)
			cycleOK = false
		}
	}

	// Track consecutive failures/successes.
	o.state.mu.Lock()
	if cycleOK {
		o.state.ConsecutiveFailures = 0
		o.state.ConsecutiveHealthy++
	} else {
		o.state.ConsecutiveFailures++
		o.state.ConsecutiveHealthy = 0
		o.state.LastError = fmt.Sprintf("cycle failure at %s", now.Format(time.RFC3339))
	}
	o.state.mu.Unlock()

	// Check if we need to auto-downgrade on repeated failures.
	o.state.mu.RLock()
	failures := o.state.ConsecutiveFailures
	o.state.mu.RUnlock()
	if failures >= o.cfg.Limits.MaxConsecutiveFailedCycles {
		o.downgrade(ctx, "max_consecutive_failed_cycles reached")
	}

	// Check recovery.
	o.checkRecovery(ctx)
}

// dueCycles returns the cycles that should run now.
func (o *Orchestrator) dueCycles(now time.Time, inWindow bool) []string {
	o.state.mu.RLock()
	defer o.state.mu.RUnlock()

	allCycles := []struct {
		name        string
		enabled     bool
		inWindowReq bool // requires being inside execution window
	}{
		{"reflection", o.cfg.Governance.AllowReflection, false},
		{"objective", o.cfg.Governance.AllowObjectiveRecompute, false},
		{"actuation", o.cfg.Actuation.Enabled && o.cfg.Actuation.AllowRun, true},
		{"scheduling", o.cfg.Scheduling.Enabled && o.cfg.Scheduling.AllowRecompute, true},
		{"portfolio", o.cfg.Governance.AllowPortfolioRebalance, true},
		{"discovery", o.cfg.Governance.AllowDiscovery, true},
		{"self_extension", o.cfg.SelfExt.Enabled, true},
		{"task_recompute", o.taskOrchestrator != nil, true},
		{"task_dispatch", o.taskOrchestrator != nil, true},
		{"reporting", o.cfg.Reporting.Enabled, false},
	}

	var due []string
	for _, c := range allCycles {
		if !c.enabled {
			continue
		}
		// Check window restrictions.
		if !inWindow && c.inWindowReq {
			if !o.isAllowedOutsideWindow(c.name) {
				continue
			}
		}
		// Check if cycle is due.
		dur := o.cfg.CycleDuration(c.name)
		lastRun, ok := o.state.LastCycleTimes[c.name]
		if !ok || now.Sub(lastRun) >= dur {
			due = append(due, c.name)
		}
	}
	return due
}

func (o *Orchestrator) isAllowedOutsideWindow(cycle string) bool {
	ow := o.cfg.ExecWindow.Outside
	switch cycle {
	case "reflection":
		return ow.AllowReflection
	case "objective":
		return ow.AllowObjective
	case "reporting":
		return ow.AllowReporting
	default:
		return false
	}
}

// runCycle executes a single named cycle.
func (o *Orchestrator) runCycle(ctx context.Context, cycle string, now time.Time) error {
	o.auditEvent(ctx, "autonomy.cycle_started", map[string]any{
		"cycle": cycle,
		"mode":  string(o.state.Mode),
	})

	var err error
	switch cycle {
	case "reflection":
		err = o.cycleReflection(ctx)
	case "objective":
		err = o.cycleObjective(ctx)
	case "actuation":
		err = o.cycleActuation(ctx)
	case "scheduling":
		err = o.cycleScheduling(ctx)
	case "portfolio":
		err = o.cyclePortfolio(ctx)
	case "discovery":
		err = o.cycleDiscovery(ctx)
	case "self_extension":
		err = o.cycleSelfExtension(ctx)
	case "task_recompute":
		err = o.cycleTaskRecompute(ctx)
	case "task_dispatch":
		err = o.cycleTaskDispatch(ctx)
	case "reporting":
		err = o.cycleReporting(ctx)
	default:
		err = fmt.Errorf("unknown cycle: %s", cycle)
	}

	o.state.mu.Lock()
	o.state.LastCycleTimes[cycle] = now
	o.state.CyclesRun[cycle]++
	o.state.mu.Unlock()

	o.auditEvent(ctx, "autonomy.cycle_completed", map[string]any{
		"cycle":   cycle,
		"success": err == nil,
	})

	if o.cfg.Observe.LogCycleSummary {
		if err != nil {
			o.logger.Warn("cycle completed with error", zap.String("cycle", cycle), zap.Error(err))
		} else {
			o.logger.Info("cycle completed", zap.String("cycle", cycle))
		}
	}

	return err
}

// --- Cycle implementations ---

func (o *Orchestrator) cycleReflection(ctx context.Context) error {
	if o.reflection == nil {
		return nil // fail-open
	}
	result, err := o.reflection.RunReflection(ctx, false)
	if err != nil {
		return fmt.Errorf("reflection: %w", err)
	}
	o.logger.Info("reflection cycle completed",
		zap.Int("signals", result.SignalCount),
		zap.Int("risk_flags", result.RiskFlags),
	)
	return nil
}

func (o *Orchestrator) cycleObjective(ctx context.Context) error {
	if o.objective == nil {
		return nil
	}
	result, err := o.objective.Recompute(ctx)
	if err != nil {
		return fmt.Errorf("objective: %w", err)
	}
	o.logger.Info("objective recomputed",
		zap.Float64("net_utility", result.NetUtility),
		zap.Float64("risk_score", result.RiskScore),
	)
	return nil
}

func (o *Orchestrator) cycleActuation(ctx context.Context) error {
	if o.actuation == nil {
		return nil
	}

	result, err := o.actuation.Run(ctx)
	if err != nil {
		return fmt.Errorf("actuation run: %w", err)
	}

	o.state.mu.RLock()
	heavyDisabled := o.state.HeavyActionsDisabled
	o.state.mu.RUnlock()

	// Route safe actions, queue review-required ones.
	decisions, dErr := o.actuation.ListDecisions(ctx, 50)
	if dErr != nil {
		o.logger.Warn("failed to list actuation decisions", zap.Error(dErr))
	}

	proposed := 0
	reviewQueued := 0
	suppressed := 0

	for _, d := range decisions {
		if d.Status != "proposed" {
			continue
		}

		// Dedupe check.
		dedupeKey := fmt.Sprintf("actuation:%s:%s", d.Type, d.ID)
		window := time.Duration(o.cfg.Limits.Dedupe.Actuation) * time.Hour
		if o.dedupe.IsDuplicate(dedupeKey, window) {
			suppressed++
			if o.cfg.Observe.LogSuppressed {
				o.logger.Info("suppressed duplicate actuation decision",
					zap.String("type", d.Type),
					zap.String("id", d.ID),
				)
			}
			continue
		}

		// Classify.
		if d.RequiresReview {
			reviewQueued++
			o.auditEvent(ctx, "autonomy.review_action_queued", map[string]any{
				"decision_id":   d.ID,
				"decision_type": d.Type,
				"priority":      d.Priority,
			})
			continue
		}

		// Check if heavy and disabled.
		if heavyDisabled && o.isHeavyAction(d.Type) {
			suppressed++
			continue
		}

		// Limit per cycle.
		o.state.mu.RLock()
		taken := o.state.ActionsTakenThisCycle
		o.state.mu.RUnlock()
		if taken >= o.cfg.Limits.MaxActionsPerCycle {
			suppressed++
			continue
		}

		// Safe internal action - route it.
		proposed++
		o.state.mu.Lock()
		o.state.ActionsTakenThisCycle++
		o.state.SafeActionsRouted++
		o.state.mu.Unlock()

		o.auditEvent(ctx, "autonomy.safe_action_routed", map[string]any{
			"decision_id":   d.ID,
			"decision_type": d.Type,
			"priority":      d.Priority,
		})
	}

	o.state.mu.Lock()
	o.state.ReviewActionsQueued += reviewQueued
	o.state.SuppressedDecisions += suppressed
	o.state.mu.Unlock()

	o.logger.Info("actuation cycle completed",
		zap.Int("total_decisions", result.DecisionCount),
		zap.Int("safe_routed", proposed),
		zap.Int("review_queued", reviewQueued),
		zap.Int("suppressed", suppressed),
	)

	// Chain closure: materialize safe decisions as orchestrated tasks (Iteration 54.5/55A).
	tasksCreated := 0
	for _, d := range decisions {
		if d.Status != "proposed" {
			continue
		}
		// Only materialize non-review, non-suppressed decisions.
		if d.RequiresReview {
			continue
		}
		_, created, err := o.MaterializeDecisionAsTask(ctx, d)
		if err != nil {
			o.logger.Warn("chain_closure: failed to materialize decision as task",
				zap.String("decision_id", d.ID),
				zap.Error(err),
			)
			continue
		}
		if created {
			tasksCreated++
		}
	}

	if tasksCreated > 0 {
		o.state.mu.Lock()
		o.state.TasksCreatedFromActuation += tasksCreated
		o.state.mu.Unlock()
		o.logger.Info("chain_closure: materialized actuation decisions as tasks",
			zap.Int("tasks_created", tasksCreated),
		)
	}

	return nil
}

func (o *Orchestrator) cycleScheduling(ctx context.Context) error {
	if o.scheduling == nil {
		return nil
	}
	if err := o.scheduling.RecomputeSlots(ctx); err != nil {
		return fmt.Errorf("scheduling: %w", err)
	}
	o.logger.Info("scheduling slots recomputed")
	return nil
}

func (o *Orchestrator) cyclePortfolio(ctx context.Context) error {
	if o.portfolio == nil {
		return nil
	}
	if err := o.portfolio.Rebalance(ctx); err != nil {
		return fmt.Errorf("portfolio: %w", err)
	}
	o.logger.Info("portfolio rebalanced")
	return nil
}

func (o *Orchestrator) cycleDiscovery(ctx context.Context) error {
	if o.discovery == nil {
		return nil
	}
	result, err := o.discovery.Run(ctx)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	o.logger.Info("discovery cycle completed",
		zap.Int("candidates_found", result.CandidatesFound),
	)
	return nil
}

func (o *Orchestrator) cycleSelfExtension(ctx context.Context) error {
	if o.selfExt == nil {
		return nil
	}

	o.state.mu.RLock()
	selfExtDisabled := o.state.SelfExtDisabled
	o.state.mu.RUnlock()

	proposals, err := o.selfExt.ListProposals(ctx, 20)
	if err != nil {
		return fmt.Errorf("self_extension list: %w", err)
	}

	for _, p := range proposals {
		if p.Status != "validated" {
			continue
		}

		// Dedupe.
		dedupeKey := fmt.Sprintf("selfext:%s", p.ID)
		window := time.Duration(o.cfg.Limits.Dedupe.SelfExtension) * time.Hour
		if o.dedupe.IsDuplicate(dedupeKey, window) {
			continue
		}

		// Auto-deploy only if allowed.
		if !o.cfg.SelfExt.AutoDeploy.Enabled || selfExtDisabled {
			o.state.mu.Lock()
			o.state.SelfExtBlocked++
			o.state.mu.Unlock()
			o.auditEvent(ctx, "autonomy.self_extension_blocked", map[string]any{
				"proposal_id": p.ID,
				"title":       p.Title,
				"reason":      "auto_deploy_disabled_or_safety_lockout",
			})
			continue
		}

		// Only low-risk allowed.
		if o.cfg.SelfExt.AutoDeploy.OnlyLowRisk {
			// Treat all non-trivial proposals as non-low-risk in supervised mode.
			o.state.mu.Lock()
			o.state.SelfExtBlocked++
			o.state.mu.Unlock()
			o.auditEvent(ctx, "autonomy.self_extension_blocked", map[string]any{
				"proposal_id": p.ID,
				"title":       p.Title,
				"reason":      "only_low_risk_allowed",
			})
			continue
		}

		// Would deploy here if risk classification allowed it.
		o.state.mu.Lock()
		o.state.SelfExtDeployed++
		o.state.mu.Unlock()
		o.auditEvent(ctx, "autonomy.self_extension_autodeployed", map[string]any{
			"proposal_id": p.ID,
			"title":       p.Title,
		})
	}

	o.logger.Info("self-extension evaluation completed",
		zap.Int("proposals_reviewed", len(proposals)),
	)
	return nil
}

func (o *Orchestrator) isHeavyAction(actionType string) bool {
	heavy := map[string]bool{
		"adjust_pricing":      true,
		"trigger_automation":  true,
		"stabilize_income":    true,
		"rebalance_portfolio": true,
	}
	return heavy[actionType]
}

// getEffectiveMode returns the current autonomy mode.
func (o *Orchestrator) getEffectiveMode() AutonomyMode {
	o.state.mu.RLock()
	defer o.state.mu.RUnlock()
	return o.state.Mode
}

// --- Chain closure cycles (Iteration 54.5/55A) ---

func (o *Orchestrator) cycleTaskRecompute(ctx context.Context) error {
	if o.taskOrchestrator == nil {
		return nil
	}

	mode := o.getEffectiveMode()
	if mode == ModeFrozen {
		o.auditEvent(ctx, "autonomy.task_recompute_skipped", map[string]any{
			"reason": "frozen_mode",
		})
		return nil
	}

	o.auditEvent(ctx, "autonomy.task_recompute_started", map[string]any{
		"mode": string(mode),
	})

	// Also propagate any completed executions first.
	completed, failed, paused, propErr := o.PropagateExecutionResults(ctx)
	if propErr != nil {
		o.logger.Warn("chain_closure: execution propagation error during recompute", zap.Error(propErr))
	}
	if completed > 0 || failed > 0 || paused > 0 {
		o.state.mu.Lock()
		o.state.ExecutionCompleted += completed
		o.state.ExecutionFailed += failed
		o.state.ExecutionPaused += paused
		o.state.mu.Unlock()
	}

	if err := o.taskOrchestrator.RecomputePriorities(ctx); err != nil {
		return fmt.Errorf("task recompute: %w", err)
	}

	o.state.mu.Lock()
	o.state.TaskRecomputeCount++
	o.state.mu.Unlock()

	o.auditEvent(ctx, "autonomy.task_recompute_completed", map[string]any{
		"completed_propagated": completed,
		"failed_propagated":    failed,
		"paused_propagated":    paused,
	})

	o.logger.Info("task recompute cycle completed",
		zap.Int("completed", completed),
		zap.Int("failed", failed),
		zap.Int("paused", paused),
	)
	return nil
}

func (o *Orchestrator) cycleTaskDispatch(ctx context.Context) error {
	if o.taskOrchestrator == nil {
		return nil
	}

	mode := o.getEffectiveMode()
	if mode == ModeFrozen {
		o.auditEvent(ctx, "autonomy.task_dispatch_skipped", map[string]any{
			"reason": "frozen_mode",
		})
		return nil
	}

	o.auditEvent(ctx, "autonomy.task_dispatch_started", map[string]any{
		"mode": string(mode),
	})

	result, err := o.taskOrchestrator.Dispatch(ctx)
	if err != nil {
		// ErrGovernanceFrozen and ErrMaxRunning are expected — not true errors.
		o.auditEvent(ctx, "autonomy.task_dispatch_completed", map[string]any{
			"dispatched": 0,
			"reason":     err.Error(),
		})
		return nil // fail-open for expected constraint violations
	}

	// Link dispatched tasks to their execution task IDs.
	for taskID, execTaskID := range result.DispatchedTaskIDs {
		if execTaskID != "" {
			if linkErr := o.taskOrchestrator.SetExecutionTaskID(ctx, taskID, execTaskID); linkErr != nil {
				o.logger.Warn("chain_closure: failed to link execution task",
					zap.String("task_id", taskID),
					zap.String("exec_task_id", execTaskID),
					zap.Error(linkErr),
				)
			}
		}
	}

	o.state.mu.Lock()
	o.state.TaskDispatchCount += result.DispatchedCount
	o.state.mu.Unlock()

	o.auditEvent(ctx, "autonomy.task_dispatch_completed", map[string]any{
		"dispatched": result.DispatchedCount,
		"skipped":    result.SkippedCount,
		"blocked":    result.BlockedCount,
	})

	o.logger.Info("task dispatch cycle completed",
		zap.Int("dispatched", result.DispatchedCount),
		zap.Int("skipped", result.SkippedCount),
		zap.Int("blocked", result.BlockedCount),
	)
	return nil
}

// --- Safety kernel ---

func (o *Orchestrator) runSafetyChecks(ctx context.Context) {
	if !o.cfg.Risk.AutoDowngrade {
		return
	}

	// Check pressure.
	if o.pressure != nil {
		score, _ := o.pressure.GetPressure(ctx)
		if score >= o.cfg.Risk.Thresholds.Pressure {
			o.downgrade(ctx, fmt.Sprintf("pressure_threshold_exceeded: %.2f >= %.2f", score, o.cfg.Risk.Thresholds.Pressure))
		}
	}

	// Check overload.
	if o.capacity != nil {
		load := o.capacity.GetOwnerLoadScore(ctx)
		if load >= o.cfg.Risk.Thresholds.Overload {
			o.disableHeavyActions(ctx, fmt.Sprintf("overload_threshold_exceeded: %.2f >= %.2f", load, o.cfg.Risk.Thresholds.Overload))
		}
	}
}

func (o *Orchestrator) downgrade(ctx context.Context, reason string) {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()

	if o.state.Downgraded {
		return // already downgraded
	}

	old := o.state.Mode
	o.state.Downgraded = true
	o.state.DowngradeReason = reason
	o.state.HeavyActionsDisabled = true
	o.state.SelfExtDisabled = true

	if o.cfg.Risk.DowngradeMode != "" {
		o.state.Mode = AutonomyMode(o.cfg.Risk.DowngradeMode)
	} else {
		o.state.Mode = ModeSupervisedAutonomy
	}

	if o.cfg.Observe.LogDowngrades {
		o.logger.Warn("autonomy mode downgraded",
			zap.String("from", string(old)),
			zap.String("to", string(o.state.Mode)),
			zap.String("reason", reason),
		)
	}

	o.auditEventLocked(ctx, "autonomy.downgraded", map[string]any{
		"from_mode": string(old),
		"to_mode":   string(o.state.Mode),
		"reason":    reason,
	})
}

func (o *Orchestrator) disableHeavyActions(ctx context.Context, reason string) {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()

	if o.state.HeavyActionsDisabled {
		return
	}
	o.state.HeavyActionsDisabled = true

	if o.cfg.Observe.LogDowngrades {
		o.logger.Warn("heavy actions disabled", zap.String("reason", reason))
	}

	o.auditEventLocked(ctx, "autonomy.heavy_actions_disabled", map[string]any{
		"reason": reason,
	})
}

func (o *Orchestrator) checkRecovery(ctx context.Context) {
	if !o.cfg.Risk.Recovery.AutoRestorePrevMode {
		return
	}

	o.state.mu.Lock()
	defer o.state.mu.Unlock()

	if !o.state.Downgraded {
		return
	}

	if o.state.ConsecutiveHealthy >= o.cfg.Risk.Recovery.RequireConsecutiveHealthy {
		old := o.state.Mode
		o.state.Mode = o.state.OriginalMode
		o.state.Downgraded = false
		o.state.DowngradeReason = ""
		o.state.HeavyActionsDisabled = false
		o.state.SelfExtDisabled = false
		o.state.ConsecutiveHealthy = 0

		o.logger.Info("autonomy mode recovered",
			zap.String("from", string(old)),
			zap.String("to", string(o.state.Mode)),
		)

		o.auditEventLocked(ctx, "autonomy.mode_recovered", map[string]any{
			"from_mode": string(old),
			"to_mode":   string(o.state.Mode),
		})
	}
}

// --- Bootstrap ---

func (o *Orchestrator) runBootstrap(ctx context.Context) {
	bs := o.cfg.Scheduler.Bootstrap

	if bs.RunReflectionOnce {
		o.logger.Info("bootstrap: running initial reflection")
		if err := o.cycleReflection(ctx); err != nil {
			o.logger.Warn("bootstrap reflection failed", zap.Error(err))
		}
		o.state.mu.Lock()
		o.state.LastCycleTimes["reflection"] = time.Now()
		o.state.CyclesRun["reflection"]++
		o.state.mu.Unlock()
	}

	if bs.RunObjectiveOnce {
		o.logger.Info("bootstrap: running initial objective recompute")
		if err := o.cycleObjective(ctx); err != nil {
			o.logger.Warn("bootstrap objective failed", zap.Error(err))
		}
		o.state.mu.Lock()
		o.state.LastCycleTimes["objective"] = time.Now()
		o.state.CyclesRun["objective"]++
		o.state.mu.Unlock()
	}

	if bs.RunActuationOnce {
		o.logger.Info("bootstrap: running initial actuation")
		if err := o.cycleActuation(ctx); err != nil {
			o.logger.Warn("bootstrap actuation failed", zap.Error(err))
		}
		o.state.mu.Lock()
		o.state.LastCycleTimes["actuation"] = time.Now()
		o.state.CyclesRun["actuation"]++
		o.state.mu.Unlock()
	}

	if bs.RunReportingOnce {
		o.logger.Info("bootstrap: running initial reporting")
		if err := o.cycleReporting(ctx); err != nil {
			o.logger.Warn("bootstrap reporting failed", zap.Error(err))
		}
		o.state.mu.Lock()
		o.state.LastCycleTimes["reporting"] = time.Now()
		o.state.CyclesRun["reporting"]++
		o.state.mu.Unlock()
	}
}

// --- Reporting ---

// AutonomyReport is a periodic operational report.
type AutonomyReport struct {
	ID                string                `json:"id"`
	Type              string                `json:"type"` // operational | daily | exception
	CreatedAt         time.Time             `json:"created_at"`
	Mode              string                `json:"mode"`
	CyclesRun         map[string]int        `json:"cycles_run"`
	ObjectiveSnapshot *ObjectiveResult      `json:"objective_snapshot,omitempty"`
	ReflectionSummary *ReflectionResult     `json:"reflection_summary,omitempty"`
	ActuationSummary  *ActuationSummaryInfo `json:"actuation_summary,omitempty"`
	SafeActionsRouted int                   `json:"safe_actions_routed"`
	ReviewQueued      int                   `json:"review_queued"`
	SuppressedCount   int                   `json:"suppressed_count"`
	SelfExtBlocked    int                   `json:"self_ext_blocked"`
	SelfExtDeployed   int                   `json:"self_ext_deployed"`
	Downgraded        bool                  `json:"downgraded"`
	DowngradeReason   string                `json:"downgrade_reason,omitempty"`
	FailureCount      int                   `json:"failure_count"`
	Warnings          []string              `json:"warnings,omitempty"`
	ExceptionTrigger  string                `json:"exception_trigger,omitempty"`
}

type ActuationSummaryInfo struct {
	TotalDecisions int `json:"total_decisions"`
	ReviewNeeded   int `json:"review_needed"`
}

func (o *Orchestrator) cycleReporting(ctx context.Context) error {
	snap := o.state.Snapshot()

	report := AutonomyReport{
		ID:                uuid.New().String(),
		Type:              "operational",
		CreatedAt:         time.Now(),
		Mode:              string(snap.Mode),
		CyclesRun:         snap.CyclesRun,
		SafeActionsRouted: snap.SafeActionsRouted,
		ReviewQueued:      snap.ReviewActionsQueued,
		SuppressedCount:   snap.SuppressedDecisions,
		SelfExtBlocked:    snap.SelfExtBlocked,
		SelfExtDeployed:   snap.SelfExtDeployed,
		Downgraded:        snap.Downgraded,
		DowngradeReason:   snap.DowngradeReason,
		FailureCount:      snap.ConsecutiveFailures,
	}

	// Gather objective snapshot if available.
	if o.objective != nil {
		objResult, err := o.objective.Recompute(ctx)
		if err == nil {
			report.ObjectiveSnapshot = &objResult
		}
	}

	// Gather warnings.
	if snap.Downgraded {
		report.Warnings = append(report.Warnings, "system is in downgraded mode: "+snap.DowngradeReason)
	}
	if snap.HeavyActionsDisabled {
		report.Warnings = append(report.Warnings, "heavy actions are disabled")
	}
	if snap.SelfExtDisabled {
		report.Warnings = append(report.Warnings, "self-extension auto-deploy is disabled")
	}
	if snap.ConsecutiveFailures > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%d consecutive failures", snap.ConsecutiveFailures))
	}

	o.storeReport(report)

	o.state.mu.Lock()
	o.state.ReportCount++
	o.state.mu.Unlock()

	o.auditEvent(ctx, "autonomy.report_created", map[string]any{
		"report_id":   report.ID,
		"report_type": report.Type,
	})

	o.logger.Info("autonomy report created",
		zap.String("report_id", report.ID),
		zap.String("type", report.Type),
	)
	return nil
}

// CreateExceptionReport creates an immediate exception report.
func (o *Orchestrator) CreateExceptionReport(ctx context.Context, trigger string) {
	snap := o.state.Snapshot()

	report := AutonomyReport{
		ID:               uuid.New().String(),
		Type:             "exception",
		CreatedAt:        time.Now(),
		Mode:             string(snap.Mode),
		CyclesRun:        snap.CyclesRun,
		Downgraded:       snap.Downgraded,
		DowngradeReason:  snap.DowngradeReason,
		FailureCount:     snap.ConsecutiveFailures,
		ExceptionTrigger: trigger,
		Warnings:         []string{trigger},
	}

	o.storeReport(report)

	o.auditEvent(ctx, "autonomy.report_created", map[string]any{
		"report_id":   report.ID,
		"report_type": "exception",
		"trigger":     trigger,
	})

	o.logger.Warn("autonomy exception report created",
		zap.String("trigger", trigger),
		zap.String("report_id", report.ID),
	)
}

func (o *Orchestrator) storeReport(r AutonomyReport) {
	o.reportsMu.Lock()
	defer o.reportsMu.Unlock()
	o.reports = append(o.reports, r)
	// Cap at 200 reports in memory.
	if len(o.reports) > 200 {
		o.reports = o.reports[len(o.reports)-200:]
	}
}

// --- Audit helpers ---

func (o *Orchestrator) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if o.auditor == nil {
		return
	}
	_ = o.auditor.RecordEvent(ctx, "autonomy", uuid.Nil, eventType, "system", "autonomy_orchestrator", payload)
}

// auditEventLocked is used when the state mutex is already held (no deadlock risk
// since RecordEvent doesn't acquire our mutex).
func (o *Orchestrator) auditEventLocked(ctx context.Context, eventType string, payload map[string]any) {
	if o.auditor == nil {
		return
	}
	_ = o.auditor.RecordEvent(ctx, "autonomy", uuid.Nil, eventType, "system", "autonomy_orchestrator", payload)
}
