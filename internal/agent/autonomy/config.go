package autonomy

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AutonomyMode defines the autonomy level of the system.
type AutonomyMode string

const (
	ModeFrozen             AutonomyMode = "frozen"
	ModeSupervisedAutonomy AutonomyMode = "supervised_autonomy"
	ModeBoundedAutonomy    AutonomyMode = "bounded_autonomy"
	ModeAutonomous         AutonomyMode = "autonomous"
)

// AutonomyConfig is the top-level config loaded from autonomy.yaml.
type AutonomyConfig struct {
	Mode       AutonomyMode     `yaml:"mode"`
	Governance GovernanceCfg    `yaml:"governance"`
	Scheduler  SchedulerCfg     `yaml:"scheduler"`
	ExecWindow ExecutionWindow  `yaml:"execution_window"`
	Limits     LimitsCfg        `yaml:"limits"`
	Risk       RiskCfg          `yaml:"risk"`
	Learning   LearningCfg      `yaml:"learning"`
	Objective  ObjectiveCfg     `yaml:"objective"`
	Portfolio  PortfolioCfg     `yaml:"portfolio"`
	Pricing    PricingCfg       `yaml:"pricing"`
	Scheduling SchedulingCfg    `yaml:"scheduling"`
	ExtActions ExtActionsCfg    `yaml:"external_actions"`
	SelfExt    SelfExtCfg       `yaml:"self_extension"`
	Actuation  ActuationCfg     `yaml:"actuation"`
	ActPrio    ActPriorityCfg   `yaml:"actuation_priority"`
	Reporting  ReportingCfg     `yaml:"reporting"`
	Observe    ObservabilityCfg `yaml:"observability"`
	Runtime    RuntimeCfg       `yaml:"runtime"`
	Launch     LaunchCfg        `yaml:"launch"`
}

type GovernanceCfg struct {
	StartupMode                   string   `yaml:"startup_mode"`
	RequireHumanReview            bool     `yaml:"require_human_review"`
	AllowLearning                 bool     `yaml:"allow_learning"`
	AllowReflection               bool     `yaml:"allow_reflection"`
	AllowObjectiveRecompute       bool     `yaml:"allow_objective_recompute"`
	AllowActuation                bool     `yaml:"allow_actuation"`
	AllowPortfolioRebalance       bool     `yaml:"allow_portfolio_rebalance"`
	AllowSchedulingRecompute      bool     `yaml:"allow_scheduling_recompute"`
	AllowDiscovery                bool     `yaml:"allow_discovery"`
	AllowPricingCompute           bool     `yaml:"allow_pricing_compute"`
	AllowSelfExtension            bool     `yaml:"allow_self_extension"`
	AllowSelfExtAutoDeployLowRisk bool     `yaml:"allow_self_extension_autodeploy_low_risk"`
	AllowExternalExecution        bool     `yaml:"allow_external_execution"`
	AllowSafeExternalExecute      bool     `yaml:"allow_safe_external_execute"`
	AllowDraftExternalExecute     bool     `yaml:"allow_draft_external_execute"`
	AllowRealSendWithoutReview    bool     `yaml:"allow_real_send_without_review"`
	GuardedActions                []string `yaml:"guarded_actions"`
	BlockedActions                []string `yaml:"blocked_actions"`
}

type SchedulerCfg struct {
	Enabled     bool         `yaml:"enabled"`
	TickSeconds int          `yaml:"tick_seconds"`
	Cycles      CyclesCfg    `yaml:"cycles"`
	Bootstrap   BootstrapCfg `yaml:"startup_bootstrap"`
}

type CyclesCfg struct {
	ReflectionHours    int `yaml:"reflection_hours"`
	ObjectiveHours     int `yaml:"objective_hours"`
	ActuationHours     int `yaml:"actuation_hours"`
	ActuationPrioHours int `yaml:"actuation_priority_hours"`
	SchedulingHours    int `yaml:"scheduling_hours"`
	PortfolioHours     int `yaml:"portfolio_hours"`
	DiscoveryHours     int `yaml:"discovery_hours"`
	PricingPerfHours   int `yaml:"pricing_performance_hours"`
	SelfExtHours       int `yaml:"self_extension_hours"`
	TaskRecomputeHours int `yaml:"task_recompute_hours"`
	TaskDispatchHours  int `yaml:"task_dispatch_hours"`
	GoalPlanningHours  int `yaml:"goal_planning_hours"`
	ReportingHours     int `yaml:"reporting_hours"`
}

