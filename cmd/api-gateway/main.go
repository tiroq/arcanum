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
	"github.com/tiroq/arcanum/internal/api"
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
	handlers := api.NewHandlers(pool, publisher, m, logger)
	router := api.NewRouter(handlers, registry, readiness, cfg.Auth.AdminToken, logger)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	logger.Info("starting api-gateway", zap.Int("port", cfg.HTTP.Port))

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down api-gateway")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}

	logger.Info("api-gateway stopped")
	return nil
}
