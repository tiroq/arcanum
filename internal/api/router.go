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

	// Agent timeline
	mux.Handle("/api/v1/agent/timeline/", chain(handlers.AgentTimeline))

	// Agent goals (advisory, read-only)
	mux.Handle("/api/v1/agent/goals", chain(handlers.AgentGoals))

	// Agent actions
	mux.Handle("/api/v1/agent/actions", chain(handlers.AgentActions))
	mux.Handle("/api/v1/agent/run-actions", chain(handlers.RunActions))

	// Agent outcomes (read-only verification layer)
	mux.Handle("/api/v1/agent/outcomes", chain(handlers.AgentOutcomes))

	// Agent action memory (read-only learning layer)
	mux.Handle("/api/v1/agent/action-memory", chain(handlers.AgentActionMemory))
	mux.Handle("/api/v1/agent/action-memory/context", chain(handlers.AgentContextMemory))
	mux.Handle("/api/v1/agent/action-memory/provider-context", chain(handlers.AgentProviderContextMemory))
	mux.Handle("/api/v1/agent/action-memory/weighted", chain(handlers.AgentWeightedMemory))
	mux.Handle("/api/v1/agent/action-memory/hierarchical", chain(handlers.AgentHierarchicalMemory))

	// Agent planning decisions (read-only adaptive planning layer)
	mux.Handle("/api/v1/agent/planning-decisions", chain(handlers.AgentPlanningDecisions))

	// Agent scheduler control
	mux.Handle("/api/v1/agent/scheduler/start", chain(handlers.SchedulerStart))
	mux.Handle("/api/v1/agent/scheduler/stop", chain(handlers.SchedulerStop))
	mux.Handle("/api/v1/agent/scheduler/status", chain(handlers.SchedulerStatus))

	// Agent reflection (self-analysis, read-only/advisory)
	mux.Handle("/api/v1/agent/reflect", chain(handlers.TriggerReflection))
	mux.Handle("/api/v1/agent/reflections", chain(handlers.ListReflections))

	// Agent decision journal (durable planning history)
	mux.Handle("/api/v1/agent/journal", chain(handlers.ListJournalDecisions))

	// Agent stability controls (self-stability layer)
	mux.Handle("/api/v1/agent/stability", chain(handlers.StabilityStatus))
	mux.Handle("/api/v1/agent/stability/reset", chain(handlers.StabilityReset))
	mux.Handle("/api/v1/agent/stability/evaluate", chain(handlers.StabilityEvaluate))

	// Agent policy adaptation
	mux.Handle("/api/v1/agent/policy", chain(handlers.PolicyState))
	mux.Handle("/api/v1/agent/policy/changes", chain(handlers.PolicyChanges))
	mux.Handle("/api/v1/agent/policy/evaluate", chain(handlers.PolicyEvaluate))

	// Agent causal reasoning
	mux.Handle("/api/v1/agent/causal", chain(handlers.CausalAttributions))
	mux.Handle("/api/v1/agent/causal/evaluate", chain(handlers.CausalEvaluate))
	mux.Handle("/api/v1/agent/causal/", chain(handlers.CausalBySubject))

	// Agent exploration (bounded exploration vs exploitation)
	mux.Handle("/api/v1/agent/exploration/status", chain(handlers.ExplorationStatus))
	mux.Handle("/api/v1/agent/exploration/history", chain(handlers.ExplorationHistory))

	// Agent strategy (bounded multi-step planning)
	mux.Handle("/api/v1/agent/strategy/status", chain(handlers.StrategyStatus))
	mux.Handle("/api/v1/agent/strategy/history", chain(handlers.StrategyHistory))
	mux.Handle("/api/v1/agent/strategy/plans", chain(handlers.StrategyPlans))

	// Agent strategy learning (strategy-level feedback + outcomes)
	mux.Handle("/api/v1/agent/strategy-memory", chain(handlers.StrategyMemoryList))
	mux.Handle("/api/v1/agent/strategy-outcomes", chain(handlers.StrategyOutcomesList))

	// Agent decision graph (graph-based decision evaluation)
	mux.Handle("/api/v1/agent/decision-graph/status", chain(handlers.DecisionGraphStatus))

	// Agent path learning (path memory + transition learning, Iteration 21)
	mux.Handle("/api/v1/agent/path-memory", chain(handlers.PathMemoryList))
	mux.Handle("/api/v1/agent/transition-memory", chain(handlers.TransitionMemoryList))
	mux.Handle("/api/v1/agent/path-outcomes", chain(handlers.PathOutcomesList))

	// Agent path comparison (comparative path selection learning, Iteration 22)
	mux.Handle("/api/v1/agent/path-snapshots", chain(handlers.PathSnapshotsList))
	mux.Handle("/api/v1/agent/path-comparative", chain(handlers.PathComparativeList))
	mux.Handle("/api/v1/agent/path-comparative-memory", chain(handlers.PathComparativeMemoryList))

	return requestIDMiddleware(mux)
}
