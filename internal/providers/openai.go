package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// OpenAIProvider implements Provider for OpenAI-compatible APIs (OpenAI, OpenRouter, etc.).
type OpenAIProvider struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *zap.Logger
}

// NewOpenAIProvider creates a new OpenAIProvider.
func NewOpenAIProvider(name, baseURL, apiKey string, timeout time.Duration, logger *zap.Logger) *OpenAIProvider {
	return &OpenAIProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
		logger:  logger,
	}
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string { return p.name }

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type string `json:"type"`
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Temperature    float64               `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	TopP           float64               `json:"top_p,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIResponse struct {
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Generate calls the chat completions endpoint with exponential backoff retry.
func (p *OpenAIProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	messages := make([]openAIMessage, 0, 2)
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: req.UserPrompt})

	body := openAIRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
	}
	if req.JSONMode {
		body.ResponseFormat = &openAIResponseFormat{Type: "json_object"}
	}

	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(math.Pow(2, float64(attempt-1)) * float64(time.Second))
			select {
			case <-ctx.Done():
				return GenerateResponse{}, ctx.Err()
			case <-time.After(wait):
			}
		}

		start := time.Now()
		resp, statusCode, err := p.doRequest(ctx, body)
		durationMS := time.Since(start).Milliseconds()

		if err != nil {
			lastErr = err
			if statusCode == http.StatusTooManyRequests || (statusCode >= 500 && statusCode < 600) {
				p.logger.Debug("provider retryable error",
					zap.String("provider", p.name),
					zap.Int("status", statusCode),
					zap.Int("attempt", attempt+1),
				)
				continue
			}
			return GenerateResponse{}, fmt.Errorf("%s: %w", p.name, err)
		}

		content := ""
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}

		p.logger.Debug("provider call complete",
			zap.String("provider", p.name),
			zap.String("model", req.Model),
			zap.Int64("duration_ms", durationMS),
			zap.Int("tokens_total", resp.Usage.TotalTokens),
		)

		return GenerateResponse{
			Content:          content,
			Model:            resp.Model,
			ModelRole:        string(req.ModelRole),
			Provider:         p.name,
			TokensPrompt:     resp.Usage.PromptTokens,
			TokensCompletion: resp.Usage.CompletionTokens,
			TokensTotal:      resp.Usage.TotalTokens,
			DurationMS:       durationMS,
		}, nil
	}
	return GenerateResponse{}, fmt.Errorf("%s: max retries exceeded: %w", p.name, lastErr)
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body openAIRequest) (*openAIResponse, int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, httpResp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, httpResp.StatusCode, fmt.Errorf("unexpected status %d: %s", httpResp.StatusCode, string(raw))
	}

	var resp openAIResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, httpResp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return nil, httpResp.StatusCode, fmt.Errorf("api error: %s", resp.Error.Message)
	}
	return &resp, httpResp.StatusCode, nil
}

// HealthCheck verifies the provider is reachable by making a minimal request.
func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("%s: health check: %w", p.name, err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
