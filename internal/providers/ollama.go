package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/config"
)

// OllamaProvider implements Provider for a local Ollama instance with role-based model selection.
type OllamaProvider struct {
	name   string
	cfg    config.OllamaConfig
	apiKey string // optional; non-empty → sets Authorization: Bearer header on every request
	client *http.Client
	logger *zap.Logger
}

// NewOllamaProvider creates a new OllamaProvider with role-based configuration.
func NewOllamaProvider(name string, cfg config.OllamaConfig, logger *zap.Logger) *OllamaProvider {
	return &OllamaProvider{
		name:   name,
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		logger: logger,
	}
}

// NewOllamaCloudProvider creates an OllamaProvider configured for Ollama Cloud.
// The cloud provider uses Bearer token authentication and a flat timeout (no
// role-based model resolution — models are specified directly in profile DSL
// candidates via "model?provider=ollama-cloud&timeout=N").
func NewOllamaCloudProvider(name string, cfg config.OllamaCloudConfig, logger *zap.Logger) *OllamaProvider {
	ollamaCfg := config.OllamaConfig{
		BaseURL:        cfg.BaseURL,
		DefaultModel:   "", // not used; models are always specified per-candidate in profiles
		TimeoutSeconds: cfg.TimeoutSeconds,
		Timeout:        cfg.Timeout,
	}
	return &OllamaProvider{
		name:   name,
		cfg:    ollamaCfg,
		apiKey: cfg.APIKey,
		client: &http.Client{Timeout: cfg.Timeout},
		logger: logger,
	}
}

// Name returns the provider name.
func (p *OllamaProvider) Name() string { return p.name }

// Config returns the provider's Ollama configuration (for diagnostics).
func (p *OllamaProvider) Config() config.OllamaConfig { return p.cfg }

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Think    *bool           `json:"think,omitempty"`
}

type ollamaChatResponse struct {
	Model           string        `json:"model"`
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

// ResolveModel returns the model name for a given role, falling back to DefaultModel.
func (p *OllamaProvider) ResolveModel(role ModelRole) string {
	return p.cfg.ResolveModel(role.String())
}

// ResolveTimeout returns the timeout for a given role, falling back to the default timeout.
func (p *OllamaProvider) ResolveTimeout(role ModelRole) time.Duration {
	return p.cfg.ResolveTimeout(role.String())
}

// Generate calls Ollama's /api/chat endpoint synchronously.
// If req.ModelRole is set and req.Model is empty, the model is resolved from configuration.
func (p *OllamaProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	role := req.ModelRole
	if role == "" {
		role = RoleDefault
	}

	model := req.Model
	usedFallback := false
	if model == "" {
		model = p.ResolveModel(role)
		if role != RoleDefault && model == p.cfg.DefaultModel {
			usedFallback = true
		}
	}

	timeout := p.ResolveTimeout(role)
	if req.Timeout > 0 {
		timeout = req.Timeout
	}

	p.logger.Debug("ollama role resolution",
		zap.String("provider", p.name),
		zap.String("role", role.String()),
		zap.String("resolved_model", model),
		zap.Duration("resolved_timeout", timeout),
		zap.Bool("used_fallback", usedFallback),
	)

	messages := make([]ollamaMessage, 0, 2)
	if req.SystemPrompt != "" {
		messages = append(messages, ollamaMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, ollamaMessage{Role: "user", Content: req.UserPrompt})

	body := ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}

	switch req.ThinkMode {
	case "thinking", "think":
		think := true
		body.Think = &think
	case "nothinking", "nothink":
		think := false
		body.Think = &think
	}

	data, err := json.Marshal(body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, p.cfg.BaseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("%s: create request: %w", p.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("%s: execute request: %w", p.name, err)
	}
	defer httpResp.Body.Close()

	durationMS := time.Since(start).Milliseconds()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("%s: read response: %w", p.name, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return GenerateResponse{}, fmt.Errorf("%s: unexpected status %d: %s", p.name, httpResp.StatusCode, string(raw))
	}

	var resp ollamaChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return GenerateResponse{}, fmt.Errorf("%s: decode response: %w", p.name, err)
	}

	tokensPrompt := resp.PromptEvalCount
	tokensCompletion := resp.EvalCount

	p.logger.Debug("ollama call complete",
		zap.String("provider", p.name),
		zap.String("model_role", role.String()),
		zap.String("model", model),
		zap.Int64("duration_ms", durationMS),
		zap.Int("tokens_prompt", tokensPrompt),
		zap.Int("tokens_completion", tokensCompletion),
	)

	return GenerateResponse{
		Content:          resp.Message.Content,
		Model:            resp.Model,
		ModelRole:        role.String(),
		Provider:         p.name,
		TokensPrompt:     tokensPrompt,
		TokensCompletion: tokensCompletion,
		TokensTotal:      tokensPrompt + tokensCompletion,
		DurationMS:       durationMS,
		TimeoutUsed:      timeout,
	}, nil
}

// HealthCheck verifies Ollama is reachable via /api/tags.
func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.BaseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("%s: health check: %w", p.name, err)
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: health check: %w", p.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: health check: unexpected status %d", p.name, resp.StatusCode)
	}
	return nil
}

// DiagnosticInfo returns diagnostic information about the Ollama provider configuration.
func (p *OllamaProvider) DiagnosticInfo() map[string]interface{} {
	info := map[string]interface{}{
		"base_url":        p.cfg.BaseURL,
		"default_model":   p.cfg.DefaultModel,
		"default_timeout": p.cfg.Timeout.String(),
	}
	if p.cfg.FastModel != "" {
		info["fast_model"] = p.cfg.FastModel
	} else {
		info["fast_model"] = p.cfg.DefaultModel + " (fallback)"
	}
	if p.cfg.PlannerModel != "" {
		info["planner_model"] = p.cfg.PlannerModel
	} else {
		info["planner_model"] = p.cfg.DefaultModel + " (fallback)"
	}
	if p.cfg.ReviewModel != "" {
		info["review_model"] = p.cfg.ReviewModel
	} else {
		info["review_model"] = p.cfg.DefaultModel + " (fallback)"
	}
	if p.cfg.FastTimeoutSeconds > 0 {
		info["fast_timeout"] = p.cfg.FastTimeout.String()
	} else {
		info["fast_timeout"] = p.cfg.Timeout.String() + " (fallback)"
	}
	if p.cfg.PlannerTimeoutSeconds > 0 {
		info["planner_timeout"] = p.cfg.PlannerTimeout.String()
	} else {
		info["planner_timeout"] = p.cfg.Timeout.String() + " (fallback)"
	}
	return info
}
