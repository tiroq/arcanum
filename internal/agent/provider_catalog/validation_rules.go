package provider_catalog

import (
	"fmt"
	"regexp"
)

var envVarRegex = regexp.MustCompile(envVarNamePattern)

// ValidCostClassesExtended includes "promo" in addition to the base set.
var ValidCostClassesExtended = map[string]bool{
	"free":    true,
	"local":   true,
	"cheap":   true,
	"promo":   true,
	"unknown": true,
}

// validateProviderFields validates provider-level required fields and semantics.
func validateProviderFields(entry ProviderCatalogFile, filename string) []ValidationIssue {
	var issues []ValidationIssue

	p := entry.Provider

	if p.Name == "" {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Field:    "provider.name",
			Code:     "provider_name_missing",
			Message:  "provider.name is required",
			Severity: SeverityError,
		})
	}

	if p.Kind == "" {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: p.Name,
			Field:    "provider.kind",
			Code:     "provider_kind_missing",
			Message:  "provider.kind is required",
			Severity: SeverityError,
		})
	} else if !ValidKinds[p.Kind] {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: p.Name,
			Field:    "provider.kind",
			Code:     "provider_kind_invalid",
			Message:  fmt.Sprintf("provider.kind %q is not valid (local|cloud|router)", p.Kind),
			Severity: SeverityError,
		})
	}

	// Enabled provider must have at least one enabled model.
	if p.Enabled {
		hasEnabledModel := false
		for _, m := range entry.Models {
			if m.Enabled {
				hasEnabledModel = true
				break
			}
		}
		if len(entry.Models) == 0 {
			issues = append(issues, ValidationIssue{
				File:     filename,
				Provider: p.Name,
				Field:    "models",
				Code:     "provider_enabled_without_models",
				Message:  "enabled provider must have at least one model",
				Severity: SeverityError,
			})
		} else if !hasEnabledModel {
			issues = append(issues, ValidationIssue{
				File:     filename,
				Provider: p.Name,
				Field:    "models",
				Code:     "provider_enabled_without_models",
				Message:  "enabled provider has models but none are enabled",
				Severity: SeverityWarning,
			})
		}
	}

	return issues
}

// validateConnectionFields validates connection-level fields.
func validateConnectionFields(entry ProviderCatalogFile, filename string) []ValidationIssue {
	var issues []ValidationIssue
	c := entry.Connection
	pName := entry.Provider.Name

	if c.BaseURL != "" {
		// We just check non-empty since we already checked presence.
		// If it was set to a whitespace-only value, that's still invalid.
	}

	if c.APIKeyEnv != "" && !envVarRegex.MatchString(c.APIKeyEnv) {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: pName,
			Field:    "connection.api_key_env",
			Code:     "api_key_env_invalid",
			Message:  fmt.Sprintf("api_key_env %q is not a valid environment variable name", c.APIKeyEnv),
			Severity: SeverityError,
		})
	}

	if c.TimeoutSeconds < 0 {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: pName,
			Field:    "connection.timeout_seconds",
			Code:     "timeout_invalid",
			Message:  fmt.Sprintf("timeout_seconds must be >= 0, got %d", c.TimeoutSeconds),
			Severity: SeverityError,
		})
	}

	return issues
}

// validateLimitsFields validates provider-level rate/token limits.
func validateLimitsFields(entry ProviderCatalogFile, filename string) []ValidationIssue {
	var issues []ValidationIssue
	l := entry.Limits
	pName := entry.Provider.Name

	type limitField struct {
		name  string
		value int
	}

	for _, lf := range []limitField{
		{"limits.rpm", l.RPM},
		{"limits.tpm", l.TPM},
		{"limits.rpd", l.RPD},
		{"limits.tpd", l.TPD},
	} {
		if lf.value < 0 {
			issues = append(issues, ValidationIssue{
				File:     filename,
				Provider: pName,
				Field:    lf.name,
				Code:     "limit_negative",
				Message:  fmt.Sprintf("%s must be >= 0, got %d", lf.name, lf.value),
				Severity: SeverityError,
			})
		}
	}

	return issues
}

