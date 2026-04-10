package externalactions

import (
	"sync"
)

// ConnectorRouter selects and manages connectors for action execution.
// Thread-safe. Supports enable/disable (kill-switch) per connector.
type ConnectorRouter struct {
	mu         sync.RWMutex
	connectors map[string]Connector
}

// NewConnectorRouter creates a new connector router.
func NewConnectorRouter() *ConnectorRouter {
	return &ConnectorRouter{
		connectors: make(map[string]Connector),
	}
}

// Register adds a connector to the router.
func (r *ConnectorRouter) Register(c Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectors[c.Name()] = c
}

// Route selects the best connector for the given action type.
// Returns the connector and true if found, or nil and false if no suitable connector.
func (r *ConnectorRouter) Route(actionType string) (Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, c := range r.connectors {
		if c.Enabled() && c.Supports(actionType) {
			return c, true
		}
	}
	return nil, false
}

// RouteByName selects a specific connector by name.
func (r *ConnectorRouter) RouteByName(name string) (Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.connectors[name]
	if !ok || !c.Enabled() {
		return nil, false
	}
	return c, true
}

// List returns all registered connector names.
func (r *ConnectorRouter) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}

// --- Policy Engine ---

// PolicyEngine evaluates whether an action requires review.
type PolicyEngine struct{}

// NewPolicyEngine creates a new policy engine.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{}
}

// Evaluate determines the policy for an action based on its type and payload.
func (p *PolicyEngine) Evaluate(action ExternalAction) ActionPolicy {
	// High-risk action types always require review.
	if HighRiskActionTypes[action.ActionType] {
		return ActionPolicy{
			RequiresReview: true,
			RiskLevel:      RiskHigh,
			Reason:         "action type " + action.ActionType + " requires human review",
		}
	}

	// Actions with financial implications require review.
	if action.OpportunityID != "" {
		return ActionPolicy{
			RequiresReview: true,
			RiskLevel:      RiskMedium,
			Reason:         "action linked to income opportunity requires review",
		}
	}

	// Default: low risk, no review needed.
	return ActionPolicy{
		RequiresReview: false,
		RiskLevel:      RiskLow,
		Reason:         "standard action — no review required",
	}
}
