package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/actions"
	"github.com/tiroq/arcanum/internal/agent/actuation"
	"github.com/tiroq/arcanum/internal/agent/autonomy"
	"github.com/tiroq/arcanum/internal/agent/calibration"
	"github.com/tiroq/arcanum/internal/agent/capacity"
	"github.com/tiroq/arcanum/internal/agent/causal"
	"github.com/tiroq/arcanum/internal/agent/counterfactual"
	decision_graph "github.com/tiroq/arcanum/internal/agent/decision_graph"
	"github.com/tiroq/arcanum/internal/agent/discovery"
	"github.com/tiroq/arcanum/internal/agent/exploration"
	externalactions "github.com/tiroq/arcanum/internal/agent/external_actions"
	financialpressure "github.com/tiroq/arcanum/internal/agent/financial_pressure"
	financialtruth "github.com/tiroq/arcanum/internal/agent/financial_truth"
	"github.com/tiroq/arcanum/internal/agent/goals"
	"github.com/tiroq/arcanum/internal/agent/governance"
	"github.com/tiroq/arcanum/internal/agent/income"
	meta_reasoning "github.com/tiroq/arcanum/internal/agent/meta_reasoning"
	"github.com/tiroq/arcanum/internal/agent/objective"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	pathcomparison "github.com/tiroq/arcanum/internal/agent/path_comparison"
	pathlearning "github.com/tiroq/arcanum/internal/agent/path_learning"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/agent/policy"
	"github.com/tiroq/arcanum/internal/agent/portfolio"
	"github.com/tiroq/arcanum/internal/agent/pricing"
	providercatalog "github.com/tiroq/arcanum/internal/agent/provider_catalog"
	providerrouting "github.com/tiroq/arcanum/internal/agent/provider_routing"
	"github.com/tiroq/arcanum/internal/agent/reflection"
	resourceopt "github.com/tiroq/arcanum/internal/agent/resource_optimization"
	"github.com/tiroq/arcanum/internal/agent/scheduler"
	"github.com/tiroq/arcanum/internal/agent/scheduling"
	selfextension "github.com/tiroq/arcanum/internal/agent/self_extension"
	"github.com/tiroq/arcanum/internal/agent/signals"
	"github.com/tiroq/arcanum/internal/agent/stability"
	"github.com/tiroq/arcanum/internal/agent/strategy"
	strategylearning "github.com/tiroq/arcanum/internal/agent/strategy_learning"
	"github.com/tiroq/arcanum/internal/api"
	"github.com/tiroq/arcanum/internal/audit"
	"github.com/tiroq/arcanum/internal/config"
	"github.com/tiroq/arcanum/internal/db"
	"github.com/tiroq/arcanum/internal/health"
	"github.com/tiroq/arcanum/internal/logging"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
	"go.uber.org/zap"
)

