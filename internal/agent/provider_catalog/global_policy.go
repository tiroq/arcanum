package provider_catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// globalPolicyFile is the reserved filename for the global routing policy.
// This file is intentionally separate from provider YAML files and is NOT
// processed by LoadCatalog — it is loaded exclusively via LoadGlobalPolicy.
const globalPolicyFile = "_global.yaml"

// GlobalPolicy holds the top-level routing policy loaded from providers/_global.yaml.
//
// The _global.yaml file was previously silently skipped by the catalog loader
// (ValidateCatalogEntry rejected it because provider.name was empty). It is now
// loaded and applied explicitly via this type.
type GlobalPolicy struct {
	RoutingPolicy GlobalRoutingPolicy `yaml:"routing_policy"`
}

// GlobalRoutingPolicy holds the granular routing settings from _global.yaml.
type GlobalRoutingPolicy struct {
	// PreferFree instructs the router to prefer zero-cost providers when scoring.
	PreferFree bool `yaml:"prefer_free"`

	// AllowExternal is a global gate for external (cloud / router) providers.
	// When false, external providers are blocked regardless of per-request settings.
	AllowExternal bool `yaml:"allow_external"`

	// MaxFallbackChain caps the fallback chain length.
	// Overrides the default MaxFallbackChainLength constant in provider_routing.
	MaxFallbackChain int `yaml:"max_fallback_chain"`

	// Priorities maps role names (fast, planner, reviewer, batch, fallback) to
	// ordered provider preference lists. Providers listed first get a small scoring
	// boost so that preference ordering influences routing decisions deterministically.
	Priorities map[string]RolePriority `yaml:"priorities"`

	// Constraints provides soft limits that inform routing decisions.
	Constraints GlobalPolicyConstraints `yaml:"constraints"`

	// DegradePolicy defines the tier ordering for fallback chain assembly.
	// Valid tier names: external_strong, external_fast, router, local.
	// Providers matching earlier tiers are placed first in the fallback chain.
	DegradePolicy []string `yaml:"degrade_policy"`
}

// RolePriority defines the preferred provider ordering for a single role.
type RolePriority struct {
	// Prefer is an ordered list of provider names. Providers listed first
	// receive the highest preference boost during scoring.
	Prefer []string `yaml:"prefer"`
}

// GlobalPolicyConstraints provides soft routing limits used for scoring hints.
type GlobalPolicyConstraints struct {
	// LatencySensitiveThresholdMs is the latency budget (ms) below which
	// latency-sensitive scoring is applied.
	LatencySensitiveThresholdMs int `yaml:"latency_sensitive_threshold_ms"`

	// HeavyTaskTokensThreshold is the token count above which a task is
	// considered "heavy" and capacity-sensitive scoring applies.
	HeavyTaskTokensThreshold int `yaml:"heavy_task_tokens_threshold"`
}

// LoadGlobalPolicy loads the global routing policy from providers/_global.yaml.
// Returns nil, nil when the file does not exist (fail-open — system uses defaults).
// Returns an error only for I/O failures or YAML parse errors.
//
// The _global.yaml file uses a different schema from provider catalog files:
// it has a top-level "routing_policy" key instead of "provider", "models", etc.
// This function must be called separately from LoadCatalog, which skips _-prefixed files.
func LoadGlobalPolicy(dir string, logger *zap.Logger) (*GlobalPolicy, error) {
	path := filepath.Join(dir, globalPolicyFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if logger != nil {
				logger.Info("global routing policy file not found; using defaults",
					zap.String("path", path))
			}
			return nil, nil
		}
		return nil, fmt.Errorf("read global policy %s: %w", path, err)
	}

	var policy GlobalPolicy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("parse global policy %s: %w", path, err)
	}

	if logger != nil {
		roleCount := len(policy.RoutingPolicy.Priorities)
		logger.Info("global routing policy loaded",
			zap.String("path", path),
			zap.Bool("allow_external", policy.RoutingPolicy.AllowExternal),
			zap.Bool("prefer_free", policy.RoutingPolicy.PreferFree),
			zap.Int("max_fallback_chain", policy.RoutingPolicy.MaxFallbackChain),
			zap.Int("role_priorities", roleCount),
			zap.Strings("degrade_policy", policy.RoutingPolicy.DegradePolicy),
		)
	}

	return &policy, nil
}
