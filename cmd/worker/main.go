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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
	"github.com/tiroq/arcanum/internal/config"
	"github.com/tiroq/arcanum/internal/db"
	"github.com/tiroq/arcanum/internal/health"
	"github.com/tiroq/arcanum/internal/jobs"
	"github.com/tiroq/arcanum/internal/logging"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/processors"
	"github.com/tiroq/arcanum/internal/prompts"
	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/execution"
	"github.com/tiroq/arcanum/internal/providers/profile"
	"github.com/tiroq/arcanum/internal/worker"
)

const (
	serviceName    = "worker"
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

	// --- Worker dependencies ---

	// Ensure JetStream RUNEFORGE stream exists (idempotent — safe to call on every startup).
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream context: %w", err)
	}
	if err := messaging.SetupStreams(js); err != nil {
		return fmt.Errorf("setup streams: %w", err)
	}
	logger.Info("jetstream streams configured")

	publisher, err := messaging.NewPublisher(nc, logger)
	if err != nil {
		return fmt.Errorf("create publisher: %w", err)
	}

	auditor := audit.NewPostgresAuditRecorder(pool)
	queue := jobs.NewQueue(pool, logger).WithAudit(auditor)
	templateLoader := prompts.NewTemplateLoader("prompts")

	// --- Provider setup with execution profiles ---

	ollamaCfg := cfg.Providers.Ollama
	ollamaBase := providers.NewOllamaProvider("ollama", ollamaCfg, logger)

	profiles, err := profile.ResolveFromConfig(
		ollamaCfg.DefaultModel,
		ollamaCfg.FastModel,
		ollamaCfg.PlannerModel,
		ollamaCfg.ReviewModel,
		ollamaCfg.DefaultProfile,
		ollamaCfg.FastProfile,
		ollamaCfg.PlannerProfile,
		ollamaCfg.ReviewProfile,
	)
	if err != nil {
		return fmt.Errorf("resolve execution profiles: %w", err)
	}

	// rawProviders holds undecorated backend implementations for per-candidate
	// provider resolution inside the execution engine. Separate from providerReg
	// (which wraps backends with auditing and execution layers). Register additional
	// backends here (e.g. openrouter, ollama-cloud) to make them available as
	// fallback targets in profile DSLs: "model?provider=openrouter&timeout=60".
	rawProviders := providers.NewProviderRegistry()
	rawProviders.Register("ollama", ollamaBase)

	execProvider := execution.NewExecutingProviderWithRegistry(ollamaBase, rawProviders, profiles, m, logger)

	providerReg := providers.NewProviderRegistry()
	providerReg.Register("ollama", providers.NewAuditedProvider(execProvider, auditor, logger))
	logger.Info("registered ollama provider with execution profiles")

	// --- Processor registry ---

	const defaultProviderName = "ollama"

	rewriteProc := processors.NewLLMRewriteProcessor(providerReg, templateLoader, logger, m, defaultProviderName)
	routingProc := processors.NewLLMRoutingProcessor(providerReg, templateLoader, logger, m, defaultProviderName)
	rulesProc := processors.NewRulesOnlyProcessor()
	compositeProc := processors.NewCompositeProcessor("composite", rewriteProc, routingProc)

	procRegistry := processors.NewRegistry()
	procRegistry.Register(rewriteProc)
	procRegistry.Register(routingProc)
	procRegistry.Register(rulesProc)
	procRegistry.Register(compositeProc)

	// --- Worker ---

	workerID, err := os.Hostname()
	if err != nil {
		workerID = fmt.Sprintf("worker-%d", os.Getpid())
	}

	w := worker.New(queue, procRegistry, publisher, pool, auditor, m, logger, workerID)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	if err := w.Start(workerCtx); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	logger.Info("worker started", zap.String("worker_id", workerID))

	// --- HTTP health/metrics server ---

	readiness := &health.ReadinessChecker{DB: pool, NATS: nc}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health.HealthHandler)
	mux.HandleFunc("/readyz", readiness.ReadinessHandler)
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:      mux,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	logger.Info("starting http server", zap.Int("port", cfg.HTTP.Port))

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down worker")

	// Stop worker first so in-flight jobs complete before connections close.
	w.Stop()
	workerCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}

	logger.Info("worker stopped")
	return nil
}
