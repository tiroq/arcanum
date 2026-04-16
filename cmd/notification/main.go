package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tiroq/arcanum/internal/config"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/db"
	"github.com/tiroq/arcanum/internal/health"
	"github.com/tiroq/arcanum/internal/logging"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/telegram"
	"go.uber.org/zap"
)

const (
	serviceName    = "notification"
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
	_ = m

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

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream context: %w", err)
	}
	if err := messaging.SetupStreams(js); err != nil {
		return fmt.Errorf("setup jetstream streams: %w", err)
	}
	logger.Info("jetstream streams initialized")

	// Initialize Telegram bot
	bot, err := telegram.New(cfg.Telegram.BotToken, cfg.Telegram.OwnerChatID, pool, logger)
	if err != nil {
		return fmt.Errorf("init telegram bot: %w", err)
	}

	// Wire API client for rich commands (goals, queue, focus, vector, etc.)
	apiClient := telegram.NewAPIClient(cfg.Telegram.APIGatewayURL, cfg.Auth.AdminToken)
	bot.SetAPIClient(apiClient)

	// Subscribe to NATS events and forward to Telegram
	sub, err := messaging.NewSubscriber(nc, logger)
	if err != nil {
		return fmt.Errorf("create subscriber: %w", err)
	}

	// Proposal created → notify owner
	if err := sub.Subscribe(subjects.SubjectProposalCreated, "notification-proposal-created", func(msg *natsgo.Msg) {
		var evt struct {
			ProposalID          string `json:"proposal_id"`
			SourceTaskID        string `json:"source_task_id"`
			ProposalType        string `json:"proposal_type"`
			HumanReviewRequired bool   `json:"human_review_required"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			logger.Error("unmarshal proposal event", zap.Error(err))
			msg.Nak() //nolint:errcheck
			return
		}
		if err := bot.SendProposalMessage(evt.ProposalID, evt.SourceTaskID, evt.ProposalType); err != nil {
			logger.Error("send proposal notification", zap.Error(err))
		}
		msg.Ack() //nolint:errcheck
	}); err != nil {
		return fmt.Errorf("subscribe proposal.created: %w", err)
	}

	// Job dead-lettered → alert owner
	if err := sub.Subscribe(subjects.SubjectJobDead, "notification-job-dead", func(msg *natsgo.Msg) {
		var evt struct {
			JobID  string `json:"job_id"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			logger.Error("unmarshal job dead event", zap.Error(err))
			msg.Nak() //nolint:errcheck
			return
		}
		text := telegram.FormatJobDead(evt.JobID, evt.Reason)
		if err := bot.SendMessage(text); err != nil {
			logger.Error("send job dead notification", zap.Error(err))
		}
		msg.Ack() //nolint:errcheck
	}); err != nil {
		return fmt.Errorf("subscribe job.dead: %w", err)
	}

	// Job retry → inform owner
	if err := sub.Subscribe(subjects.SubjectJobRetry, "notification-job-retry", func(msg *natsgo.Msg) {
		var evt struct {
			JobID        string `json:"job_id"`
			AttemptCount int    `json:"attempt_count"`
			Reason       string `json:"reason"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			logger.Error("unmarshal job retry event", zap.Error(err))
			msg.Nak() //nolint:errcheck
			return
		}
		text := telegram.FormatJobFailed(evt.JobID, evt.Reason, evt.AttemptCount)
		if err := bot.SendMessage(text); err != nil {
			logger.Error("send job retry notification", zap.Error(err))
		}
		msg.Ack() //nolint:errcheck
	}); err != nil {
		return fmt.Errorf("subscribe job.retry: %w", err)
	}

	// Control alert: lease expired (reclaimed)
	if err := sub.Subscribe(subjects.SubjectControlAlertLeaseExpired, "notification-control-lease-expired", func(msg *natsgo.Msg) {
		var evt struct {
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			logger.Error("unmarshal lease_expired alert", zap.Error(err))
			msg.Nak() //nolint:errcheck
			return
		}
		text := telegram.FormatControlAlertLeaseExpired(evt.Count)
		if err := bot.SendMessage(text); err != nil {
			logger.Error("send lease_expired notification", zap.Error(err))
		}
		msg.Ack() //nolint:errcheck
	}); err != nil {
		return fmt.Errorf("subscribe control.alert.lease_expired: %w", err)
	}

	// Control alert: retry overdue (requeued)
	if err := sub.Subscribe(subjects.SubjectControlAlertRetryOverdue, "notification-control-retry-overdue", func(msg *natsgo.Msg) {
		var evt struct {
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			logger.Error("unmarshal retry_overdue alert", zap.Error(err))
			msg.Nak() //nolint:errcheck
			return
		}
		text := telegram.FormatControlAlertRetryOverdue(evt.Count)
		if err := bot.SendMessage(text); err != nil {
			logger.Error("send retry_overdue notification", zap.Error(err))
		}
		msg.Ack() //nolint:errcheck
	}); err != nil {
		return fmt.Errorf("subscribe control.alert.retry_overdue: %w", err)
	}

	// Control alert: queue backlog — deduplicated: suppress repeats with same count within 5 minutes
	var (
		backlogMu         sync.Mutex
		lastBacklogCount  int64
		lastBacklogNotify time.Time
	)
	if err := sub.Subscribe(subjects.SubjectControlAlertQueueBacklog, "notification-control-queue-backlog", func(msg *natsgo.Msg) {
		var evt struct {
			Count     int64 `json:"count"`
			Threshold int64 `json:"threshold"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			logger.Error("unmarshal queue_backlog alert", zap.Error(err))
			msg.Nak() //nolint:errcheck
			return
		}
		backlogMu.Lock()
		suppressed := evt.Count == lastBacklogCount && time.Since(lastBacklogNotify) < 5*time.Minute
		if !suppressed {
			lastBacklogCount = evt.Count
			lastBacklogNotify = time.Now()
		}
		backlogMu.Unlock()
		if !suppressed {
			text := telegram.FormatControlAlertQueueBacklog(evt.Count, evt.Threshold)
			if err := bot.SendMessage(text); err != nil {
				logger.Error("send queue_backlog notification", zap.Error(err))
			}
		}
		msg.Ack() //nolint:errcheck
	}); err != nil {
		return fmt.Errorf("subscribe control.alert.queue_backlog: %w", err)
	}

	// Control alert: lease lost mid-execution
	if err := sub.Subscribe(subjects.SubjectControlAlertLeaseLost, "notification-control-lease-lost", func(msg *natsgo.Msg) {
		var evt struct {
			JobID    string `json:"job_id"`
			WorkerID string `json:"worker_id"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			logger.Error("unmarshal lease_lost alert", zap.Error(err))
			msg.Nak() //nolint:errcheck
			return
		}
		text := telegram.FormatControlAlertLeaseLost(evt.JobID, evt.WorkerID)
		if err := bot.SendMessage(text); err != nil {
			logger.Error("send lease_lost notification", zap.Error(err))
		}
		msg.Ack() //nolint:errcheck
	}); err != nil {
		return fmt.Errorf("subscribe control.alert.lease_lost: %w", err)
	}

	// Start Telegram polling for incoming commands
	botCtx, botCancel := context.WithCancel(context.Background())
	defer botCancel()
	bot.StartPolling(botCtx)

	// Send startup notification
	startupMsg := fmt.Sprintf("<b>🚀 Arcanum Online</b>\n\nNotification service started.\nAPI: %s\n\nUse /help for commands.\nUse /status for system state.", cfg.Telegram.APIGatewayURL)
	if err := bot.SendMessage(startupMsg); err != nil {
		logger.Warn("failed to send startup message", zap.Error(err))
	}

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

	logger.Info("starting notification", zap.Int("port", cfg.HTTP.Port))

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down notification")
	botCancel()
	bot.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}

	logger.Info("notification stopped")
	return nil
}