// validateRoutingFields validates provider-level routing fields.
func validateRoutingFields(entry ProviderCatalogFile, filename string) []ValidationIssue {
	var issues []ValidationIssue
	r := entry.Routing
	pName := entry.Provider.Name

	for _, role := range r.Roles {
		if !ValidRoles[role] {
			issues = append(issues, ValidationIssue{
				File:     filename,
				Provider: pName,
				Field:    "routing.roles",
				Code:     "role_invalid",
				Message:  fmt.Sprintf("routing role %q is not recognized", role),
				Severity: SeverityWarning,
			})
		}
	}

	if r.FallbackPriority < 0 {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: pName,
			Field:    "routing.fallback_priority",
			Code:     "fallback_priority_invalid",
			Message:  fmt.Sprintf("fallback_priority must be >= 0, got %d", r.FallbackPriority),
			Severity: SeverityError,
		})
	}

	return issues
}

// validateModelFields validates a single model within a provider.
func validateModelFields(model ModelSpec, index int, providerName, filename string, providerRoles []string) []ValidationIssue {
	var issues []ValidationIssue

	if model.Name == "" {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: providerName,
			Field:    fmt.Sprintf("models[%d].name", index),
			Code:     "model_name_missing",
			Message:  fmt.Sprintf("models[%d].name is required", index),
			Severity: SeverityError,
		})
		return issues // can't validate further without a name
	}

	mName := model.Name

	// Cost class validation.
	if model.CostClass != "" && !ValidCostClassesExtended[model.CostClass] {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: providerName,
			Model:    mName,
			Field:    "cost_class",
			Code:     "model_cost_class_invalid",
			Message:  fmt.Sprintf("cost_class %q is not valid (free|local|cheap|promo|unknown)", model.CostClass),
			Severity: SeverityError,
		})
	}

	// Relative cost range.
	if model.RelativeCost < 0 || model.RelativeCost > 1 {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: providerName,
			Model:    mName,
			Field:    "relative_cost",
			Code:     "model_relative_cost_out_of_range",
			Message:  fmt.Sprintf("relative_cost must be 0.0–1.0, got %.4f", model.RelativeCost),
			Severity: SeverityError,
		})
	}

	// Max output tokens.
	if model.MaxOutputTokens < 0 {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: providerName,
			Model:    mName,
			Field:    "max_output_tokens",
			Code:     "model_max_output_tokens_invalid",
			Message:  fmt.Sprintf("max_output_tokens must be >= 0, got %d", model.MaxOutputTokens),
			Severity: SeverityError,
		})
	}

	// Roles validation.
	for _, role := range model.Roles {
		if !ValidRoles[role] {
			issues = append(issues, ValidationIssue{
				File:     filename,
				Provider: providerName,
				Model:    mName,
				Field:    "roles",
				Code:     "role_invalid",
				Message:  fmt.Sprintf("model role %q is not recognized", role),
				Severity: SeverityWarning,
			})
		}
	}

	// Capabilities validation.
	for _, cap := range model.Capabilities {
		if !ValidCapabilities[cap] {
			issues = append(issues, ValidationIssue{
				File:     filename,
				Provider: providerName,
				Model:    mName,
				Field:    "capabilities",
				Code:     "model_capability_invalid",
				Message:  fmt.Sprintf("capability %q is not recognized", cap),
				Severity: SeverityWarning,
			})
		}
	}

	// Enabled model must have roles (explicit or inheritable from provider).
	if model.Enabled && len(model.Roles) == 0 && len(providerRoles) == 0 {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: providerName,
			Model:    mName,
			Field:    "roles",
			Code:     "model_enabled_without_roles",
			Message:  "enabled model has no roles and provider has no roles to inherit",
			Severity: SeverityWarning,
		})
	}

	return issues
}

// validateModels validates all models within a provider entry.
func validateModels(entry ProviderCatalogFile, filename string) []ValidationIssue {
	var issues []ValidationIssue
	pName := entry.Provider.Name

	// Check for duplicate model names.
	modelNames := make(map[string]bool)
	for i, m := range entry.Models {
		issues = append(issues, validateModelFields(m, i, pName, filename, entry.Routing.Roles)...)

		if m.Name != "" {
			if modelNames[m.Name] {
				issues = append(issues, ValidationIssue{
					File:     filename,
					Provider: pName,
					Model:    m.Name,
					Field:    "name",
					Code:     "model_duplicate",
					Message:  fmt.Sprintf("duplicate model name %q within provider", m.Name),
					Severity: SeverityError,
				})
			}
			modelNames[m.Name] = true
		}
	}

	// Check model count.
	if len(entry.Models) > MaxModelsPerProvider {
		issues = append(issues, ValidationIssue{
			File:     filename,
			Provider: pName,
			Field:    "models",
			Code:     "enabled_model_cap_exceeded",
			Message:  fmt.Sprintf("too many models: %d > max %d", len(entry.Models), MaxModelsPerProvider),
			Severity: SeverityError,
		})
	}

	return issues
}

