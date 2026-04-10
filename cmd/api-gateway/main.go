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
	"github.com/tiroq/arcanum/internal/agent/calibration"
	"github.com/tiroq/arcanum/internal/agent/causal"
	"github.com/tiroq/arcanum/internal/agent/counterfactual"
	decision_graph "github.com/tiroq/arcanum/internal/agent/decision_graph"
	"github.com/tiroq/arcanum/internal/agent/exploration"
	"github.com/tiroq/arcanum/internal/agent/goals"
	"github.com/tiroq/arcanum/internal/agent/governance"
	"github.com/tiroq/arcanum/internal/agent/income"
	meta_reasoning "github.com/tiroq/arcanum/internal/agent/meta_reasoning"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	pathcomparison "github.com/tiroq/arcanum/internal/agent/path_comparison"
	pathlearning "github.com/tiroq/arcanum/internal/agent/path_learning"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/agent/policy"
	providercatalog "github.com/tiroq/arcanum/internal/agent/provider_catalog"
	providerrouting "github.com/tiroq/arcanum/internal/agent/provider_routing"
	"github.com/tiroq/arcanum/internal/agent/reflection"
	resourceopt "github.com/tiroq/arcanum/internal/agent/resource_optimization"
	"github.com/tiroq/arcanum/internal/agent/scheduler"
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
	incomeEngine := income.NewEngine(incomeOppStore, incomePropStore, incomeOutcomeStore, auditor, logger)
	if govAdapter != nil {
		incomeEngine.WithGovernance(govAdapter)
	}
	incomeGraphAdapter := income.NewGraphAdapter(incomeEngine, logger)
	graphAdapter.WithIncomeSignals(incomeGraphAdapter)
	logger.Info("income engine initialised")

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
		WithIncomeEngine(incomeEngine)
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

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down api-gateway")

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
