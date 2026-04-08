package provider_routing

import (
	"context"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// ProviderRoutingProvider is the interface exposed to the decision graph planner adapter.
// Defined with primitive parameters to avoid import cycles.
// Fail-open: if provider is nil, routing defaults to existing behavior.
type ProviderRoutingProvider interface {
	// RouteForTask selects the best provider for the given task parameters.
	// Returns selected provider name, fallback chain, and routing reason.
	// Returns ("", nil, "no router") if routing is unavailable (fail-open).
	RouteForTask(ctx context.Context, goalType, taskType, preferredRole string,
		estimatedTokens, latencyBudgetMs int, confidenceRequired float64,
		allowExternal bool) (selected string, fallbackChain []string, reason string)
}

// GraphAdapter implements ProviderRoutingProvider, bridging the Router
// to the decision graph planner adapter without import cycles.
type GraphAdapter struct {
	router  *Router
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter wrapping a Router.
func NewGraphAdapter(router *Router, auditor audit.AuditRecorder, logger *zap.Logger) *GraphAdapter {
	if router == nil {
		return nil
	}
	return &GraphAdapter{
		router:  router,
		auditor: auditor,
		logger:  logger,
	}
}

// RouteForTask implements ProviderRoutingProvider.
func (a *GraphAdapter) RouteForTask(ctx context.Context, goalType, taskType, preferredRole string,
	estimatedTokens, latencyBudgetMs int, confidenceRequired float64,
	allowExternal bool) (string, []string, string) {
	if a == nil || a.router == nil {
		return "", nil, "provider router not configured"
	}

	input := RoutingInput{
		GoalType:           goalType,
		TaskType:           taskType,
		PreferredRole:      preferredRole,
		EstimatedTokens:    estimatedTokens,
		LatencyBudgetMs:    latencyBudgetMs,
		ConfidenceRequired: confidenceRequired,
		AllowExternal:      allowExternal,
	}

	decision := a.router.Route(ctx, input)
	return decision.SelectedProvider, decision.FallbackChain, decision.Reason
}

// GetRouter returns the underlying router for direct access (API handlers).
func (a *GraphAdapter) GetRouter() *Router {
	if a == nil {
		return nil
	}
	return a.router
}

// GetRegistry returns the underlying registry for direct access (API handlers).
func (a *GraphAdapter) GetRegistry() *Registry {
	if a == nil || a.router == nil {
		return nil
	}
	return a.router.registry
}

// GetQuotaTracker returns the underlying quota tracker for direct access (API handlers).
func (a *GraphAdapter) GetQuotaTracker() *QuotaTracker {
	if a == nil || a.router == nil {
		return nil
	}
	return a.router.quotas
}
