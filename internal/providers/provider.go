package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// GenerateRequest holds all parameters for a generation call.
type GenerateRequest struct {
	Model                 string
	ModelRole             ModelRole
	SystemPrompt          string
	UserPrompt            string
	Temperature           float64
	MaxTokens             int
	TopP                  float64
	JSONMode              bool
	Timeout               time.Duration
	PromptTemplateID      string
	PromptTemplateVersion string
	InputVariables        map[string]string
	ThinkMode             string
}

// GenerateResponse holds the LLM output.
type GenerateResponse struct {
	Content          string
	Model            string
	ModelRole        string
	Provider         string
	TokensPrompt     int
	TokensCompletion int
	TokensTotal      int
	DurationMS       int64
	TimeoutUsed      time.Duration
	ThinkingContent  string
	ExecutionTrace   json.RawMessage
	// UsedFallback is true when the provider resolved the requested ModelRole to
	// the default model because no role-specific model was configured.
	// Only populated by providers that implement role resolution (e.g. Ollama).
	UsedFallback bool
	// ErrorClass is populated on failure; empty string on success.
	// Carries the classified failure reason (timeout, rate_limit, validation, etc.)
	// so callers can route, audit, and log errors without re-inspecting the error string.
	ErrorClass string
}

// Provider is the LLM provider interface.
type Provider interface {
	Name() string
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
	HealthCheck(ctx context.Context) error
}

// ProviderRegistry holds named providers.
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewProviderRegistry creates an empty ProviderRegistry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: make(map[string]Provider)}
}

// Register adds a provider under the given name.
func (r *ProviderRegistry) Register(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
}

// Get returns the provider registered under name, or an error if not found.
func (r *ProviderRegistry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}
	return p, nil
}

// All returns a copy of all registered providers.
func (r *ProviderRegistry) All() map[string]Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Provider, len(r.providers))
	for k, v := range r.providers {
		out[k] = v
	}
	return out
}
