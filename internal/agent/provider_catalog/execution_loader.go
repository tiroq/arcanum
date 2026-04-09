package provider_catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

// requiredExecutionRoles are the model roles that must have execution candidates
// defined in a provider's execution_profiles section. LoadExecutionProfiles fails
// explicitly if any of these roles are missing.
var requiredExecutionRoles = []providers.ModelRole{
	providers.RoleDefault,
	providers.RoleFast,
	providers.RolePlanner,
	providers.RoleReview,
}

// LoadExecutionProfiles reads the YAML file for the named provider from dir and
// returns a map of role → ordered model candidates suitable for worker execution.
//
// The function reads the execution_profiles section of the provider YAML file.
// It does NOT load from env vars. If execution_profiles is missing or any required
// role has no candidates, it returns an explicit error — operators must configure
// execution settings in providers/<providerName>.yaml.
//
// Fails explicitly rather than silently degrading, per requirement F.
func LoadExecutionProfiles(dir, providerName string, logger *zap.Logger) (map[providers.ModelRole][]profile.ModelCandidate, error) {
	path := filepath.Join(dir, providerName+".yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"execution profiles: provider file %q not found in %q — "+
					"create providers/%s.yaml with an execution_profiles section",
				providerName+".yaml", dir, providerName)
		}
		return nil, fmt.Errorf("execution profiles: read %s: %w", path, err)
	}

	var entry ProviderCatalogFile
	if err := yaml.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("execution profiles: parse %s: %w", path, err)
	}

	if len(entry.ExecutionProfiles) == 0 {
		return nil, fmt.Errorf(
			"execution profiles: %s.yaml has no execution_profiles section — "+
				"add an execution_profiles block to providers/%s.yaml to configure "+
				"per-role model candidate chains (think mode, timeout, json_mode)",
			providerName, providerName)
	}

	result := make(map[providers.ModelRole][]profile.ModelCandidate, len(requiredExecutionRoles))

	for _, role := range requiredExecutionRoles {
		specs, ok := entry.ExecutionProfiles[string(role)]
		if !ok || len(specs) == 0 {
			return nil, fmt.Errorf(
				"execution profiles: required role %q has no candidates in %s.yaml — "+
					"add a %q block under execution_profiles in providers/%s.yaml",
				role, providerName, role, providerName)
		}

		candidates := make([]profile.ModelCandidate, 0, len(specs))
		for i, spec := range specs {
			if spec.Model == "" {
				return nil, fmt.Errorf(
					"execution profiles: role %q candidate %d in %s.yaml: model name is required",
					role, i, providerName)
			}

			c := profile.ModelCandidate{
				ModelName:    spec.Model,
				ProviderName: providerName,
				JSONMode:     spec.JSONMode,
			}

			if spec.Think != "" {
				tm, err := profile.ParseThinkMode(spec.Think)
				if err != nil {
					return nil, fmt.Errorf(
						"execution profiles: role %q candidate %d (%q) in %s.yaml: %w",
						role, i, spec.Model, providerName, err)
				}
				c.ThinkMode = tm
			}

			if spec.TimeoutSeconds > 0 {
				c.Timeout = time.Duration(spec.TimeoutSeconds) * time.Second
			}

			candidates = append(candidates, c)
		}

		result[role] = candidates
	}

	if logger != nil {
		logger.Info("loaded execution profiles from catalog",
			zap.String("provider", providerName),
			zap.String("path", path),
			zap.Int("roles", len(result)),
		)
	}

	return result, nil
}