type BootstrapCfg struct {
	RunReflectionOnce bool `yaml:"run_reflection_once"`
	RunObjectiveOnce  bool `yaml:"run_objective_once"`
	RunActuationOnce  bool `yaml:"run_actuation_once"`
	RunReportingOnce  bool `yaml:"run_reporting_once"`
}

type ExecutionWindow struct {
	Enabled  bool          `yaml:"enabled"`
	Start    string        `yaml:"start"`
	End      string        `yaml:"end"`
	Timezone string        `yaml:"timezone"`
	Outside  OutsideWindow `yaml:"outside_window"`
}

type OutsideWindow struct {
	AllowObserve        bool `yaml:"allow_observe"`
	AllowReflection     bool `yaml:"allow_reflection"`
	AllowObjective      bool `yaml:"allow_objective"`
	AllowReporting      bool `yaml:"allow_reporting"`
	AllowSafeExecution  bool `yaml:"allow_safe_execution"`
	AllowHeavyExecution bool `yaml:"allow_heavy_execution"`
	AllowSelfExtension  bool `yaml:"allow_self_extension"`
}

type LimitsCfg struct {
	MaxActionsPerCycle          int         `yaml:"max_actions_per_cycle"`
	MaxHeavyActionsPerCycle     int         `yaml:"max_heavy_actions_per_cycle"`
	MaxReviewRequiredPerCycle   int         `yaml:"max_review_required_per_cycle"`
	MaxSelfExtPerDay            int         `yaml:"max_self_extensions_per_day"`
	MaxExtActionsPerDay         int         `yaml:"max_external_actions_per_day"`
	MaxReportsPerDay            int         `yaml:"max_reports_per_day"`
	MaxConsecutiveFailedCycles  int         `yaml:"max_consecutive_failed_cycles"`
	MaxDuplicateDecisionRepeats int         `yaml:"max_duplicate_decision_repeats"`
	Dedupe                      DedupeCfg   `yaml:"dedupe_hours"`
	QueueLimits                 QueueLimits `yaml:"queue_limits"`
}

type DedupeCfg struct {
	Actuation       int `yaml:"actuation"`
	SelfExtension   int `yaml:"self_extension"`
	ExternalDraft   int `yaml:"external_draft"`
	ReportException int `yaml:"report_exception"`
}

type QueueLimits struct {
	MaxReviewQueue        int `yaml:"max_review_queue"`
	MaxPendingSafeActions int `yaml:"max_pending_safe_actions"`
	MaxPendingSelfExt     int `yaml:"max_pending_self_extensions"`
}

type RiskCfg struct {
	AutoDowngrade bool         `yaml:"auto_downgrade"`
	DowngradeMode string       `yaml:"downgrade_mode"`
	Thresholds    RiskThresh   `yaml:"thresholds"`
	OnBreach      BreachPolicy `yaml:"on_threshold_breach"`
	Recovery      RecoveryCfg  `yaml:"recovery"`
}

type RiskThresh struct {
	FailureRate        float64 `yaml:"failure_rate_threshold"`
	Overload           float64 `yaml:"overload_threshold"`
	Pressure           float64 `yaml:"pressure_threshold"`
	ReviewQueue        int     `yaml:"review_queue_threshold"`
	ExtFailureSpike    int     `yaml:"external_failure_spike_threshold"`
	ObjNetUtilityFloor float64 `yaml:"objective_net_utility_floor"`
}

