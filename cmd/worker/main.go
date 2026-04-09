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

	"github.com/tiroq/arcanum/internal/agent/core"
	"github.com/tiroq/arcanum/internal/agent/eventstore"
	agentmemory "github.com/tiroq/arcanum/internal/agent/memory"
	"github.com/tiroq/arcanum/internal/agent/provider_catalog"
	agentstate "github.com/tiroq/arcanum/internal/agent/state"
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

	baseAuditor := audit.NewPostgresAuditRecorder(pool)
	agentCore := core.New(
		baseAuditor,
		eventstore.New(pool),
		agentstate.New(pool),
		agentmemory.New(pool),
		logger,
	)
	queue := jobs.NewQueue(pool, logger).WithAudit(agentCore)
	templateLoader := prompts.NewTemplateLoader("prompts")

	// --- Provider setup with execution profiles ---

	ollamaCfg := cfg.Providers.Ollama
	ollamaBase := providers.NewOllamaProvider("ollama", ollamaCfg, logger)

	// Load execution profiles from the provider catalog.
	// providers/ollama.yaml must have an execution_profiles section defining per-role
	// model candidate chains. Fails explicitly if the section is missing or incomplete.
	catalogLocalCandidates, err := provider_catalog.LoadExecutionProfiles(
		cfg.Providers.CatalogDir, "ollama", logger)
	if err != nil {
		return fmt.Errorf("load execution profiles: %w", err)
	}

	// Build RoleProfiles directly from catalog. Provider routing (cloud/openrouter escalation)
	// is handled at the planning layer via the provider router — not at the worker level.
	// The worker executes whatever model the plan specifies; it does not escalate on its own.
	workerProfiles := profile.RoleProfiles(catalogLocalCandidates)

	logger.Info("worker execution profiles loaded from catalog",
		zap.Int("roles", len(workerProfiles)),
	)

	// rawProviders holds undecorated backend implementations for per-candidate
	// provider resolution inside the execution engine. Separate from providerReg
	// (which wraps backends with auditing and execution layers). Register additional
	// backends here (e.g. openrouter, ollama-cloud) to make them available as
	// fallback targets in profile DSLs: "model?provider=openrouter&timeout=60".
	rawProviders := providers.NewProviderRegistry()
	rawProviders.Register("ollama", ollamaBase)

	// Register Ollama Cloud if explicitly enabled. When disabled, candidates
	// with provider=ollama-cloud fall back to primary with a logged warning.
	if cloudCfg := cfg.Providers.OllamaCloud; cloudCfg.Enabled {
		ollamaCloud := providers.NewOllamaCloudProvider("ollama-cloud", cloudCfg, logger)
		rawProviders.Register("ollama-cloud", ollamaCloud)
		logger.Info("registered ollama-cloud provider",
			zap.String("base_url", cloudCfg.BaseURL),
			zap.Bool("has_api_key", cloudCfg.APIKey != ""),
		)
	} else {
		logger.Debug("ollama-cloud provider disabled (set OLLAMA_CLOUD_ENABLED=true to enable)")
	}

	// Register OpenRouter if explicitly enabled. When disabled, candidates
	// with provider=openrouter fall back to primary with a logged warning.
	if orCfg := cfg.Providers.OpenRouter; orCfg.Enabled {
		openRouter := providers.NewOpenRouterProvider("openrouter", orCfg, logger)
		rawProviders.Register("openrouter", openRouter)
		logger.Info("registered openrouter provider",
			zap.String("base_url", orCfg.BaseURL),
			zap.Bool("has_api_key", orCfg.APIKey != ""),
		)
	} else {
		logger.Debug("openrouter provider disabled (set OPENROUTER_ENABLED=true to enable)")
	}

	execProvider := execution.NewExecutingProviderWithRegistry(ollamaBase, rawProviders, workerProfiles, m, logger)

	providerReg := providers.NewProviderRegistry()
	providerReg.Register("ollama", providers.NewAuditedProvider(execProvider, agentCore, logger))
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

	w := worker.New(queue, procRegistry, publisher, pool, agentCore, m, logger, workerID)

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