const (
	serviceName    = "api-gateway"
	serviceVersion = "0.1.0"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := logging.NewLogger(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck
	logger = logging.WithService(logger, serviceName, serviceVersion)

	registry := prometheus.NewRegistry()
	m, err := metrics.NewMetrics(registry)
	if err != nil {
		return fmt.Errorf("init metrics: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.Database.DSN, cfg.Database.MaxConns)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	logger.Info("database connected")

	nc, err := natsgo.Connect(cfg.NATS.URL,
		natsgo.Name(serviceName),
		natsgo.MaxReconnects(-1),
		natsgo.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("connect to nats: %w", err)
	}
	defer nc.Drain() //nolint:errcheck
	logger.Info("nats connected", zap.String("url", cfg.NATS.URL))

	readiness := &health.ReadinessChecker{DB: pool, NATS: nc}

	publisher, err := messaging.NewPublisher(nc, logger)
	if err != nil {
		return fmt.Errorf("create publisher: %w", err)
	}
	goalEngine := goals.NewGoalEngine(pool, logger)

	// Action engine wiring.
	auditor := audit.NewPostgresAuditRecorder(pool)
	apiBaseURL := fmt.Sprintf("http://localhost:%d", cfg.HTTP.Port)
	staticPlanner := actions.NewPlanner(pool, logger)
	guardrails := actions.NewGuardrails(pool, logger)
	executor := actions.NewExecutor(apiBaseURL, cfg.Auth.AdminToken, logger)

	// Action memory + feedback loop (Iteration 5).
	memoryStore := actionmemory.NewStore(pool)
	feedbackAdapter := actionmemory.NewFeedbackAdapter(memoryStore)
	guardrails.WithFeedback(feedbackAdapter)

	// Adaptive planning layer (Iteration 6).
	contextCollector := planning.NewContextCollector(pool, memoryStore, logger)
	adaptivePlanner := planning.NewAdaptivePlanner(contextCollector, staticPlanner, auditor, logger)

	// Decision journal (Iteration 8).
	decisionJournal := planning.NewDecisionJournal(pool)
	adaptivePlanner.WithJournal(decisionJournal)

	// Policy adaptation layer (Iteration 10).
	policyStore := policy.NewStore(pool)
	policyAdapter := policy.NewPlannerAdapter(policyStore)
	adaptivePlanner.WithPolicy(policyAdapter)

	actionEngine := actions.NewEngine(goalEngine, adaptivePlanner, guardrails, executor, auditor, logger)

	// Outcome verification layer (Iteration 4).
	outcomeEval := outcome.NewEvaluator(pool, logger)
	outcomeStore := outcome.NewStore(pool)
	outcomeHandler := outcome.NewHandler(outcomeEval, outcomeStore, auditor, logger).
		WithMemoryStore(memoryStore)
	actionEngine.WithOutcomeVerification(outcomeHandler)

	// Self-reflection engine (Iteration 8).
	reflectionFindingStore := reflection.NewStore(pool)
	reflectionEngine := reflection.NewEngine(pool, decisionJournal, outcomeStore, memoryStore, reflectionFindingStore, auditor, logger)

	// Self-stability layer (Iteration 9).
	stabilityStore := stability.NewStore(pool)
	stabilityEngine := stability.NewEngine(
		stabilityStore,
		decisionJournal,
		outcomeStore,
		memoryStore,
		reflectionFindingStore,
		auditor,
		logger,
	)
	stabilityGuardrailAdapter := stability.NewGuardrailAdapter(stabilityStore)
	guardrails.WithStability(stabilityGuardrailAdapter)

	// Policy engine (Iteration 10) — after reflection + stability are available.
	policyEngine := policy.NewEngine(
		policyStore,
		memoryStore,
		reflectionFindingStore,
		stabilityEngine,
		auditor,
		logger,
	)

	// Causal reasoning layer (Iteration 11).
	causalStore := causal.NewStore(pool)
	causalEngine := causal.NewEngine(
		causalStore,
		policyStore,
		memoryStore,
		stabilityEngine,
		auditor,
		logger,
	)

	// Autonomous scheduler (Iteration 7).
	agentScheduler := scheduler.New(
		actionEngine,
		time.Duration(cfg.Scheduler.IntervalSeconds)*time.Second,
		time.Duration(cfg.Scheduler.TimeoutSeconds)*time.Second,
		auditor,
		logger,
	)

	// Wire stability into scheduler.
	schedulerStabilityAdapter := stability.NewSchedulerAdapter(stabilityEngine)
	agentScheduler.WithStability(schedulerStabilityAdapter)

	// Exploration vs exploitation layer (Iteration 16).
	explorationBudgetStore := exploration.NewBudgetStore(pool)
	explorationStabilityAdapter := stability.NewExplorationStabilityAdapter(stabilityEngine)
	explorationEngine := exploration.NewEngine(explorationBudgetStore, explorationStabilityAdapter, auditor, logger)
	explorationPlannerAdapter := exploration.NewPlannerAdapter(explorationEngine)
	adaptivePlanner.WithExploration(explorationPlannerAdapter)

	// Strategic planning layer (Iteration 17).
	strategyStore := strategy.NewStore(pool)
	strategyStabilityAdapter := stability.NewStrategyStabilityAdapter(stabilityEngine)
	strategyEngine := strategy.NewEngine(strategyStore, strategyStabilityAdapter, auditor, logger)
	strategyPlannerAdapter := strategy.NewPlannerAdapter(strategyEngine)
	_ = strategyPlannerAdapter // retained for backward compat; decision graph replaces portfolio

	// Decision graph layer (Iteration 20) — replaces strategy portfolio competition.
	graphStabilityAdapter := stability.NewStrategyStabilityAdapter(stabilityEngine)
	graphAdapter := decision_graph.NewGraphPlannerAdapter(graphStabilityAdapter, auditor, logger)

	// Path learning layer (Iteration 21) — path memory + transition learning.
	pathMemStore := pathlearning.NewMemoryStore(pool)
	pathTransStore := pathlearning.NewTransitionStore(pool)
	pathLearningAdapter := pathlearning.NewGraphAdapter(pathMemStore, pathTransStore, logger)
	graphAdapter.WithPathLearning(pathLearningAdapter)

	// Wire path learning evaluator into outcome handler for path-level outcome evaluation.
	pathLearningEvaluator := pathlearning.NewEvaluator(pathMemStore, pathTransStore, auditor, logger)
	outcomeHandler.WithPathOutcomeEvaluator(pathLearningEvaluator)

	// Comparative path selection learning (Iteration 22).
	compSnapshotStore := pathcomparison.NewSnapshotStore(pool)
	compOutcomeStore := pathcomparison.NewOutcomeStore(pool)
	compMemoryStore := pathcomparison.NewMemoryStore(pool)
	compGraphAdapter := pathcomparison.NewGraphAdapter(compMemoryStore, logger)
	graphAdapter.WithComparativeLearning(compGraphAdapter)

	// Wire snapshot capturer for decision snapshots.
	snapshotCapturer := pathcomparison.NewSnapshotCapturerAdapter(compSnapshotStore, auditor, logger)
	graphAdapter.WithSnapshotCapturer(snapshotCapturer)

	// Wire comparative evaluator into outcome handler to evaluate decision quality.
	compEvaluator := pathcomparison.NewEvaluator(compSnapshotStore, compOutcomeStore, compMemoryStore, auditor, logger)
	outcomeHandler.WithComparativeEvaluator(compEvaluator)

	// Counterfactual simulation layer (Iteration 23).
	cfSimStore := counterfactual.NewSimulationStore(pool)
	cfOutcomeStore := counterfactual.NewPredictionOutcomeStore(pool)
	cfMemoryStore := counterfactual.NewPredictionMemoryStore(pool)
	cfAdapter := counterfactual.NewGraphAdapter(cfSimStore, cfMemoryStore, pathLearningAdapter, compGraphAdapter, auditor, logger)
	graphAdapter.WithCounterfactual(cfAdapter)

	// Wire counterfactual prediction evaluator into outcome handler.
	cfPredictor := counterfactual.NewPredictor(cfSimStore, cfOutcomeStore, cfMemoryStore, auditor, logger)
	outcomeHandler.WithCounterfactualEvaluator(cfPredictor)

	// Meta-reasoning layer (Iteration 24).
	metaMemoryStore := meta_reasoning.NewMemoryStore(pool)
	metaHistoryStore := meta_reasoning.NewHistoryStore(pool)
	metaEngine := meta_reasoning.NewEngine(metaMemoryStore, metaHistoryStore, auditor, logger)
	metaGraphAdapter := meta_reasoning.NewGraphAdapter(metaEngine)
	graphAdapter.WithMetaReasoning(metaGraphAdapter)

	// Wire meta-reasoning outcome evaluator into outcome handler.
	outcomeHandler.WithMetaReasoningEvaluator(metaEngine)

	// Self-calibration layer (Iteration 25).
	calibrationTracker := calibration.NewTracker(pool)
	calibrator := calibration.NewCalibrator(calibrationTracker, auditor, logger)
	calibrationGraphAdapter := calibration.NewGraphAdapter(calibrator, logger)
	graphAdapter.WithCalibration(calibrationGraphAdapter)

	// Wire calibration recorder into outcome handler.
	calibrationOutcomeAdapter := calibration.NewOutcomeAdapter(calibrator, logger)
	outcomeHandler.WithCalibrationRecorder(calibrationOutcomeAdapter)

	// Contextual confidence calibration layer (Iteration 26).
	contextCalStore := calibration.NewContextStore(pool)
	contextCalibrator := calibration.NewContextCalibrator(contextCalStore, auditor, logger)
	contextCalGraphAdapter := calibration.NewContextGraphAdapter(contextCalibrator, logger)
	graphAdapter.WithContextualCalibration(contextCalGraphAdapter)

	// Wire contextual calibration recorder into outcome handler.
	contextCalOutcomeAdapter := calibration.NewContextOutcomeAdapter(contextCalibrator, logger)
	outcomeHandler.WithContextualCalibrationRecorder(contextCalOutcomeAdapter)

	// Mode-specific calibration layer (Iteration 28).
	modeCalTracker := calibration.NewModeTracker(pool)
	modeCalibrator := calibration.NewModeCalibrator(modeCalTracker, auditor, logger)
	modeCalGraphAdapter := calibration.NewModeGraphAdapter(modeCalibrator, logger)
	graphAdapter.WithModeCalibration(modeCalGraphAdapter)

	// Wire mode calibration recorder into outcome handler.
	modeCalOutcomeAdapter := calibration.NewModeOutcomeAdapter(modeCalibrator, logger)
	outcomeHandler.WithModeCalibrationRecorder(modeCalOutcomeAdapter)

	// Resource / cost / latency-aware optimization layer (Iteration 29).
	resourceTracker := resourceopt.NewTracker(pool)
	resourceAdapter := resourceopt.NewGraphAdapter(resourceTracker, auditor, logger)
	graphAdapter.WithResourceOptimization(resourceAdapter)

	// Wire resource outcome recorder into outcome handler.
	resourceOutcomeAdapter := resourceopt.NewOutcomeAdapter(resourceAdapter, logger)
	outcomeHandler.WithResourceOutcomeRecorder(resourceOutcomeAdapter)

	// Human override + governance layer (Iteration 30).
	govStateStore := governance.NewStateStore(pool)
	govActionStore := governance.NewActionStore(pool)
	govController := governance.NewController(govStateStore, govActionStore, auditor, logger)
	govAdapter := governance.NewControllerAdapter(govController, logger)
	graphAdapter.WithGovernance(govAdapter)

	// Wire governance replay pack support.
	govReplayStore := governance.NewReplayStore(pool)
	govReplayBuilder := governance.NewReplayPackBuilder(govReplayStore, auditor, logger)
	govReplayAdapter := governance.NewGraphReplayAdapter(govReplayBuilder)
	graphAdapter.WithReplayRecorder(govReplayAdapter)

	// Wire governance learning guard into outcome handler.
	outcomeHandler.WithGovernanceLearningGuard(govAdapter)

	// Provider policy + quota-aware routing layer (Iteration 31).
	providerRegistry := providerrouting.NewRegistry()
	// Register default local provider (always available).
	providerRegistry.Register(providerrouting.Provider{
		Name:         "ollama",
		Kind:         providerrouting.KindLocal,
		Roles:        []string{providerrouting.RoleFast, providerrouting.RolePlanner, providerrouting.RoleReviewer, providerrouting.RoleBatch, providerrouting.RoleFallback},
		Capabilities: []string{"json_mode", "low_latency"},
		Limits:       providerrouting.ProviderLimits{}, // local = no external limits
		Cost:         providerrouting.ProviderCostModel{CostClass: providerrouting.CostLocal, RelativeCost: 0.0},
		Health:       providerrouting.ProviderHealth{Enabled: true, Reachable: true},
	})
	// Register cloud providers if configured.
	if cfg.Providers.OpenRouter.Enabled {
		providerRegistry.Register(providerrouting.Provider{
			Name:         "openrouter",
			Kind:         providerrouting.KindRouter,
			Roles:        []string{providerrouting.RolePlanner, providerrouting.RoleReviewer, providerrouting.RoleFallback},
			Capabilities: []string{"json_mode", "long_context", "tool_calling"},
			Limits:       providerrouting.ProviderLimits{RPM: 20, RPD: 200},
			Cost:         providerrouting.ProviderCostModel{CostClass: providerrouting.CostFree, RelativeCost: 0.1},
			Health:       providerrouting.ProviderHealth{Enabled: true, Reachable: true},
		})
	}
	if cfg.Providers.OllamaCloud.Enabled {
		providerRegistry.Register(providerrouting.Provider{
			Name:         "ollama-cloud",
			Kind:         providerrouting.KindCloud,
			Roles:        []string{providerrouting.RolePlanner, providerrouting.RoleReviewer, providerrouting.RoleBatch},
			Capabilities: []string{"json_mode"},
			Limits:       providerrouting.ProviderLimits{},
			Cost:         providerrouting.ProviderCostModel{CostClass: providerrouting.CostCheap, RelativeCost: 0.2},
			Health:       providerrouting.ProviderHealth{Enabled: true, Reachable: true},
		})
	}

	quotaTracker := providerrouting.NewQuotaTracker(pool)
	if err := quotaTracker.LoadFromDB(ctx); err != nil {
		logger.Warn("failed to load provider usage from DB", zap.Error(err))
	}
	providerRouter := providerrouting.NewRouter(providerRegistry, quotaTracker, auditor, logger)

	// Load global routing policy from providers/_global.yaml and wire it into the
	// routing engine. This makes _global.yaml the authoritative source of truth for:
	//   - allow_external (global gate for external providers)
	//   - max_fallback_chain (overrides MaxFallbackChainLength constant)
	//   - role-based provider preference ordering
	//   - degrade_policy tier ordering for fallback chain assembly
	// Previously _global.yaml was silently skipped by the catalog loader and had
	// zero runtime effect. It is now enforced here (Iteration 33 cleanup).
	globalPolicy, err := providercatalog.LoadGlobalPolicy(cfg.Providers.CatalogDir, logger)
	if err != nil {
		logger.Warn("failed to load global routing policy; using defaults", zap.Error(err))
	}
	if globalPolicy != nil {
		rp := globalPolicy.RoutingPolicy
		policyCfg := &providerrouting.GlobalPolicyConfig{
			PreferFree:       rp.PreferFree,
			AllowExternal:    rp.AllowExternal,
			MaxFallbackChain: rp.MaxFallbackChain,
			DegradePolicy:    rp.DegradePolicy,
		}
		if len(rp.Priorities) > 0 {
			policyCfg.RolePreferences = make(map[string][]string, len(rp.Priorities))
			for role, prio := range rp.Priorities {
				policyCfg.RolePreferences[role] = prio.Prefer
			}
		}
		providerRouter.WithGlobalPolicy(policyCfg)
		logger.Info("global routing policy wired into provider router",
			zap.Bool("allow_external", policyCfg.AllowExternal),
			zap.Int("max_fallback_chain", policyCfg.MaxFallbackChain),
			zap.Int("role_preferences", len(policyCfg.RolePreferences)),
		)
	}

	providerRoutingAdapter := providerrouting.NewGraphAdapter(providerRouter, auditor, logger)

	// Provider catalog + model-aware routing layer (Iteration 32).
	catalogEntries, err := providercatalog.LoadCatalog(cfg.Providers.CatalogDir, logger)
	if err != nil {
		logger.Warn("failed to load provider catalog", zap.Error(err))
	}
	catalogRegistry := providercatalog.NewCatalogRegistry()
	catalogRegistry.BuildFromCatalog(catalogEntries)
	logger.Info("provider catalog loaded",
		zap.Int("models", catalogRegistry.Count()),
		zap.Int("files", len(catalogEntries)),
	)

	// Build and wire model execution map into the router.
	// Maps "provider/model" → ExecutionConfig derived from catalog models[].execution blocks.
	// This enables the router to populate ExecutionPlan.Execution for selected models.
	catalogExecMap := providercatalog.BuildModelExecutionMap(catalogEntries)
	if len(catalogExecMap) > 0 {
		routingExecMap := make(map[string]providerrouting.ExecutionConfig, len(catalogExecMap))
		for key, spec := range catalogExecMap {
			routingExecMap[key] = providerrouting.ExecutionConfig{
				TimeoutSeconds:  spec.TimeoutSeconds,
				ThinkMode:       spec.Think,
				JSONMode:        spec.JSONMode,
				MaxOutputTokens: spec.MaxOutputTokens,
			}
		}
		providerRouter.WithModelExecutionMap(routingExecMap)
		logger.Info("model execution map wired into provider router",
			zap.Int("entries", len(routingExecMap)),
		)
	}

	// Wire provider routing into decision graph.
	graphAdapter.WithProviderRouting(providerRoutingAdapter)

	// Goal-driven execution layer (Iteration 35).
	// Load strategic system goals from YAML at startup. Fail-open: if file missing or
	// invalid, the graph adapter is nil and goal alignment is skipped entirely.
	systemGoals, err := goals.LoadSystemGoals(cfg.Agent.SystemGoalsPath)
	if err != nil {
		logger.Warn("system goals load failed; goal alignment disabled",
			zap.String("path", cfg.Agent.SystemGoalsPath),
			zap.Error(err),
		)
	} else {
		goalAlignmentAdapter := goals.NewGoalGraphAdapter(systemGoals.Goals)
		graphAdapter.WithGoalAlignment(goalAlignmentAdapter)
		logger.Info("system goals loaded",
			zap.Int("goals", len(systemGoals.Goals)),
			zap.String("path", cfg.Agent.SystemGoalsPath),
		)
	}

	// Income engine (Iteration 36).
	// Creates the income pipeline: stores + engine + graph adapter.
	// Fail-open: if DB is unavailable, income engine still starts but queries fail gracefully.
	incomeOppStore := income.NewOpportunityStore(pool)
	incomePropStore := income.NewProposalStore(pool)
	incomeOutcomeStore := income.NewOutcomeStore(pool)
	incomeLearningStore := income.NewLearningStore(pool)
	incomeEngine := income.NewEngine(incomeOppStore, incomePropStore, incomeOutcomeStore, auditor, logger)
	incomeEngine.WithLearning(incomeLearningStore)
	if govAdapter != nil {
		incomeEngine.WithGovernance(govAdapter)
	}
	incomeGraphAdapter := income.NewGraphAdapter(incomeEngine, logger)
	graphAdapter.WithIncomeSignals(incomeGraphAdapter)
	graphAdapter.WithOutcomeAttribution(incomeGraphAdapter)
	logger.Info("income engine initialised")

	// Signal ingestion (Iteration 37).
	// Creates the signal pipeline: stores + engine + graph adapter.
	// Fail-open: if DB is unavailable, signal engine still starts but queries fail gracefully.
	rawEventStore := signals.NewRawEventStore(pool)
	signalStore := signals.NewSignalStore(pool)
	derivedStateStore := signals.NewDerivedStateStore(pool)
	signalEngine := signals.NewEngine(rawEventStore, signalStore, derivedStateStore, auditor, logger)
	signalGraphAdapter := signals.NewGraphAdapter(signalEngine, logger)
	graphAdapter.WithSignalIngestion(signalGraphAdapter)
	logger.Info("signal ingestion engine initialised")

	// Financial pressure (Iteration 38).
	// Creates the financial pressure pipeline: store + adapter.
	// Fail-open: if DB is unavailable, pressure returns 0 (no boost).
	financialPressureStore := financialpressure.NewStore(pool)
	financialPressureAdapter := financialpressure.NewGraphAdapter(financialPressureStore, auditor, logger)
	graphAdapter.WithFinancialPressure(financialPressureAdapter)
	logger.Info("financial pressure layer initialised")

	// Financial truth layer (Iteration 42).
	// Creates the financial truth pipeline: stores + engine + adapters.
	// Fail-open: if DB is unavailable, truth engine starts but queries fail gracefully.
	financialEventStore := financialtruth.NewEventStore(pool)
	financialFactStore := financialtruth.NewFactStore(pool)
	financialMatchStore := financialtruth.NewMatchStore(pool)
	financialTruthEngine := financialtruth.NewEngine(financialEventStore, financialFactStore, financialMatchStore, auditor, logger)

	// Wire truth provider into financial pressure (prefer verified income).
	pressureTruthAdapter := financialtruth.NewPressureTruthAdapter(financialTruthEngine)
	financialPressureAdapter.WithTruthProvider(pressureTruthAdapter)

	// Wire truth provider into income engine (prefer verified value for learning).
	learningTruthAdapter := financialtruth.NewLearningTruthAdapter(financialTruthEngine)
	incomeEngine.WithTruthProvider(learningTruthAdapter)
	logger.Info("financial truth layer initialised")

	// Opportunity discovery engine (Iteration 40).
	// Creates the discovery pipeline: store + adapters + dedup + promoter + engine.
	// Fail-open: if DB is unavailable, discovery engine starts but queries fail gracefully.
	discoveryCandidateStore := discovery.NewCandidateStore(pool)
	discoverySignalAdapter := discovery.NewSignalStoreAdapter(pool)
	discoveryOutcomeAdapter := discovery.NewOutcomeStoreAdapter(pool)
	discoveryProposalAdapter := discovery.NewProposalStoreAdapter(pool)
	discoveryOppAdapter := discovery.NewOpportunityStoreAdapter(pool)
	discoveryDeduplicator := discovery.NewDeduplicator(discoveryCandidateStore, discoveryOppAdapter, discovery.DedupeWindowHours)
	discoveryPromoter := discovery.NewPromoter(incomeEngine)
	discoveryEngine := discovery.NewEngine(discoveryCandidateStore, discoveryDeduplicator, discoveryPromoter, auditor, logger).
		WithSignals(discoverySignalAdapter).
		WithOutcomes(discoveryOutcomeAdapter).
		WithProposals(discoveryProposalAdapter)
	logger.Info("opportunity discovery engine initialised")

	// Time allocation / owner capacity layer (Iteration 41).
	// Loads family context, creates capacity engine + adapter.
	// Integrates with signals for owner_load_score.
	// Fail-open: if family_context.yaml is missing, defaults are used.
	familyCfg := capacity.LoadFamilyConfig("configs/family_context.yaml")
	capacityStore := capacity.NewStore(pool)
	capacityEngine := capacity.NewEngine(capacityStore, familyCfg, auditor, logger)
	// Wire signals-derived state (owner_load_score) into capacity engine.
	signalDerivedAdapter := capacity.NewSignalDerivedAdapter(func(ctx context.Context) map[string]float64 {
		active := signalEngine.GetActiveSignals(ctx)
		return active.Derived
	})
	capacityEngine.WithDerivedState(signalDerivedAdapter)
	capacityGraphAdapter := capacity.NewGraphAdapter(capacityEngine, logger)
	graphAdapter.WithCapacity(capacityGraphAdapter)
	logger.Info("time allocation layer initialised",
		zap.Float64("max_daily_hours", familyCfg.MaxDailyWorkHours),
		zap.Float64("min_family_hours", familyCfg.MinFamilyTimeHours),
		zap.Int("blocked_ranges", len(familyCfg.BlockedRanges)),
	)

	// Controlled self-extension sandbox (Iteration 43).
	// Creates the self-extension pipeline: store + engine + adapter.
	// Integrates with capacity layer to gate sandbox builds on owner availability.
	// Fail-safe for deployment: requires explicit approval, never auto-deploys.
	selfExtStore := selfextension.NewStore(pool)
	selfExtEngine := selfextension.NewEngine(selfExtStore, auditor, logger)
	selfExtEngine.WithCapacity(capacityGraphAdapter)
	selfExtAdapter := selfextension.NewGraphAdapter(selfExtEngine, logger)
	logger.Info("self-extension sandbox initialised")

	// External action connectors (Iteration 45).
	// Creates the external actions pipeline: store + connectors + router + policy + engine + adapter.
	// Fail-open: connectors that are unavailable are skipped, never crash.
	// Fail-safe: high-risk actions require human approval before execution.
	extActStore := externalactions.NewStore(pool)
	extActRouter := externalactions.NewConnectorRouter()
	extActRouter.Register(externalactions.NewNoopConnector())
	extActRouter.Register(externalactions.NewLogConnector())
	extActRouter.Register(externalactions.NewHTTPConnector(nil)) // no transport by default; dry-run only
	extActRouter.Register(externalactions.NewEmailDraftConnector())
	extActPolicy := externalactions.NewPolicyEngine()
	extActEngine := externalactions.NewEngine(extActStore, extActRouter, extActPolicy, auditor, logger)
	extActAdapter := externalactions.NewGraphAdapter(extActEngine, logger)
	logger.Info("external action connectors initialised",
		zap.Int("connectors", len(extActRouter.List())),
	)

	// Strategic revenue portfolio (Iteration 46).
	// Creates the portfolio pipeline: stores + engine + adapter.
	// Integrates with financial pressure + capacity for informed allocation.
	// Decision graph receives strategy-level boost/penalty.
	// Fail-open: if DB is unavailable, portfolio engine starts but queries fail gracefully.
	portfolioStrategyStore := portfolio.NewStrategyStore(pool)
	portfolioAllocationStore := portfolio.NewAllocationStore(pool)
	portfolioPerformanceStore := portfolio.NewPerformanceStore(pool)
	portfolioEngine := portfolio.NewEngine(portfolioStrategyStore, portfolioAllocationStore, portfolioPerformanceStore, auditor, logger)
	portfolioEngine.WithPressure(portfolioFinancialPressureAdapter{fp: financialPressureAdapter})
	portfolioEngine.WithCapacity(portfolioCapacityAdapter{ca: capacityGraphAdapter})
	portfolioGraphAdapter := portfolio.NewGraphAdapter(portfolioEngine, logger)
	graphAdapter.WithPortfolio(portfolioGraphAdapter)
	logger.Info("strategic revenue portfolio initialised")

	// Negotiation / pricing intelligence (Iteration 47).
	// Creates the pricing pipeline: stores + engine + adapter.
	// Integrates with financial pressure + capacity for informed floor pricing.
	// Fail-open: if DB is unavailable, pricing engine starts but queries fail gracefully.
	pricingProfileStore := pricing.NewProfileStore(pool)
	pricingNegotiationStore := pricing.NewNegotiationStore(pool)
	pricingOutcomeStore := pricing.NewOutcomeStore(pool)
	pricingPerformanceStore := pricing.NewPerformanceStore(pool)
	pricingEngine := pricing.NewEngine(pricingProfileStore, pricingNegotiationStore, pricingOutcomeStore, pricingPerformanceStore, auditor, logger)
	pricingEngine.WithPressure(pricingFinancialPressureAdapter{fp: financialPressureAdapter})
	pricingEngine.WithCapacity(pricingCapacityAdapter{ca: capacityGraphAdapter})
	pricingGraphAdapter := pricing.NewGraphAdapter(pricingEngine, logger)
	logger.Info("pricing intelligence initialised")

	// Autonomous scheduling & calendar control (Iteration 48).
	// Creates the scheduling pipeline: stores + slot generation config + engine + adapter.
	// Integrates with capacity (owner load) and portfolio (strategy priority).
	// Fail-open: if no calendar connector is set, recommendations still work.
	schedSlotStore := scheduling.NewSlotStore(pool)
	schedCandidateStore := scheduling.NewCandidateStore(pool)
	schedDecisionStore := scheduling.NewDecisionStore(pool)
	schedCalendarStore := scheduling.NewCalendarStore(pool)
	schedFamilyCfg := scheduling.SlotGenerationConfig{
		MaxDailyWorkHours:  familyCfg.MaxDailyWorkHours,
		MinFamilyTimeHours: familyCfg.MinFamilyTimeHours,
		WorkingWindows:     familyCfg.WorkingWindows,
		DaysAhead:          1,
	}
	for _, br := range familyCfg.BlockedRanges {
		schedFamilyCfg.BlockedRanges = append(schedFamilyCfg.BlockedRanges, scheduling.BlockedRange{
			Reason: br.Reason,
			Range:  br.Range,
		})
	}
	schedEngine := scheduling.NewEngine(
		schedSlotStore, schedCandidateStore, schedDecisionStore, schedCalendarStore,
		schedFamilyCfg, auditor, logger,
	)
	schedEngine.WithCapacity(schedulingCapacityAdapter{ca: capacityGraphAdapter})
	schedEngine.WithPortfolio(schedulingPortfolioAdapter{pa: portfolioGraphAdapter})
	schedAdapter := scheduling.NewGraphAdapter(schedEngine, logger)
	logger.Info("autonomous scheduling layer initialised",
		zap.Float64("max_daily_hours", schedFamilyCfg.MaxDailyWorkHours),
		zap.Int("blocked_ranges", len(schedFamilyCfg.BlockedRanges)),
	)

	// Meta-reflection & meta-learning layer (Iteration 49).
	metaReportStore := reflection.NewReportStore(pool)
	metaAggregator := reflection.NewAggregator(logger).
		WithIncome(reflectionIncomeAdapter{ie: incomeEngine}).
		WithFinancialTruth(reflectionTruthAdapter{ft: financialTruthEngine}).
		WithSignals(reflectionSignalAdapter{se: signalEngine}).
		WithCapacity(reflectionCapacityAdapter{ca: capacityGraphAdapter}).
		WithExternalActions(reflectionExtActAdapter{ea: extActAdapter})
	metaTrigger := reflection.NewTrigger(reflection.DefaultTriggerConfig())
	metaReflectionEngine := reflection.NewMetaEngine(metaAggregator, metaTrigger, metaReportStore, auditor, logger)
	metaAdapter := reflection.NewMetaGraphAdapter(metaReflectionEngine, logger)
	logger.Info("meta-reflection layer initialised")

	// Global objective function + risk model (Iteration 50).
	// Creates the objective pipeline: stores + engine + adapter.
	// Integrates with truth, pressure, capacity, portfolio, income, pricing, external actions.
	// Fail-open: if any provider is unavailable, its inputs default to zero.
	objStateStore := objective.NewObjectiveStateStore(pool)
	objRiskStore := objective.NewRiskStateStore(pool)
	objSummaryStore := objective.NewSummaryStore(pool)
	objectiveEngine := objective.NewEngine(objStateStore, objRiskStore, objSummaryStore, auditor, logger)
	objectiveEngine.WithTruth(objectiveTruthAdapter{ft: financialTruthEngine})
	objectiveEngine.WithPressure(objectivePressureAdapter{fp: financialPressureAdapter})
	objectiveEngine.WithCapacity(objectiveCapacityAdapter{ca: capacityGraphAdapter})
	objectiveEngine.WithPortfolio(objectivePortfolioAdapter{pa: portfolioGraphAdapter})
	objectiveEngine.WithIncome(objectiveIncomeAdapter{ie: incomeEngine})
	objectiveEngine.WithPricing(objectivePricingAdapter{pa: pricingGraphAdapter})
	objectiveEngine.WithExternalActions(objectiveExtActAdapter{ea: extActAdapter})
	objectiveGraphAdapter := objective.NewGraphAdapter(objectiveEngine, logger)
	graphAdapter.WithObjectiveFunction(objectiveFunctionBridge{oa: objectiveGraphAdapter})
	logger.Info("global objective function initialised")

	// Closed feedback actuation (Iteration 51).
	// Creates the actuation pipeline: store + engine + adapter.
	// Consumes reflection signals (Iteration 49) and objective state (Iteration 50).
	// Produces proposed corrective actions routed to existing subsystems.
	// Fail-open: if providers are unavailable, no actions are proposed.
	actuationStore := actuation.NewDecisionStore(pool)
	actuationEngine := actuation.NewEngine(actuationStore, auditor, logger)
	actuationEngine.WithReflection(actuationReflectionAdapter{ma: metaAdapter})
	actuationEngine.WithObjective(actuationObjectiveAdapter{oa: objectiveGraphAdapter})
	actuationGraphAdapter := actuation.NewGraphAdapter(actuationEngine, logger)
	logger.Info("closed feedback actuation initialised")

	// Autonomy runtime orchestrator (Iteration 52).
	// Loads autonomy config, creates orchestrator, wires all subsystem providers.
	// Orchestrator runs recurring cycles: reflection, objective, actuation,
	// scheduling, portfolio, discovery, self-extension, reporting.
	// Safety kernel enforces downgrade on threshold breach.
	// All providers are optional and fail-open.
	autonomyConfigPath := "configs/autonomy.yaml"
	autonomyCfg, autonomyCfgErr := autonomy.LoadAutonomyConfig(autonomyConfigPath)
	var autonomyAdapter *autonomy.APIAdapter
	if autonomyCfgErr != nil {
		logger.Warn("autonomy config not loaded, orchestrator disabled",
			zap.String("path", autonomyConfigPath),
			zap.Error(autonomyCfgErr),
		)
	} else {
		autonomyOrch := autonomy.NewOrchestrator(autonomyCfg, auditor, logger).
			WithReflection(autonomyReflectionBridge{me: metaReflectionEngine}).
			WithObjective(autonomyObjectiveBridge{oe: objectiveEngine}).
			WithActuation(autonomyActuationBridge{ae: actuationEngine}).
			WithScheduling(autonomySchedulingBridge{se: schedEngine}).
			WithPortfolio(autonomyPortfolioBridge{pe: portfolioEngine}).
			WithDiscovery(autonomyDiscoveryBridge{de: discoveryEngine}).
			WithSelfExtension(autonomySelfExtBridge{se: selfExtEngine}).
			WithPressure(autonomyPressureBridge{fp: financialPressureAdapter}).
			WithCapacity(autonomyCapacityBridge{ca: capacityGraphAdapter}).
			WithGovernance(autonomyGovernanceBridge{gc: govController})
		autonomyAdapter = autonomy.NewAPIAdapter(autonomyOrch, autonomyConfigPath)
		logger.Info("autonomy orchestrator initialised",
			zap.String("mode", string(autonomyCfg.Mode)),
			zap.Bool("scheduler_enabled", autonomyCfg.Scheduler.Enabled),
		)
	}

	adaptivePlanner.WithStrategy(graphAdapter)

	// Strategy learning layer (Iteration 18).
	strategyLearningMemory := strategylearning.NewMemoryStore(pool)
	strategyLearningAdapter := strategylearning.NewPlannerAdapter(strategyLearningMemory)
	adaptivePlanner.WithStrategyLearning(strategyLearningAdapter)

	handlers := api.NewHandlers(pool, publisher, m, logger).
		WithGoalEngine(goalEngine).
		WithActionEngine(actionEngine).
		WithOutcomeStore(outcomeStore).
		WithActionMemoryStore(memoryStore).
		WithAdaptivePlanner(adaptivePlanner).
		WithDecisionJournal(decisionJournal).
		WithReflectionEngine(reflectionEngine, reflectionFindingStore).
		WithScheduler(agentScheduler, cfg.Scheduler.Enabled).
		WithStabilityEngine(stabilityEngine).
		WithPolicyEngine(policyEngine).
		WithCausalEngine(causalEngine).
		WithExplorationEngine(explorationEngine).
		WithStrategyEngine(strategyEngine).
		WithStrategyLearning(strategyLearningMemory).
		WithDecisionGraph(graphAdapter).
		WithPathLearning(pathMemStore, pathTransStore).
		WithPathComparison(compSnapshotStore, compOutcomeStore, compMemoryStore).
		WithCounterfactual(cfSimStore, cfOutcomeStore, cfMemoryStore).
		WithMetaReasoning(metaEngine).
		WithCalibration(calibrator, calibrationTracker).
		WithContextCalibration(contextCalStore).
		WithModeCalibration(modeCalibrator, modeCalTracker).
		WithResourceOptimization(resourceAdapter).
		WithGovernance(govController, govReplayBuilder).
		WithProviderRouting(providerRoutingAdapter).
		WithProviderCatalog(catalogRegistry).
		WithIncomeEngine(incomeEngine).
		WithSignalEngine(signalEngine).
		WithFinancialPressure(financialPressureAdapter).
		WithDiscoveryEngine(discoveryEngine).
		WithCapacity(capacityGraphAdapter).
		WithFinancialTruth(financialTruthEngine).
		WithSelfExtension(selfExtAdapter).
		WithExternalActions(extActAdapter).
		WithPortfolio(portfolioGraphAdapter).
		WithPricing(pricingGraphAdapter).
		WithScheduling(schedAdapter).
		WithMetaReflection(metaAdapter, metaReportStore).
		WithObjective(objectiveGraphAdapter).
		WithActuation(actuationGraphAdapter).
		WithAutonomy(autonomyAdapter)
	router := api.NewRouter(handlers, registry, readiness, cfg.Auth.AdminToken, logger)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	logger.Info("starting api-gateway", zap.Int("port", cfg.HTTP.Port))

	// Start scheduler if enabled in config.
	if cfg.Scheduler.Enabled {
		agentScheduler.Start()
		logger.Info("agent scheduler started",
			zap.Int("interval_seconds", cfg.Scheduler.IntervalSeconds),
			zap.Int("timeout_seconds", cfg.Scheduler.TimeoutSeconds),
		)
	}

	// Start autonomy orchestrator if configured.
	if autonomyAdapter != nil && autonomyCfg != nil && autonomyCfg.Scheduler.Enabled {
		startCtx := context.Background()
		if err := autonomyAdapter.Start(startCtx); err != nil {
			logger.Error("failed to start autonomy orchestrator", zap.Error(err))
		} else {
			logger.Info("autonomy orchestrator started",
				zap.String("mode", string(autonomyCfg.Mode)),
			)
		}
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down api-gateway")

	// Stop autonomy orchestrator before scheduler.
	if autonomyAdapter != nil {
		autonomyAdapter.Stop(context.Background())
	}

	// Stop scheduler before HTTP server shutdown.
	agentScheduler.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}

	logger.Info("api-gateway stopped")
	return nil
}

// --- Portfolio bridge adapters (Iteration 46) ---
// These thin adapters bridge existing adapters to the portfolio.FinancialPressureProvider
// and portfolio.CapacityProvider interfaces without introducing import cycles.

type portfolioFinancialPressureAdapter struct {
	fp *financialpressure.GraphAdapter
}

func (a portfolioFinancialPressureAdapter) GetPressure(ctx context.Context) (float64, string) {
	if a.fp == nil {
		return 0, "low"
	}
	return a.fp.GetPressure(ctx)
}

type portfolioCapacityAdapter struct {
	ca *capacity.GraphAdapter
}

func (a portfolioCapacityAdapter) GetAvailableHoursWeek(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.AvailableHoursWeek
}

// --- Scheduling bridge adapters (Iteration 48) ---
// Bridge capacity and portfolio adapters to scheduling engine interfaces.

type schedulingCapacityAdapter struct {
	ca *capacity.GraphAdapter
}

func (a schedulingCapacityAdapter) GetAvailableHoursToday(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.AvailableHoursToday
}

func (a schedulingCapacityAdapter) GetOwnerLoadScore(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.OwnerLoadScore
}

type schedulingPortfolioAdapter struct {
	pa *portfolio.GraphAdapter
}

func (a schedulingPortfolioAdapter) GetStrategyPriority(ctx context.Context, itemType string) float64 {
	if a.pa == nil {
		return 0
	}
	boost := a.pa.GetStrategyBoost(ctx, itemType)
	return boost
}

// --- Pricing bridge adapters (Iteration 47) ---
// These thin adapters bridge existing adapters to the pricing.FinancialPressureProvider
// and pricing.CapacityProvider interfaces without introducing import cycles.

type pricingFinancialPressureAdapter struct {
	fp *financialpressure.GraphAdapter
}

func (a pricingFinancialPressureAdapter) GetPressure(ctx context.Context) (float64, string) {
	if a.fp == nil {
		return 0, "low"
	}
	return a.fp.GetPressure(ctx)
}

type pricingCapacityAdapter struct {
	ca *capacity.GraphAdapter
}

func (a pricingCapacityAdapter) GetCapacityPenalty(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	return a.ca.GetCapacityPenalty(ctx)
}

// --- Meta-reflection bridge adapters (Iteration 49) ---
// These thin adapters bridge existing engines/adapters to reflection.* interfaces
// to avoid import cycles.

type reflectionIncomeAdapter struct {
	ie *income.Engine
}

func (a reflectionIncomeAdapter) GetPerformanceStats(ctx context.Context) (totalOutcomes int, successRate, avgAccuracy, estimatedIncome float64) {
	if a.ie == nil {
		return 0, 0, 0, 0
	}
	perf := a.ie.GetPerformance(ctx)
	sig := a.ie.GetSignal(ctx)
	return perf.TotalOutcomes, perf.OverallSuccessRate, perf.OverallAccuracy, float64(sig.OpenOpportunities) * sig.BestOpenScore
}

func (a reflectionIncomeAdapter) GetOpportunityCount(ctx context.Context) int {
	if a.ie == nil {
		return 0
	}
	sig := a.ie.GetSignal(ctx)
	return sig.OpenOpportunities
}

type reflectionTruthAdapter struct {
	ft *financialtruth.Engine
}

func (a reflectionTruthAdapter) GetVerifiedIncome(ctx context.Context) float64 {
	if a.ft == nil {
		return 0
	}
	ts := a.ft.GetTruthSignal(ctx)
	return ts.VerifiedMonthlyIncome
}

type reflectionSignalAdapter struct {
	se *signals.Engine
}

func (a reflectionSignalAdapter) GetDerivedState(ctx context.Context) map[string]float64 {
	if a.se == nil {
		return nil
	}
	active := a.se.GetActiveSignals(ctx)
	return active.Derived
}

type reflectionCapacityAdapter struct {
	ca *capacity.GraphAdapter
}

func (a reflectionCapacityAdapter) GetOwnerLoadScore(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.OwnerLoadScore
}

func (a reflectionCapacityAdapter) GetAvailableHoursToday(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.AvailableHoursToday
}

type reflectionExtActAdapter struct {
	ea *externalactions.GraphAdapter
}

func (a reflectionExtActAdapter) GetRecentActionCounts(ctx context.Context, since time.Time) map[string]int {
	if a.ea == nil {
		return nil
	}
	actions, err := a.ea.ListActions(ctx, 200)
	if err != nil {
		return nil
	}
	counts := make(map[string]int)
	for _, act := range actions {
		if act.CreatedAt.After(since) {
			counts[act.ActionType]++
		}
	}
	return counts
}

// --- Objective function bridge adapters (Iteration 50) ---
// These thin adapters bridge existing adapters to the objective.* interfaces
// to avoid import cycles.

type objectiveTruthAdapter struct {
	ft *financialtruth.Engine
}

func (a objectiveTruthAdapter) GetVerifiedIncome(ctx context.Context) float64 {
	if a.ft == nil {
		return 0
	}
	sig := a.ft.GetTruthSignal(ctx)
	return sig.VerifiedMonthlyIncome
}

func (a objectiveTruthAdapter) GetTargetIncome(ctx context.Context) float64 {
	if a.ft == nil {
		return 0
	}
	// Target income comes from financial pressure state.
	return 0 // will be supplemented by pressure adapter below
}

type objectivePressureAdapter struct {
	fp *financialpressure.GraphAdapter
}

func (a objectivePressureAdapter) GetPressure(ctx context.Context) (float64, string) {
	if a.fp == nil {
		return 0, "low"
	}
	return a.fp.GetPressure(ctx)
}

type objectiveCapacityAdapter struct {
	ca *capacity.GraphAdapter
}

func (a objectiveCapacityAdapter) GetOwnerLoadScore(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.OwnerLoadScore
}

func (a objectiveCapacityAdapter) GetAvailableHoursToday(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.AvailableHoursToday
}

func (a objectiveCapacityAdapter) GetAvailableHoursWeek(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.AvailableHoursWeek
}

func (a objectiveCapacityAdapter) GetMaxDailyWorkHours(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.MaxDailyWorkHours
}

func (a objectiveCapacityAdapter) GetBlockedHoursToday(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.BlockedHoursToday
}

func (a objectiveCapacityAdapter) GetMinFamilyTimeHours(ctx context.Context) float64 {
	if a.ca == nil {
		return 0
	}
	state, err := a.ca.GetCapacityState(ctx)
	if err != nil {
		return 0
	}
	return state.MinFamilyTimeHours
}

type objectivePortfolioAdapter struct {
	pa *portfolio.GraphAdapter
}

func (a objectivePortfolioAdapter) GetDiversificationIndex(ctx context.Context) float64 {
	if a.pa == nil {
		return 0
	}
	p := a.pa.GetPortfolio(ctx)
	return p.DiversificationIdx
}

func (a objectivePortfolioAdapter) GetDominantAllocation(ctx context.Context) float64 {
	if a.pa == nil {
		return 0
	}
	p := a.pa.GetPortfolio(ctx)
	if p.TotalAllocatedHrs <= 0 {
		return 0
	}
	// Find the largest allocation fraction.
	maxFrac := 0.0
	for _, e := range p.Entries {
		if e.Allocation != nil && p.TotalAllocatedHrs > 0 {
			frac := e.Allocation.AllocatedHours / p.TotalAllocatedHrs
			if frac > maxFrac {
				maxFrac = frac
			}
		}
	}
	return maxFrac
}

func (a objectivePortfolioAdapter) GetActiveStrategyCount(ctx context.Context) int {
	if a.pa == nil {
		return 0
	}
	p := a.pa.GetPortfolio(ctx)
	return p.Summary.TotalActiveStrategies
}

func (a objectivePortfolioAdapter) GetPortfolioROI(ctx context.Context) float64 {
	if a.pa == nil {
		return 0
	}
	p := a.pa.GetPortfolio(ctx)
	return p.PortfolioROI
}

type objectiveIncomeAdapter struct {
	ie *income.Engine
}

func (a objectiveIncomeAdapter) GetBestOpenScore(ctx context.Context) float64 {
	if a.ie == nil {
		return 0
	}
	sig := a.ie.GetSignal(ctx)
	return sig.BestOpenScore
}

func (a objectiveIncomeAdapter) GetOpenOpportunityCount(ctx context.Context) int {
	if a.ie == nil {
		return 0
	}
	sig := a.ie.GetSignal(ctx)
	return sig.OpenOpportunities
}

type objectivePricingAdapter struct {
	pa *pricing.GraphAdapter
}

func (a objectivePricingAdapter) GetPricingConfidence(ctx context.Context) float64 {
	if a.pa == nil {
		return 0
	}
	perfs, err := a.pa.ListPerformance(ctx)
	if err != nil || len(perfs) == 0 {
		return 0
	}
	// Average win rate as proxy for confidence.
	total := 0.0
	for _, p := range perfs {
		total += p.WinRate
	}
	return total / float64(len(perfs))
}

func (a objectivePricingAdapter) GetWinRate(ctx context.Context) float64 {
	if a.pa == nil {
		return 0
	}
	perfs, err := a.pa.ListPerformance(ctx)
	if err != nil || len(perfs) == 0 {
		return 0
	}
	total := 0.0
	for _, p := range perfs {
		total += p.WinRate
	}
	return total / float64(len(perfs))
}

type objectiveExtActAdapter struct {
	ea *externalactions.GraphAdapter
}

func (a objectiveExtActAdapter) GetActionCounts(ctx context.Context) (failed, pending, total int) {
	if a.ea == nil {
		return 0, 0, 0
	}
	actions, err := a.ea.ListActions(ctx, 500)
	if err != nil {
		return 0, 0, 0
	}
	for _, act := range actions {
		total++
		switch act.Status {
		case "failed":
			failed++
		case "created", "review_required", "ready":
			pending++
		}
	}
	return failed, pending, total
}

// objectiveFunctionBridge bridges the objective.GraphAdapter to the
// decision_graph.ObjectiveFunctionProvider interface.
type objectiveFunctionBridge struct {
	oa *objective.GraphAdapter
}

func (b objectiveFunctionBridge) GetObjectiveSignal(ctx context.Context) decision_graph.ObjectiveSignalExport {
	if b.oa == nil {
		return decision_graph.ObjectiveSignalExport{}
	}
	sig := b.oa.GetObjectiveSignal(ctx)
	return decision_graph.ObjectiveSignalExport{
		SignalType:  sig.SignalType,
		Strength:    sig.Strength,
		Explanation: sig.Explanation,
		ContextTags: sig.ContextTags,
	}
}

// --- Actuation bridge adapters (Iteration 51) ---
// These thin adapters bridge existing adapters to the actuation.* interfaces
// to avoid import cycles.

type actuationReflectionAdapter struct {
	ma *reflection.MetaGraphAdapter
}

func (a actuationReflectionAdapter) GetReflectionSignals(ctx context.Context) ([]actuation.ReflectionSignalInput, error) {
	if a.ma == nil {
		return nil, nil
	}
	signals, err := a.ma.GetReflectionSignals(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]actuation.ReflectionSignalInput, len(signals))
	for i, s := range signals {
		result[i] = actuation.ReflectionSignalInput{
			SignalType: string(s.SignalType),
			Strength:   s.Strength,
		}
	}
	return result, nil
}

type actuationObjectiveAdapter struct {
	oa *objective.GraphAdapter
}

func (a actuationObjectiveAdapter) GetNetUtility(ctx context.Context) float64 {
	if a.oa == nil {
		return 0
	}
	summary, err := a.oa.GetSummary(ctx)
	if err != nil {
		return 0
	}
	return summary.NetUtility
}

func (a actuationObjectiveAdapter) GetUtilityScore(ctx context.Context) float64 {
	if a.oa == nil {
		return 0
	}
	summary, err := a.oa.GetSummary(ctx)
	if err != nil {
		return 0
	}
	return summary.UtilityScore
}

func (a actuationObjectiveAdapter) GetRiskScore(ctx context.Context) float64 {
	if a.oa == nil {
		return 0
	}
	summary, err := a.oa.GetSummary(ctx)
	if err != nil {
		return 0
	}
	return summary.RiskScore
}

func (a actuationObjectiveAdapter) GetFinancialRisk(ctx context.Context) float64 {
	if a.oa == nil {
		return 0
	}
	risk, err := a.oa.GetRiskState(ctx)
	if err != nil {
		return 0
	}
	return risk.FinancialInstabilityRisk
}

func (a actuationObjectiveAdapter) GetOverloadRisk(ctx context.Context) float64 {
	if a.oa == nil {
		return 0
	}
	risk, err := a.oa.GetRiskState(ctx)
	if err != nil {
		return 0
	}
	return risk.OverloadRisk
}
