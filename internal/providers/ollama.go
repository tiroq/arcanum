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
)

// OllamaProvider implements Provider for a local Ollama instance.
type OllamaProvider struct {
	name    string
	baseURL string
	client  *http.Client
	logger  *zap.Logger
}

// NewOllamaProvider creates a new OllamaProvider.
func NewOllamaProvider(name, baseURL string, timeout time.Duration, logger *zap.Logger) *OllamaProvider {
	return &OllamaProvider{
		name:    name,
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
		logger:  logger,
	}
}

// Name returns the provider name.
func (p *OllamaProvider) Name() string { return p.name }

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaChatResponse struct {
	Model   string        `json:"model"`
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// Generate calls Ollama's /api/chat endpoint synchronously.
func (p *OllamaProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	messages := make([]ollamaMessage, 0, 2)
	if req.SystemPrompt != "" {
		messages = append(messages, ollamaMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, ollamaMessage{Role: "user", Content: req.UserPrompt})

	body := ollamaChatRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("%s: create request: %w", p.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

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

	p.logger.Debug("ollama call complete",
		zap.String("provider", p.name),
		zap.String("model", req.Model),
		zap.Int64("duration_ms", durationMS),
	)

	return GenerateResponse{
		Content:    resp.Message.Content,
		Model:      resp.Model,
		Provider:   p.name,
		DurationMS: durationMS,
	}, nil
}

// HealthCheck verifies Ollama is reachable via /api/tags.
func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("%s: health check: %w", p.name, err)
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
