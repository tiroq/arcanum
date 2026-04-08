package provider_routing

import (
	"sort"
	"sync"
)

// Registry is a thread-safe provider registry for managing known providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds or replaces a provider in the registry.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name] = p
}

// Get returns a provider by name and whether it exists.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// All returns all registered providers in deterministic (name-sorted) order.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Enabled returns all enabled providers in deterministic order.
func (r *Registry) Enabled() []Provider {
	all := r.All()
	result := make([]Provider, 0, len(all))
	for _, p := range all {
		if p.Health.Enabled {
			result = append(result, p)
		}
	}
	return result
}

// ByRole returns enabled providers matching the given role, in deterministic order.
func (r *Registry) ByRole(role string) []Provider {
	enabled := r.Enabled()
	result := make([]Provider, 0)
	for _, p := range enabled {
		if p.HasRole(role) {
			result = append(result, p)
		}
	}
	return result
}

// ByCapability returns enabled providers matching the given capability, in deterministic order.
func (r *Registry) ByCapability(cap string) []Provider {
	enabled := r.Enabled()
	result := make([]Provider, 0)
	for _, p := range enabled {
		if p.HasCapability(cap) {
			result = append(result, p)
		}
	}
	return result
}

// Count returns the number of registered providers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers)
}
