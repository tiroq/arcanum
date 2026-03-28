package processors

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/prompts"
)

// LLMRoutingProcessor classifies tasks and suggests a target list/project.
type LLMRoutingProcessor struct {
	providers       *providers.ProviderRegistry
	prompts         *prompts.TemplateLoader
	logger          *zap.Logger
	metrics         *metrics.Metrics
	defaultProvider string
	defaultModel    string
}

// NewLLMRoutingProcessor creates a new LLMRoutingProcessor.
func NewLLMRoutingProcessor(
	providerReg *providers.ProviderRegistry,
	templateLoader *prompts.TemplateLoader,
	logger *zap.Logger,
	m *metrics.Metrics,
	defaultProvider, defaultModel string,
) *LLMRoutingProcessor {
	return &LLMRoutingProcessor{
		providers:       providerReg,
		prompts:         templateLoader,
		logger:          logger,
		metrics:         m,
		defaultProvider: defaultProvider,
		defaultModel:    defaultModel,
	}
}

func (p *LLMRoutingProcessor) Name() string    { return "llm_routing" }
func (p *LLMRoutingProcessor) Version() string { return "v1" }

func (p *LLMRoutingProcessor) CanHandle(jobType string) bool {
	return jobType == "llm_routing"
}

type routingInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type routingOutput struct {
	SuggestedList string  `json:"suggested_list"`
	Confidence    float64 `json:"confidence"`
	Reasoning     string  `json:"reasoning"`
}

// Process calls the LLM to suggest a target list/project for the task.
func (p *LLMRoutingProcessor) Process(ctx context.Context, jc JobContext) (ProcessResult, error) {
	var input routingInput
	if len(jc.SnapshotPayload) > 0 {
		if err := json.Unmarshal(jc.SnapshotPayload, &input); err != nil {
			return ProcessResult{Outcome: "error", ErrorMessage: "unmarshal input: " + err.Error()}, nil
		}
	}

	const templateID = "routing"
	const templateVersion = "v1"

	tpl, err := p.prompts.Load(templateID, templateVersion)
	if err != nil {
		return ProcessResult{Outcome: "error", ErrorMessage: fmt.Sprintf("load template: %v", err)}, nil
	}

	vars := map[string]string{
		"title":       input.Title,
		"description": input.Description,
	}
	userPrompt, err := p.prompts.Render(tpl, vars)
	if err != nil {
		return ProcessResult{Outcome: "error", ErrorMessage: fmt.Sprintf("render template: %v", err)}, nil
	}

	provider, err := p.providers.Get(p.defaultProvider)
	if err != nil {
		return ProcessResult{Outcome: "error", ErrorMessage: fmt.Sprintf("get provider: %v", err)}, nil
	}

	start := time.Now()
	genResp, err := provider.Generate(ctx, providers.GenerateRequest{
		Model:                 p.defaultModel,
		SystemPrompt:          tpl.SystemPrompt,
		UserPrompt:            userPrompt,
		Temperature:           0.2,
		MaxTokens:             512,
		JSONMode:              true,
		PromptTemplateID:      templateID,
		PromptTemplateVersion: templateVersion,
	})
	durationMS := time.Since(start).Milliseconds()

	if err != nil {
		if p.metrics != nil {
			p.metrics.ProviderFailures.WithLabelValues(p.defaultProvider).Inc()
		}
		return ProcessResult{Outcome: "error", ErrorMessage: fmt.Sprintf("generate: %v", err)}, nil
	}

	if p.metrics != nil {
		p.metrics.ProviderCalls.WithLabelValues(p.defaultProvider).Inc()
		p.metrics.TokensUsed.WithLabelValues(p.defaultProvider).Add(float64(genResp.TokensTotal))
	}

	var out routingOutput
	if err := json.Unmarshal([]byte(genResp.Content), &out); err != nil {
		p.logger.Warn("failed to parse routing JSON output", zap.String("content", genResp.Content), zap.Error(err))
		out = routingOutput{SuggestedList: "inbox", Confidence: 0.0, Reasoning: "parse error"}
	}

	outPayload, _ := json.Marshal(out)

	return ProcessResult{
		ProposalType:          "routing",
		OutputPayload:         outPayload,
		HumanReviewRequired:   out.Confidence < 0.8,
		Outcome:               "success",
		PromptTemplateID:      templateID,
		PromptTemplateVersion: templateVersion,
		ModelProvider:         p.defaultProvider,
		ModelName:             p.defaultModel,
		TokensUsed:            genResp.TokensTotal,
		DurationMS:            durationMS,
	}, nil
}