type BreachPolicy struct {
	DisableHeavyActions        bool `yaml:"disable_heavy_actions"`
	DisableSelfExtAutodeploy   bool `yaml:"disable_self_extension_autodeploy"`
	RestrictExtToSafeDraftOnly bool `yaml:"restrict_external_execution_to_draft_only"`
	ForceStableStrategyBias    bool `yaml:"force_stable_strategy_bias"`
	IncreaseReportingFrequency bool `yaml:"increase_reporting_frequency"`
}

type RecoveryCfg struct {
	RequireConsecutiveHealthy int  `yaml:"require_consecutive_healthy_cycles"`
	AutoRestorePrevMode       bool `yaml:"auto_restore_previous_mode"`
}

type LearningCfg struct {
	EnableFeedback           bool `yaml:"enable_feedback"`
	EnableOutcomeAttribution bool `yaml:"enable_outcome_attribution_updates"`
	EnablePricingLearning    bool `yaml:"enable_pricing_learning"`
	EnablePortfolioLearning  bool `yaml:"enable_portfolio_learning"`
	EnableReflectionSignals  bool `yaml:"enable_reflection_signals"`
	AllowWeightAdj           bool `yaml:"allow_weight_adjustments"`
	AllowThresholdAdj        bool `yaml:"allow_threshold_adjustments"`
}

type ObjectiveCfg struct {
	NeutralNetUtility float64            `yaml:"neutral_net_utility"`
	RiskPenaltyWeight float64            `yaml:"risk_penalty_weight"`
	Weights           map[string]float64 `yaml:"weights"`
	SignalBounds      SignalBounds       `yaml:"signal_bounds"`
}

type SignalBounds struct {
	BoostMax   float64 `yaml:"boost_max"`
	PenaltyMax float64 `yaml:"penalty_max"`
}

type PortfolioCfg struct {
	RebalanceEnabled       bool `yaml:"rebalance_enabled"`
	FamilySafeMode         bool `yaml:"family_safe_mode"`
	EnforceDiversification bool `yaml:"enforce_diversification"`
}

type PricingCfg struct {
	Enabled bool `yaml:"enabled"`
}

type SchedulingCfg struct {
	Enabled            bool `yaml:"enabled"`
	AllowRecompute     bool `yaml:"allow_recompute"`
	AllowAutoProposals bool `yaml:"allow_auto_proposals"`
	AllowCalendarWrite bool `yaml:"allow_calendar_write"`
}

type ExtActionsCfg struct {
	Enabled   bool `yaml:"enabled"`
	Execution struct {
		AutoExecSafeInternal      bool `yaml:"auto_execute_safe_internal"`
		AutoExecSafeExternalDraft bool `yaml:"auto_execute_safe_external_draft"`
		AutoExecRealExternalSend  bool `yaml:"auto_execute_real_external_send"`
		RequireReviewForGuarded   bool `yaml:"require_review_for_guarded_actions"`
	} `yaml:"execution_policy"`
}

type SelfExtCfg struct {
	Enabled    bool `yaml:"enabled"`
	AutoDeploy struct {
		Enabled                       bool `yaml:"enabled"`
		OnlyLowRisk                   bool `yaml:"only_low_risk"`
		RequireAllTestsPass           bool `yaml:"require_all_tests_pass"`
		RequireNoExternalEffects      bool `yaml:"require_no_external_effects"`
		RequireNoCorePipelineMutation bool `yaml:"require_no_core_pipeline_mutation"`
	} `yaml:"auto_deploy"`
	BlockedChangeTypes []string `yaml:"blocked_change_types"`
}

type ActuationCfg struct {
	Enabled       bool `yaml:"enabled"`
	AllowRun      bool `yaml:"allow_run"`
	AllowRouting  bool `yaml:"allow_routing"`
	AllowExecSafe bool `yaml:"allow_execution_of_safe_actions"`
	Limits        struct {
		MaxDecisionsPerRun      int `yaml:"max_decisions_per_run"`
		MaxHeavyPerRun          int `yaml:"max_heavy_decisions_per_run"`
		MaxReviewRequiredPerRun int `yaml:"max_review_required_per_run"`
	} `yaml:"limits"`
}

