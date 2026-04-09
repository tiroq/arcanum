package provider_catalog

// MaxModelsPerProvider is the maximum number of models evaluated per provider during routing.
const MaxModelsPerProvider = 10

// ProviderCatalogFile represents the top-level structure of a provider YAML file.
type ProviderCatalogFile struct {
	Provider          ProviderSpec          `yaml:"provider"           json:"provider"`
	Connection        ConnectionSpec        `yaml:"connection"         json:"connection"`
	Limits            LimitsSpec            `yaml:"limits"             json:"limits"`
	Routing           RoutingSpec           `yaml:"routing"            json:"routing"`
	Models            []ModelSpec           `yaml:"models"             json:"models"`
	ExecutionProfiles ExecutionProfilesSpec `yaml:"execution_profiles" json:"execution_profiles,omitempty"`
}

// ExecutionProfilesSpec maps role names to ordered lists of execution candidates.
// Used by providers to define per-role model candidate chains.
// Candidates reference models by name (ref); execution settings are resolved
// from the referenced model's execution block in models[].
//
// Role names match providers.ModelRole values: "default", "fast", "planner", "review".
type ExecutionProfilesSpec map[string][]ExecutionCandidateSpec

// ExecutionCandidateSpec defines a single candidate in an execution profile.
// Candidates reference a model by name via the Ref field.
// Execution settings (think, timeout, json_mode) are resolved from the
// referenced model's execution block — no inline execution settings are allowed.
type ExecutionCandidateSpec struct {
	// Ref is the model name to use (must match a name in models[]).
	// Inline execution settings are forbidden; configure them in models[N].execution.
	Ref string `yaml:"ref" json:"ref"`
}

// ModelExecutionSpec defines the per-model execution configuration.
// Each model carries its own execution settings so execution_profiles
// only describe ordering, not duplicate execution config.
type ModelExecutionSpec struct {
	// TimeoutSeconds is the per-call timeout. Zero means use provider default.
	TimeoutSeconds int `yaml:"timeout_seconds" json:"timeout_seconds,omitempty"`
	// Think controls thinking/reasoning behavior: "on", "off", or "" (provider default).
	Think string `yaml:"think" json:"think,omitempty"`
	// JSONMode requests structured JSON output from the model.
	JSONMode bool `yaml:"json_mode" json:"json_mode,omitempty"`
	// MaxOutputTokens overrides the model-level max_output_tokens for execution.
	// Zero means use the top-level max_output_tokens field.
	MaxOutputTokens int `yaml:"max_output_tokens" json:"max_output_tokens,omitempty"`
}

// ProviderSpec defines the identity and classification of a provider.
type ProviderSpec struct {
	Name    string `yaml:"name"    json:"name"`
	Kind    string `yaml:"kind"    json:"kind"` // local | cloud | router
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// ConnectionSpec defines how to connect to the provider.
// api_key_env references an environment variable name — the actual secret
// is never stored in the YAML file.
type ConnectionSpec struct {
	BaseURL        string `yaml:"base_url"         json:"base_url"`
	APIKeyEnv      string `yaml:"api_key_env"      json:"api_key_env,omitempty"`
	TimeoutSeconds int    `yaml:"timeout_seconds"  json:"timeout_seconds"`
}

// LimitsSpec defines provider-level rate and token limits.
// A zero value means the limit is unknown (not unlimited).
type LimitsSpec struct {
	RPM int `yaml:"rpm" json:"rpm"`
	TPM int `yaml:"tpm" json:"tpm"`
	RPD int `yaml:"rpd" json:"rpd"`
	TPD int `yaml:"tpd" json:"tpd"`
}

// RoutingSpec defines provider-level routing metadata.
type RoutingSpec struct {
	Roles            []string `yaml:"roles"             json:"roles"`
	FallbackPriority int      `yaml:"fallback_priority" json:"fallback_priority"`
	AllowExternal    bool     `yaml:"allow_external"    json:"allow_external"`
}

// ModelSpec defines a single model offered by a provider.
type ModelSpec struct {
	Name            string   `yaml:"name"              json:"name"`
	Enabled         bool     `yaml:"enabled"           json:"enabled"`
	Roles           []string `yaml:"roles"             json:"roles"`
	Capabilities    []string `yaml:"capabilities"      json:"capabilities"`
	CostClass       string   `yaml:"cost_class"        json:"cost_class"`
	RelativeCost    float64  `yaml:"relative_cost"     json:"relative_cost"`
	MaxOutputTokens int      `yaml:"max_output_tokens" json:"max_output_tokens"`
	// Execution holds per-model execution configuration.
	// Referenced by execution_profiles entries (via Ref) to build ExecutionPlan.
	Execution ModelExecutionSpec `yaml:"execution"         json:"execution,omitempty"`
}

// ProviderModel is the flattened, runtime representation of a provider+model pair
// used by the routing engine.
type ProviderModel struct {
	ProviderName string   `json:"provider_name"`
	ModelName    string   `json:"model_name"`
	ProviderKind string   `json:"provider_kind"` // local | cloud | router
	Roles        []string `json:"roles"`
	Capabilities []string `json:"capabilities"`
	CostClass    string   `json:"cost_class"`
	RelativeCost float64  `json:"relative_cost"`

	MaxOutputTokens int  `json:"max_output_tokens"`
	Enabled         bool `json:"enabled"`
}

// RoutingTarget represents a selected provider+model pair.
type RoutingTarget struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// String returns a deterministic string representation: "provider/model" or "provider".
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

// HasRole returns true if the model has the given role.
func (pm ProviderModel) HasRole(role string) bool {
	for _, r := range pm.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasCapability returns true if the model has the given capability.
func (pm ProviderModel) HasCapability(cap string) bool {
	for _, c := range pm.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// IsLocal returns true if the provider backing this model is local.
func (pm ProviderModel) IsLocal() bool {
	return pm.ProviderKind == "local"
}

// IsExternal returns true if the provider backing this model is cloud or router.
func (pm ProviderModel) IsExternal() bool {
	return pm.ProviderKind == "cloud" || pm.ProviderKind == "router"
}
