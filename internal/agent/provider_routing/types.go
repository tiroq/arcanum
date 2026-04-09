package provider_routing

import "time"

// Provider kinds.
const (
	KindLocal  = "local"
	KindCloud  = "cloud"
	KindRouter = "router"
)

// Provider roles.
const (
	RoleFast     = "fast"
	RolePlanner  = "planner"
	RoleReviewer = "reviewer"
	RoleBatch    = "batch"
	RoleFallback = "fallback"
)

// Cost classes.
const (
	CostFree    = "free"
	CostLocal   = "local"
	CostCheap   = "cheap"
	CostUnknown = "unknown"
)

// Scoring weights — must sum to 1.0.
const (
	WeightLatencyFit      = 0.30
	WeightQuotaHeadroom   = 0.25
	WeightReliability     = 0.20
	WeightCostEfficiency  = 0.15
	WeightModelCapability = 0.10
)

// MaxFallbackChainLength is the maximum number of providers in a fallback chain (excluding primary).
const MaxFallbackChainLength = 3

// TieBreakEpsilon is the threshold below which two scores are considered tied.
const TieBreakEpsilon = 0.001

// Provider defines a registered LLM provider with its capabilities, limits, and health.
type Provider struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`         // local | cloud | router
	Roles        []string `json:"roles"`        // fast | planner | reviewer | batch | fallback
	Capabilities []string `json:"capabilities"` // json_mode | long_context | low_latency | tool_calling

	Limits ProviderLimits    `json:"limits"`
	Cost   ProviderCostModel `json:"cost"`
	Health ProviderHealth    `json:"health"`
}

// IsLocal returns true if the provider is a local provider.
func (p Provider) IsLocal() bool {
	return p.Kind == KindLocal
}

// IsExternal returns true if the provider is a cloud or router provider.
func (p Provider) IsExternal() bool {
	return p.Kind == KindCloud || p.Kind == KindRouter
}

// HasRole returns true if the provider has the given role.
func (p Provider) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasCapability returns true if the provider has the given capability.
func (p Provider) HasCapability(cap string) bool {
	for _, c := range p.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// ProviderLimits defines rate and token limits for a provider.
// A zero value means the limit is unknown (not unlimited).
type ProviderLimits struct {
	RPM int `json:"rpm"` // requests per minute; 0 = unknown
	TPM int `json:"tpm"` // tokens per minute; 0 = unknown
	RPD int `json:"rpd"` // requests per day; 0 = unknown
	TPD int `json:"tpd"` // tokens per day; 0 = unknown
}

// ProviderCostModel defines cost characteristics for a provider.
type ProviderCostModel struct {
	CostClass    string  `json:"cost_class"`    // free | local | cheap | unknown
	RelativeCost float64 `json:"relative_cost"` // normalized 0.0–1.0
}

// ProviderHealth defines the operational status of a provider.
type ProviderHealth struct {
	Enabled       bool      `json:"enabled"`
	Reachable     bool      `json:"reachable"`
	Degraded      bool      `json:"degraded"`
	LastCheckedAt time.Time `json:"last_checked_at"`
}

// RoutingInput describes what the system needs from a provider for this task.
type RoutingInput struct {
	GoalType             string   `json:"goal_type"`
	TaskType             string   `json:"task_type"`
	PreferredRole        string   `json:"preferred_role"` // fast | planner | reviewer | batch
	EstimatedTokens      int      `json:"estimated_tokens"`
	LatencyBudgetMs      int      `json:"latency_budget_ms"`
	ConfidenceRequired   float64  `json:"confidence_required"`
	AllowExternal        bool     `json:"allow_external"`
	RequiredCapabilities []string `json:"required_capabilities,omitempty"` // Iteration 32: optional capability requirements
}

// RoutingDecision is the final output of the routing engine.
type RoutingDecision struct {
	SelectedProvider string       `json:"selected_provider"`
	SelectedModel    string       `json:"selected_model,omitempty"` // Iteration 32: model-level selection
	FallbackChain    []string     `json:"fallback_chain"`
	Reason           string       `json:"reason"`
	Trace            RoutingTrace `json:"trace"`
}

// SelectedTarget returns a RoutingTarget for the selected provider+model.
func (d RoutingDecision) SelectedTarget() RoutingTarget {
	return RoutingTarget{Provider: d.SelectedProvider, Model: d.SelectedModel}
}

// RoutingTarget represents a selected provider+model pair.
// Backward compatible: Model may be empty for provider-only routing.
type RoutingTarget struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// String returns a deterministic string representation.
func (t RoutingTarget) String() string {
	if t.Model == "" {
		return t.Provider
	}
	return t.Provider + "/" + t.Model
}

// IsEmpty returns true if no provider is selected.
func (t RoutingTarget) IsEmpty() bool {
	return t.Provider == ""
}

// RoutingTrace records the full decision-making process for observability.
type RoutingTrace struct {
	ConsideredProviders []string           `json:"considered_providers"`
	RejectedProviders   []RejectedProvider `json:"rejected_providers"`
	RankedProviders     []RankedProvider   `json:"ranked_providers"`
	FinalReason         string             `json:"final_reason"`
}

// RejectedProvider records why a provider was filtered out.
type RejectedProvider struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"` // Iteration 32: model-level rejection
	Reason   string `json:"reason"`
}