// validateSingleEntry runs all per-file validation rules on one catalog entry.
func validateSingleEntry(entry ProviderCatalogFile, filename string) []ValidationIssue {
	var issues []ValidationIssue
	issues = append(issues, validateProviderFields(entry, filename)...)
	issues = append(issues, validateConnectionFields(entry, filename)...)
	issues = append(issues, validateLimitsFields(entry, filename)...)
	issues = append(issues, validateRoutingFields(entry, filename)...)
	issues = append(issues, validateModels(entry, filename)...)
	return issues
}

// validateCrossFile runs directory-level cross-file validation rules.
func validateCrossFile(entries []ProviderCatalogFile, filenames []string) []ValidationIssue {
	if len(entries) == 0 {
		return nil
	}

	var issues []ValidationIssue

	// 1. Check for duplicate provider names across files.
	providerSeen := make(map[string]string) // name → filename
	for i, entry := range entries {
		pName := entry.Provider.Name
		if pName == "" {
			continue
		}
		if prevFile, exists := providerSeen[pName]; exists {
			issues = append(issues, ValidationIssue{
				File:     filenames[i],
				Provider: pName,
				Field:    "provider.name",
				Code:     "provider_duplicate",
				Message:  fmt.Sprintf("provider %q already defined in %s", pName, prevFile),
				Severity: SeverityError,
			})
		} else {
			providerSeen[pName] = filenames[i]
		}
	}

	// 2. Check for duplicate (provider, model) targets.
	type target struct{ provider, model string }
	targetSeen := make(map[target]string) // target → filename
	for i, entry := range entries {
		pName := entry.Provider.Name
		for _, m := range entry.Models {
			if m.Name == "" {
				continue
			}
			t := target{pName, m.Name}
			if prevFile, exists := targetSeen[t]; exists {
				issues = append(issues, ValidationIssue{
					File:     filenames[i],
					Provider: pName,
					Model:    m.Name,
					Field:    "name",
					Code:     "provider_model_duplicate",
					Message:  fmt.Sprintf("target %s/%s already defined in %s", pName, m.Name, prevFile),
					Severity: SeverityError,
				})
			} else {
				targetSeen[t] = filenames[i]
			}
		}
	}

	// 3. Check critical role coverage across all enabled providers/models.
	enabledRoles := make(map[string]bool)
	hasLocalProvider := false
	allExternal := true

	for _, entry := range entries {
		if !entry.Provider.Enabled {
			continue
		}
		if entry.Provider.Kind == "local" {
			hasLocalProvider = true
			allExternal = false
		}
		for _, m := range entry.Models {
			if !m.Enabled {
				continue
			}
			roles := m.Roles
			if len(roles) == 0 {
				roles = entry.Routing.Roles
			}
			for _, role := range roles {
				enabledRoles[role] = true
			}
		}
	}

	for _, critRole := range CriticalRoles {
		if !enabledRoles[critRole] {
			issues = append(issues, ValidationIssue{
				File:     "(catalog)",
				Field:    "roles",
				Code:     "critical_role_missing",
				Message:  fmt.Sprintf("no enabled model covers critical role %q", critRole),
				Severity: SeverityError,
			})
		}
	}

	// 4. Local fallback warning.
	if !hasLocalProvider && len(entries) > 0 {
		issues = append(issues, ValidationIssue{
			File:     "(catalog)",
			Code:     "no_local_fallback_warning",
			Message:  "no enabled local provider exists; no local fallback available",
			Severity: SeverityWarning,
		})
	}

	// 5. All external warning.
	if allExternal && len(entries) > 0 {
		// Only warn if there are enabled providers.
		hasEnabled := false
		for _, e := range entries {
			if e.Provider.Enabled {
				hasEnabled = true
				break
			}
		}
		if hasEnabled {
			issues = append(issues, ValidationIssue{
				File:     "(catalog)",
				Code:     "all_external_warning",
				Message:  "all enabled providers are external; consider adding a local provider",
				Severity: SeverityWarning,
			})
		}
	}

	return issues
}