type ActPriorityCfg struct {
	Enabled              bool `yaml:"enabled"`
	RequireBeforeRouting bool `yaml:"require_before_routing"`
}

type ReportingCfg struct {
	Enabled bool `yaml:"enabled"`
	Cadence struct {
		OperationalHours    int  `yaml:"operational_hours"`
		DailySummaryEnabled bool `yaml:"daily_summary_enabled"`
		DailySummaryHour    int  `yaml:"daily_summary_hour"`
	} `yaml:"cadence"`
	ImmediateOn []string `yaml:"immediate_on"`
	Thresholds  struct {
		MinMeaningfulChangeScore float64 `yaml:"min_meaningful_change_score"`
		MaxSilenceHours          int     `yaml:"max_silence_hours"`
	} `yaml:"thresholds"`
	Include ReportInclude `yaml:"include"`
}

type ReportInclude struct {
	ObjectiveSnapshot   bool `yaml:"objective_snapshot"`
	RiskSnapshot        bool `yaml:"risk_snapshot"`
	ReflectionSummary   bool `yaml:"reflection_summary"`
	ActuationSummary    bool `yaml:"actuation_summary"`
	SuppressedDecisions bool `yaml:"suppressed_decisions"`
	SafeActionsExecuted bool `yaml:"safe_actions_executed"`
	ReviewQueueSummary  bool `yaml:"review_queue_summary"`
	SelfExtSummary      bool `yaml:"self_extension_summary"`
	FailuresAndWarnings bool `yaml:"failures_and_warnings"`
}

type ObservabilityCfg struct {
	EmitAuditEvents    bool `yaml:"emit_audit_events"`
	PersistReports     bool `yaml:"persist_reports"`
	PersistLatestState bool `yaml:"persist_latest_runtime_state"`
	LogCycleSummary    bool `yaml:"log_cycle_summary"`
	LogSuppressed      bool `yaml:"log_suppressed_decisions"`
	LogDowngrades      bool `yaml:"log_downgrades"`
}

type RuntimeCfg struct {
	FailOpenOptional bool `yaml:"fail_open_optional_providers"`
	FailSafeRisky    bool `yaml:"fail_safe_risky_execution"`
	StopOnPanic      bool `yaml:"stop_on_panic"`
	RecoverAndLog    bool `yaml:"recover_and_log"`
}

type LaunchCfg struct {
	RequireCleanBuild bool `yaml:"require_clean_build"`
	RequireTests      bool `yaml:"require_tests_passed"`
	RequireMigrations bool `yaml:"require_migrations_applied"`
	RequireDB         bool `yaml:"require_db_ready"`
	RequireNATS       bool `yaml:"require_nats_ready"`
}

// LoadAutonomyConfig reads and validates the autonomy config from a YAML file.
func LoadAutonomyConfig(path string) (*AutonomyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read autonomy config %s: %w", path, err)
	}
	var cfg AutonomyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse autonomy config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("autonomy config validation: %w", err)
	}
	return &cfg, nil
}

