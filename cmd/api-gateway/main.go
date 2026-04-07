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
	"github.com/tiroq/arcanum/internal/agent/causal"
	"github.com/tiroq/arcanum/internal/agent/exploration"
	"github.com/tiroq/arcanum/internal/agent/goals"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/agent/policy"
	"github.com/tiroq/arcanum/internal/agent/reflection"
	"github.com/tiroq/arcanum/internal/agent/scheduler"
	"github.com/tiroq/arcanum/internal/agent/stability"
	"github.com/tiroq/arcanum/internal/agent/strategy"
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
	adaptivePlanner.WithStrategy(strategyPlannerAdapter)

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
		WithStrategyEngine(strategyEngine)
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
