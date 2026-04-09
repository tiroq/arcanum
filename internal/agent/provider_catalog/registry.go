package provider_catalog

import (
	"sort"
	"sync"
)

// CatalogRegistry holds the in-memory provider+model catalog built from YAML files.
// Thread-safe for concurrent reads. Built once at startup; no dynamic reload.
type CatalogRegistry struct {
	mu       sync.RWMutex
	models   []ProviderModel          // all models, deterministic order
	byKey    map[string]ProviderModel // key = "provider/model"
	catalogs []ProviderCatalogFile    // raw catalog files for API
}

// NewCatalogRegistry creates an empty catalog registry.
func NewCatalogRegistry() *CatalogRegistry {
	return &CatalogRegistry{
		models: make([]ProviderModel, 0),
		byKey:  make(map[string]ProviderModel),
	}
}

// BuildFromCatalog populates the registry from parsed catalog entries.
// Provider+model pairs are flattened and stored in deterministic order
// (provider name ASC, model name ASC).
func (cr *CatalogRegistry) BuildFromCatalog(catalogs []ProviderCatalogFile) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	cr.catalogs = catalogs
	cr.models = make([]ProviderModel, 0)
	cr.byKey = make(map[string]ProviderModel)

	for _, cat := range catalogs {
		if !cat.Provider.Enabled {
			continue
		}
		for _, m := range cat.Models {
			if !m.Enabled {
				continue
			}

			// Inherit roles: model-specific roles override provider-level roles.
			roles := m.Roles
			if len(roles) == 0 {
				roles = cat.Routing.Roles
			}

			// Inherit capabilities from model only (no provider-level capabilities).
			caps := m.Capabilities

			pm := ProviderModel{
				ProviderName:    cat.Provider.Name,
				ModelName:       m.Name,
				ProviderKind:    cat.Provider.Kind,
				Roles:           roles,
				Capabilities:    caps,
				CostClass:       m.CostClass,
				RelativeCost:    m.RelativeCost,
				MaxOutputTokens: m.MaxOutputTokens,
				Enabled:         true,
			}

			key := pm.ProviderName + "/" + pm.ModelName
			cr.byKey[key] = pm
			cr.models = append(cr.models, pm)
		}
	}

	// Sort deterministically: provider name ASC, then model name ASC.
	sort.Slice(cr.models, func(i, j int) bool {
		if cr.models[i].ProviderName != cr.models[j].ProviderName {
			return cr.models[i].ProviderName < cr.models[j].ProviderName
		}
		return cr.models[i].ModelName < cr.models[j].ModelName
	})
}

// All returns all enabled provider+model pairs in deterministic order.
func (cr *CatalogRegistry) All() []ProviderModel {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	result := make([]ProviderModel, len(cr.models))
	copy(result, cr.models)
	return result
}

// Get returns a provider+model by key ("provider/model").
func (cr *CatalogRegistry) Get(provider, model string) (ProviderModel, bool) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	key := provider + "/" + model
	pm, ok := cr.byKey[key]
	return pm, ok
}

// ByProvider returns all models for a given provider in deterministic order.
func (cr *CatalogRegistry) ByProvider(provider string) []ProviderModel {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	var result []ProviderModel
	for _, pm := range cr.models {
		if pm.ProviderName == provider {
			result = append(result, pm)
		}
	}
	return result
}

// ByRole returns all models matching the given role in deterministic order.
func (cr *CatalogRegistry) ByRole(role string) []ProviderModel {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	var result []ProviderModel
	for _, pm := range cr.models {
		if pm.HasRole(role) {
			result = append(result, pm)
		}
	}
	return result
}

// Count returns the number of registered provider+model pairs.
func (cr *CatalogRegistry) Count() int {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return len(cr.models)
}

// RawCatalogs returns the raw catalog files for API responses.
func (cr *CatalogRegistry) RawCatalogs() []ProviderCatalogFile {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	result := make([]ProviderCatalogFile, len(cr.catalogs))
	copy(result, cr.catalogs)
	return result
}

// Targets returns all provider+model pairs as RoutingTarget in deterministic order.
func (cr *CatalogRegistry) Targets() []RoutingTarget {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	result := make([]RoutingTarget, len(cr.models))
	for i, pm := range cr.models {
		result[i] = RoutingTarget{
			Provider: pm.ProviderName,
			Model:    pm.ModelName,
		}
	}
	return result
}

// BuildModelExecutionMap constructs a map of "provider/model" → ModelExecutionSpec
// for all enabled provider+model pairs in all catalog files.
// Used by cmd/api-gateway/main.go to populate the router's execution config map
// without creating an import cycle between provider_catalog and provider_routing.
func BuildModelExecutionMap(catalogs []ProviderCatalogFile) map[string]ModelExecutionSpec {
	m := make(map[string]ModelExecutionSpec)
	for _, cat := range catalogs {
		if !cat.Provider.Enabled {
			continue
		}
		for _, model := range cat.Models {
			if !model.Enabled {
				continue
			}
			key := cat.Provider.Name + "/" + model.Name
			m[key] = model.Execution
		}
	}
	return m
}