// Validate performs semantic validation of the autonomy config.
func (c *AutonomyConfig) Validate() error {
	var errs []string

	// Mode validation.
	validModes := map[AutonomyMode]bool{
		ModeFrozen: true, ModeSupervisedAutonomy: true,
		ModeBoundedAutonomy: true, ModeAutonomous: true,
	}
	if !validModes[c.Mode] {
		errs = append(errs, fmt.Sprintf("mode must be one of frozen, supervised_autonomy, bounded_autonomy, autonomous; got %q", c.Mode))
	}

	// Scheduler.
	if c.Scheduler.Enabled {
		if c.Scheduler.TickSeconds <= 0 {
			errs = append(errs, "scheduler.tick_seconds must be > 0")
		}
		if c.Scheduler.Cycles.ReflectionHours <= 0 {
			errs = append(errs, "scheduler.cycles.reflection_hours must be > 0")
		}
		if c.Scheduler.Cycles.ObjectiveHours <= 0 {
			errs = append(errs, "scheduler.cycles.objective_hours must be > 0")
		}
	}

	// Execution window.
	if c.ExecWindow.Enabled {
		if !isValidTime(c.ExecWindow.Start) {
			errs = append(errs, fmt.Sprintf("execution_window.start must be HH:MM; got %q", c.ExecWindow.Start))
		}
		if !isValidTime(c.ExecWindow.End) {
			errs = append(errs, fmt.Sprintf("execution_window.end must be HH:MM; got %q", c.ExecWindow.End))
		}
	}

	// Limits.
	if c.Limits.MaxActionsPerCycle <= 0 {
		errs = append(errs, "limits.max_actions_per_cycle must be > 0")
	}
	if c.Limits.MaxConsecutiveFailedCycles <= 0 {
		errs = append(errs, "limits.max_consecutive_failed_cycles must be > 0")
	}

	// Risk thresholds.
	if c.Risk.Thresholds.FailureRate <= 0 || c.Risk.Thresholds.FailureRate > 1 {
		errs = append(errs, "risk.thresholds.failure_rate_threshold must be in (0,1]")
	}
	if c.Risk.Thresholds.Overload <= 0 || c.Risk.Thresholds.Overload > 1 {
		errs = append(errs, "risk.thresholds.overload_threshold must be in (0,1]")
	}
	if c.Risk.Thresholds.Pressure <= 0 || c.Risk.Thresholds.Pressure > 1 {
		errs = append(errs, "risk.thresholds.pressure_threshold must be in (0,1]")
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid autonomy config:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// IsInsideWindow returns true if the given time is within the execution window.
func (c *AutonomyConfig) IsInsideWindow(t time.Time) bool {
	if !c.ExecWindow.Enabled {
		return true // no window means always active
	}
	startH, startM := parseHHMM(c.ExecWindow.Start)
	endH, endM := parseHHMM(c.ExecWindow.End)

	nowMin := t.Hour()*60 + t.Minute()
	startMin := startH*60 + startM
	endMin := endH*60 + endM

	if startMin <= endMin {
		return nowMin >= startMin && nowMin < endMin
	}
	// Wrap-around (e.g., 22:00 - 06:00)
	return nowMin >= startMin || nowMin < endMin
}

// CycleDuration returns the duration for a given cycle based on config.
func (c *AutonomyConfig) CycleDuration(cycle string) time.Duration {
	hours := 0
	switch cycle {
	case "reflection":
		hours = c.Scheduler.Cycles.ReflectionHours
	case "objective":
		hours = c.Scheduler.Cycles.ObjectiveHours
	case "actuation":
		hours = c.Scheduler.Cycles.ActuationHours
	case "actuation_priority":
		hours = c.Scheduler.Cycles.ActuationPrioHours
	case "scheduling":
		hours = c.Scheduler.Cycles.SchedulingHours
	case "portfolio":
		hours = c.Scheduler.Cycles.PortfolioHours
	case "discovery":
		hours = c.Scheduler.Cycles.DiscoveryHours
	case "pricing_performance":
		hours = c.Scheduler.Cycles.PricingPerfHours
	case "self_extension":
		hours = c.Scheduler.Cycles.SelfExtHours
	case "task_recompute":
		hours = c.Scheduler.Cycles.TaskRecomputeHours
	case "task_dispatch":
		hours = c.Scheduler.Cycles.TaskDispatchHours
	case "goal_planning":
		hours = c.Scheduler.Cycles.GoalPlanningHours
	case "reporting":
		hours = c.Scheduler.Cycles.ReportingHours
	}
	if hours <= 0 {
		hours = 24 // default fallback
	}
	return time.Duration(hours) * time.Hour
}

func isValidTime(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h := (int(s[0]-'0'))*10 + int(s[1]-'0')
	m := (int(s[3]-'0'))*10 + int(s[4]-'0')
	return h >= 0 && h <= 23 && m >= 0 && m <= 59
}

func parseHHMM(s string) (int, int) {
	h := (int(s[0]-'0'))*10 + int(s[1]-'0')
	m := (int(s[3]-'0'))*10 + int(s[4]-'0')
	return h, m
}
