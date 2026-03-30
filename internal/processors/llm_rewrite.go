package processors

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/prompts"
	"github.com/tiroq/arcanum/internal/providers"
)

// LLMRewriteProcessor rewrites task titles/descriptions using an LLM.
type LLMRewriteProcessor struct {
	providers       *providers.ProviderRegistry
	prompts         *prompts.TemplateLoader
	logger          *zap.Logger
	metrics         *metrics.Metrics
	defaultProvider string
	modelRole       providers.ModelRole
}

// NewLLMRewriteProcessor creates a new LLMRewriteProcessor.
func NewLLMRewriteProcessor(
	providerReg *providers.ProviderRegistry,
	templateLoader *prompts.TemplateLoader,
	logger *zap.Logger,
	m *metrics.Metrics,
	defaultProvider string,
) *LLMRewriteProcessor {
	return &LLMRewriteProcessor{
		providers:       providerReg,
		prompts:         templateLoader,
		logger:          logger,
		metrics:         m,
		defaultProvider: defaultProvider,
		modelRole:       providers.RoleDefault,
	}
}

func (p *LLMRewriteProcessor) Name() string    { return "llm_rewrite" }
func (p *LLMRewriteProcessor) Version() string { return "v1" }

func (p *LLMRewriteProcessor) CanHandle(jobType string) bool {
	return jobType == "llm_rewrite"
}

type rewriteInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type rewriteOutput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Reasoning   string `json:"reasoning"`
}

// Process loads a prompt template and calls the LLM provider to rewrite content.
func (p *LLMRewriteProcessor) Process(ctx context.Context, jc JobContext) (ProcessResult, error) {
	var input rewriteInput
	if len(jc.SnapshotPayload) > 0 {
		if err := json.Unmarshal(jc.SnapshotPayload, &input); err != nil {
			return ProcessResult{Outcome: "error", ErrorMessage: "unmarshal input: " + err.Error()}, nil
		}
	}

	const templateID = "task_title_rewrite"
	const templateVersion = "v1"

	tpl, err := p.prompts.Load(templateID, templateVersion)
	if err != nil {
		return ProcessResult{Outcome: "error", ErrorMessage: fmt.Sprintf("load template: %v", err)}, nil
	}

	vars := map[string]string{
		"Title":       input.Title,
		"Description": input.Description,
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
		ModelRole:             p.modelRole,
		SystemPrompt:          tpl.SystemPrompt,
		UserPrompt:            userPrompt,
		Temperature:           0.3,
		MaxTokens:             1024,
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

	var out rewriteOutput
	if err := json.Unmarshal([]byte(genResp.Content), &out); err != nil {
		p.logger.Warn("failed to parse LLM JSON output", zap.String("content", genResp.Content), zap.Error(err))
		out = rewriteOutput{Title: input.Title, Description: input.Description}
	}

	outPayload, _ := json.Marshal(out)

	return ProcessResult{
		ProposalType:          "rewrite",
		OutputPayload:         outPayload,
		HumanReviewRequired:   true,
		Outcome:               "success",
		PromptTemplateID:      templateID,
		PromptTemplateVersion: templateVersion,
		ModelProvider:         p.defaultProvider,
		ModelRole:             p.modelRole,
		ModelName:             genResp.Model,
		TokensUsed:            genResp.TokensTotal,
		DurationMS:            durationMS,
		TimeoutUsed:           genResp.TimeoutUsed,
		ExecutionTrace:        genResp.ExecutionTrace,
	}, nil
}
