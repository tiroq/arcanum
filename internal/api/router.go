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

	// Agent counterfactual simulation (predictive intelligence, Iteration 23)
	mux.Handle("/api/v1/agent/counterfactual/predictions", chain(handlers.CounterfactualPredictionsList))
	mux.Handle("/api/v1/agent/counterfactual/memory", chain(handlers.CounterfactualMemoryList))
	mux.Handle("/api/v1/agent/counterfactual/errors", chain(handlers.CounterfactualErrorsList))

	// Agent meta-reasoning (mode selection + learning, Iteration 24)
	mux.Handle("/api/v1/agent/meta-reasoning/status", chain(handlers.MetaReasoningStatus))
	mux.Handle("/api/v1/agent/meta-reasoning/memory", chain(handlers.MetaReasoningMemory))
	mux.Handle("/api/v1/agent/meta-reasoning/history", chain(handlers.MetaReasoningHistory))

	// Agent calibration (self-calibration layer, Iteration 25)
	mux.Handle("/api/v1/agent/calibration/summary", chain(handlers.CalibrationSummary))
	mux.Handle("/api/v1/agent/calibration/buckets", chain(handlers.CalibrationBuckets))
	mux.Handle("/api/v1/agent/calibration/errors", chain(handlers.CalibrationErrors))

	// Agent contextual calibration (context-aware calibration, Iteration 26)
	mux.Handle("/api/v1/agent/calibration/context", chain(handlers.CalibrationContextList))

	// Agent mode-specific calibration (per-mode calibration, Iteration 28)
	mux.Handle("/api/v1/agent/calibration/mode-summary", chain(handlers.ModeCalibrationSummaryList))
	mux.Handle("/api/v1/agent/calibration/mode-buckets", chain(handlers.ModeCalibrationBucketsList))
	mux.Handle("/api/v1/agent/calibration/mode-records", chain(handlers.ModeCalibrationRecordsList))

	// Agent signal arbitration (Iteration 27)
	mux.Handle("/api/v1/agent/arbitration/trace", chain(handlers.ArbitrationTrace))

	// Agent resource optimization (cost/latency-aware decision, Iteration 29)
	mux.Handle("/api/v1/agent/resource/profiles", chain(handlers.ResourceProfiles))
	mux.Handle("/api/v1/agent/resource/summary", chain(handlers.ResourceSummary))
	mux.Handle("/api/v1/agent/resource/decisions", chain(handlers.ResourceDecisions))

	// Agent governance (human override + governance layer, Iteration 30)
	mux.Handle("/api/v1/agent/governance/state", chain(handlers.GovernanceState))
	mux.Handle("/api/v1/agent/governance/actions", chain(handlers.GovernanceActions))
	mux.Handle("/api/v1/agent/governance/freeze", chain(handlers.GovernanceFreeze))
	mux.Handle("/api/v1/agent/governance/unfreeze", chain(handlers.GovernanceUnfreeze))
	mux.Handle("/api/v1/agent/governance/force-mode", chain(handlers.GovernanceForceMode))
	mux.Handle("/api/v1/agent/governance/safe-hold", chain(handlers.GovernanceSafeHold))
	mux.Handle("/api/v1/agent/governance/rollback", chain(handlers.GovernanceRollback))
	mux.Handle("/api/v1/agent/governance/clear", chain(handlers.GovernanceClearOverride))
	mux.Handle("/api/v1/agent/governance/replay/", chain(handlers.GovernanceReplay))

	// Agent provider routing (quota-aware provider selection, Iteration 31)
	mux.Handle("/api/v1/agent/providers/status", chain(handlers.ProviderStatus))
	mux.Handle("/api/v1/agent/providers/usage", chain(handlers.ProviderUsage))
	mux.Handle("/api/v1/agent/providers/decisions", chain(handlers.ProviderDecisions))

	// Agent provider catalog + model-aware targets (Iteration 32)
	mux.Handle("/api/v1/agent/providers/catalog", chain(handlers.ProviderCatalog))
	mux.Handle("/api/v1/agent/providers/targets", chain(handlers.ProviderTargets))

	// Agent income engine (income pipeline, Iteration 36)
	mux.Handle("/api/v1/agent/income/opportunities", chain(handlers.IncomeOpportunities))
	mux.Handle("/api/v1/agent/income/evaluate", chain(handlers.IncomeEvaluate))
	mux.Handle("/api/v1/agent/income/proposals", chain(handlers.IncomeProposals))
	mux.Handle("/api/v1/agent/income/outcomes", chain(handlers.IncomeOutcomes))
	mux.Handle("/api/v1/agent/income/signal", chain(handlers.IncomeSignal))
	mux.Handle("/api/v1/agent/income/performance", chain(handlers.IncomePerformance))

	// Agent signal ingestion (perception layer, Iteration 37)
	mux.Handle("/api/v1/agent/signals/ingest", chain(handlers.SignalsIngest))
	mux.Handle("/api/v1/agent/signals", chain(handlers.SignalsList))
	mux.Handle("/api/v1/agent/signals/derived", chain(handlers.SignalsDerived))
	mux.Handle("/api/v1/agent/signals/recompute", chain(handlers.SignalsRecompute))

	// Agent financial pressure (Iteration 38)
	mux.Handle("/api/v1/agent/financial/state", chain(handlers.FinancialState))
	mux.Handle("/api/v1/agent/financial/pressure", chain(handlers.FinancialPressure))

	// Agent financial truth (Iteration 42)
	mux.Handle("/api/v1/agent/financial/events", chain(handlers.FinancialEvents))
	mux.Handle("/api/v1/agent/financial/facts", chain(handlers.FinancialFacts))
	mux.Handle("/api/v1/agent/financial/link", chain(handlers.FinancialLink))
	mux.Handle("/api/v1/agent/financial/truth/summary", chain(handlers.FinancialTruthSummary))
	mux.Handle("/api/v1/agent/financial/truth/recompute", chain(handlers.FinancialTruthRecompute))

	// Agent opportunity discovery (Iteration 40)
	mux.Handle("/api/v1/agent/income/discovery/candidates", chain(handlers.DiscoveryCandidates))
	mux.Handle("/api/v1/agent/income/discovery/run", chain(handlers.DiscoveryRun))
	mux.Handle("/api/v1/agent/income/discovery/stats", chain(handlers.DiscoveryStats))
	mux.Handle("/api/v1/agent/income/discovery/promote/", chain(handlers.DiscoveryPromote))

	// Agent time allocation / owner capacity (Iteration 41)
	mux.Handle("/api/v1/agent/capacity/state", chain(handlers.CapacityState))
	mux.Handle("/api/v1/agent/capacity/recompute", chain(handlers.CapacityRecompute))
	mux.Handle("/api/v1/agent/capacity/recommendations", chain(handlers.CapacityRecommendations))
	mux.Handle("/api/v1/agent/capacity/summary", chain(handlers.CapacitySummary))

	// Agent controlled self-extension sandbox (Iteration 43)
	mux.Handle("/api/v1/agent/self/proposals", chain(handlers.SelfProposals))
	mux.Handle("/api/v1/agent/self/spec/", chain(handlers.SelfSpec))
	mux.Handle("/api/v1/agent/self/sandbox/run/", chain(handlers.SelfSandboxRun))
	mux.Handle("/api/v1/agent/self/sandbox/results/", chain(handlers.SelfSandboxResults))
	mux.Handle("/api/v1/agent/self/deploy/", chain(handlers.SelfDeploy))
	mux.Handle("/api/v1/agent/self/rollback/", chain(handlers.SelfRollback))
	mux.Handle("/api/v1/agent/self/approve/", chain(handlers.SelfApprove))

	// Agent external action connectors (Iteration 45)
	mux.Handle("/api/v1/agent/external/actions", chain(handlers.ExternalActions))
	mux.Handle("/api/v1/agent/external/actions/", chain(handlers.ExternalActionRouter))

	// Agent strategic revenue portfolio (Iteration 46)
	mux.Handle("/api/v1/agent/strategies", chain(handlers.PortfolioStrategies))
	mux.Handle("/api/v1/agent/strategies/performance", chain(handlers.PortfolioStrategyPerformance))
	mux.Handle("/api/v1/agent/portfolio", chain(handlers.PortfolioView))
	mux.Handle("/api/v1/agent/portfolio/performance", chain(handlers.PortfolioPerformance))
	mux.Handle("/api/v1/agent/portfolio/rebalance", chain(handlers.PortfolioRebalance))
	mux.Handle("/api/v1/agent/portfolio/allocations", chain(handlers.PortfolioAllocations))

	// Agent autonomous scheduling & calendar control (Iteration 48)
	mux.Handle("/api/v1/agent/schedule/slots", chain(handlers.ScheduleSlots))
	mux.Handle("/api/v1/agent/schedule/recompute", chain(handlers.ScheduleRecompute))
	mux.Handle("/api/v1/agent/schedule/candidates", chain(handlers.ScheduleCandidates))
	mux.Handle("/api/v1/agent/schedule/recommend", chain(handlers.ScheduleRecommend))
	mux.Handle("/api/v1/agent/schedule/approve/", chain(handlers.ScheduleApprove))
	mux.Handle("/api/v1/agent/schedule/decisions", chain(handlers.ScheduleDecisions))
	mux.Handle("/api/v1/agent/schedule/calendar/", chain(handlers.ScheduleCalendar))

	// Agent negotiation / pricing intelligence (Iteration 47)
	mux.Handle("/api/v1/agent/pricing/profiles", chain(handlers.PricingProfiles))
	mux.Handle("/api/v1/agent/pricing/compute/", chain(handlers.PricingCompute))
	mux.Handle("/api/v1/agent/pricing/negotiations", chain(handlers.PricingNegotiations))
	mux.Handle("/api/v1/agent/pricing/recommend/", chain(handlers.PricingRecommend))
	mux.Handle("/api/v1/agent/pricing/outcomes", chain(handlers.PricingOutcomes))
	mux.Handle("/api/v1/agent/pricing/performance", chain(handlers.PricingPerformance))

	// Agent meta-reflection & meta-learning (Iteration 49)
	mux.Handle("/api/v1/agent/reflection/reports", chain(handlers.MetaReflectionReports))
	mux.Handle("/api/v1/agent/reflection/run", chain(handlers.MetaReflectionRun))
	mux.Handle("/api/v1/agent/reflection/latest", chain(handlers.MetaReflectionLatest))

	// Agent global objective function + risk model (Iteration 50)
	mux.Handle("/api/v1/agent/objective/state", chain(handlers.ObjectiveState))
	mux.Handle("/api/v1/agent/objective/risk", chain(handlers.ObjectiveRisk))
	mux.Handle("/api/v1/agent/objective/summary", chain(handlers.ObjectiveSummary))
	mux.Handle("/api/v1/agent/objective/recompute", chain(handlers.ObjectiveRecompute))

	// Agent closed feedback actuation (Iteration 51)
	mux.Handle("/api/v1/agent/actuation/decisions", chain(handlers.ActuationDecisions))
	mux.Handle("/api/v1/agent/actuation/run", chain(handlers.ActuationRun))
	mux.Handle("/api/v1/agent/actuation/approve/", chain(handlers.ActuationApprove))
	mux.Handle("/api/v1/agent/actuation/reject/", chain(handlers.ActuationReject))
	mux.Handle("/api/v1/agent/actuation/execute/", chain(handlers.ActuationExecute))

	// Agent execution loop (Iteration 53)
	mux.Handle("/api/v1/agent/execution/tasks", chain(handlers.ExecutionTasks))
	mux.Handle("/api/v1/agent/execution/tasks/", chain(handlers.ExecutionTaskDetail))
	mux.Handle("/api/v1/agent/execution/run/", chain(handlers.ExecutionRun))
	mux.Handle("/api/v1/agent/execution/abort/", chain(handlers.ExecutionAbort))

	// Agent autonomy runtime (Iteration 52)
	mux.Handle("/api/v1/agent/autonomy/state", chain(handlers.AutonomyState))
	mux.Handle("/api/v1/agent/autonomy/start", chain(handlers.AutonomyStart))
	mux.Handle("/api/v1/agent/autonomy/stop", chain(handlers.AutonomyStop))
	mux.Handle("/api/v1/agent/autonomy/reload-config", chain(handlers.AutonomyReloadConfig))
	mux.Handle("/api/v1/agent/autonomy/reports", chain(handlers.AutonomyReports))
	mux.Handle("/api/v1/agent/autonomy/set-mode", chain(handlers.AutonomySetMode))

	return requestIDMiddleware(mux)
}
