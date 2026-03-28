package api

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/health"
)

// NewRouter builds the HTTP router with all routes registered.
func NewRouter(handlers *Handlers, registry *prometheus.Registry, rc *health.ReadinessChecker, adminToken string, logger *zap.Logger) http.Handler {
	mux := http.NewServeMux()

	// Infrastructure routes (no auth required)
	mux.HandleFunc("/health", health.HealthHandler)
	mux.HandleFunc("/ready", rc.ReadinessHandler)
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	// Admin-protected API routes
	auth := authMiddleware(adminToken)
	log := loggingMiddleware(logger)
	rec := recoveryMiddleware
	chain := func(h http.HandlerFunc) http.Handler {
		return rec(log(auth(h)))
	}

	// Source connections
	mux.Handle("/api/v1/source-connections", chain(handlers.SourceConnections))
	mux.Handle("/api/v1/source-connections/", chain(handlers.SourceConnectionByID))

	// Source tasks
	mux.Handle("/api/v1/source-tasks", chain(handlers.SourceTasks))
	mux.Handle("/api/v1/source-tasks/", chain(handlers.SourceTaskRouter))

	// Jobs
	mux.Handle("/api/v1/jobs", chain(handlers.Jobs))
	mux.Handle("/api/v1/jobs/", chain(handlers.JobRouter))

	// Proposals
	mux.Handle("/api/v1/proposals", chain(handlers.Proposals))
	mux.Handle("/api/v1/proposals/", chain(handlers.ProposalRouter))

	// Processor runs
	mux.Handle("/api/v1/processor-runs", chain(handlers.ProcessorRuns))
	mux.Handle("/api/v1/processor-runs/", chain(handlers.ProcessorRunByID))

	// Audit events
	mux.Handle("/api/v1/audit-events", chain(handlers.AuditEvents))

	// Metrics summary
	mux.Handle("/api/v1/metrics/summary", chain(handlers.MetricsSummary))

	return requestIDMiddleware(mux)
}