// RankedProvider records a provider's score and scoring rationale.
type RankedProvider struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model,omitempty"` // Iteration 32: model-level ranking
	Score    float64 `json:"score"`
	Reason   string  `json:"reason"`
}

// ProviderUsageState tracks current quota consumption for a provider.
type ProviderUsageState struct {
	ProviderName string `json:"provider_name"`

	RequestsThisMinute int `json:"requests_this_minute"`
	TokensThisMinute   int `json:"tokens_this_minute"`

	RequestsToday int `json:"requests_today"`
	TokensToday   int `json:"tokens_today"`

	LastUpdated time.Time `json:"last_updated"`
}

// GlobalPolicyConfig holds routing policy settings derived from providers/_global.yaml.
// It is consumed by the Router without importing the provider_catalog package
// (to avoid import cycles). The api-gateway main.go bridges the two by reading
// the GlobalPolicy and constructing a GlobalPolicyConfig.
type GlobalPolicyConfig struct {
	// PreferFree instructs the router to boost free/local providers during scoring.
	PreferFree bool

	// AllowExternal is a global gate for external (cloud / router) providers.
	// When false, external providers are filtered even if RoutingInput.AllowExternal=true.
	// Applied as: effectiveAllowExternal = input.AllowExternal && policy.AllowExternal.
	AllowExternal bool

	// MaxFallbackChain overrides the MaxFallbackChainLength constant when > 0.
	MaxFallbackChain int

	// RolePreferences maps role names to ordered provider preference lists.
	// Providers listed first receive a scoring boost (position 0 = highest boost).
	// Example: {"fast": ["groq", "gemini", "ollama"]}.
	RolePreferences map[string][]string

	// DegradePolicy defines provider tier ordering for fallback chain assembly.
	// Valid tier names: external_strong, external_fast, router, local.
	// Providers matching earlier tiers appear first in the fallback chain.
	DegradePolicy []string
}

// RoutingRecord persists a routing decision for observability queries.
type RoutingRecord struct {
	ID               string    `json:"id"`
	GoalType         string    `json:"goal_type"`
	TaskType         string    `json:"task_type"`
	SelectedProvider string    `json:"selected_provider"`
	SelectedModel    string    `json:"selected_model,omitempty"` // Iteration 32
	FallbackChain    []string  `json:"fallback_chain"`
	Reason           string    `json:"reason"`
	CreatedAt        time.Time `json:"created_at"`
}
