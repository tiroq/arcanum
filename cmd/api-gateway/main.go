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
	meta_reasoning "github.com/tiroq/arcanum/internal/agent/meta_reasoning"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	pathcomparison "github.com/tiroq/arcanum/internal/agent/path_comparison"
	pathlearning "github.com/tiroq/arcanum/internal/agent/path_learning"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/agent/policy"
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
		WithGovernance(govController, govReplayBuilder)
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
