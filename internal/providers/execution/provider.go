package execution

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

// ExecutingProvider wraps a Provider with candidate-chain execution, fallback,
// validation, and trace recording. It implements the providers.Provider interface
// so processors can use it as a drop-in replacement.
type ExecutingProvider struct {
	inner    providers.Provider
	profiles profile.RoleProfiles
	engine   *ExecutionEngine
	metrics  *metrics.Metrics
	logger   *zap.Logger

	mu        sync.Mutex
	lastTrace *ExecutionTrace
}

// NewExecutingProvider creates a provider that runs candidate chains for each role.
func NewExecutingProvider(
	inner providers.Provider,
	profiles profile.RoleProfiles,
	m *metrics.Metrics,
	logger *zap.Logger,
) *ExecutingProvider {
	engine := NewExecutionEngine(inner, logger)
	return &ExecutingProvider{
		inner:    inner,
		profiles: profiles,
		engine:   engine,
		metrics:  m,
		logger:   logger,
	}
}

func (p *ExecutingProvider) Name() string {
	return p.inner.Name()
}

func (p *ExecutingProvider) HealthCheck(ctx context.Context) error {
	return p.inner.HealthCheck(ctx)
}

func (p *ExecutingProvider) Generate(ctx context.Context, req providers.GenerateRequest) (providers.GenerateResponse, error) {
	candidates := p.profiles.CandidatesForRole(req.ModelRole)
	if len(candidates) == 0 {
		return providers.GenerateResponse{}, fmt.Errorf("no candidates for role %q", req.ModelRole)
	}

	if len(candidates) == 1 && candidates[0].ThinkMode == profile.ThinkDefault && !candidates[0].JSONMode && !req.JSONMode && candidates[0].Timeout == 0 {
		resp, err := p.inner.Generate(ctx, req)
		p.recordPassthrough(req, resp, err)
		return resp, err
	}

	validation := ValidationPolicy{}
	if candidates[0].JSONMode || req.JSONMode {
		validation.Validators = append(validation.Validators, JSONValidator{})
		validation.FailAction = ActionNextCandidate
	}

	result, err := p.engine.Execute(ctx, req, candidates, validation)

	p.mu.Lock()
	p.lastTrace = result.Trace
	p.mu.Unlock()

	p.recordMetrics(req, result)

	if err != nil {
		resp := providers.GenerateResponse{}
		if result.Trace != nil {
			if traceJSON, jErr := result.Trace.ToJSON(); jErr == nil {
				resp.ExecutionTrace = traceJSON
			}
		}
		return resp, err
	}

	resp := result.Response
	if result.Trace != nil {
		if traceJSON, jErr := result.Trace.ToJSON(); jErr == nil {
			resp.ExecutionTrace = traceJSON
		} else {
			p.logger.Warn("failed to serialize execution trace", zap.Error(jErr))
		}
	}
	return resp, nil
}

// LastTrace returns the execution trace from the most recent Generate call.
// Returns nil if no execution has been performed or the last call was a passthrough.
func (p *ExecutingProvider) LastTrace() *ExecutionTrace {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastTrace
}

func (p *ExecutingProvider) recordPassthrough(req providers.GenerateRequest, resp providers.GenerateResponse, err error) {
	if p.metrics == nil {
		return
	}
	role := req.ModelRole.String()
	if err != nil {
		p.metrics.ExecutionOutcomeTotal.WithLabelValues(role, string(OutcomeExhausted)).Inc()
	} else {
		p.metrics.ExecutionOutcomeTotal.WithLabelValues(role, string(OutcomeSuccess)).Inc()
		// Record token usage for the passthrough call.
		if resp.TokensTotal > 0 {
			p.metrics.TokensPromptTotal.WithLabelValues(p.inner.Name(), resp.Model, role).Add(float64(resp.TokensPrompt))
			p.metrics.TokensCompletionTotal.WithLabelValues(p.inner.Name(), resp.Model, role).Add(float64(resp.TokensCompletion))
			p.metrics.TokensGrandTotal.WithLabelValues(p.inner.Name(), resp.Model, role).Add(float64(resp.TokensTotal))
		}
	}
	p.metrics.ExecutionCandidatesTried.WithLabelValues(role).Add(1)
}

func (p *ExecutingProvider) recordMetrics(req providers.GenerateRequest, result ExecuteResult) {
	if p.metrics == nil || result.Trace == nil {
		return
	}
	role := req.ModelRole.String()

	p.metrics.ExecutionOutcomeTotal.WithLabelValues(role, string(result.Outcome)).Inc()
	p.metrics.ExecutionCandidatesTried.WithLabelValues(role).Add(float64(len(result.Trace.Attempts)))
	p.metrics.ExecutionDuration.WithLabelValues(role).Observe(float64(result.Trace.TotalDurationMS) / 1000.0)

	for _, attempt := range result.Trace.Attempts {
		if attempt.Outcome == "failed" && attempt.FallbackAction == ActionNextCandidate {
			p.metrics.ExecutionFallbacksTotal.WithLabelValues(role, attempt.FailureClass.String()).Inc()
		}
		if attempt.FailureClass == FailureValidation {
			p.metrics.ExecutionValidationFailures.WithLabelValues(role, "chain").Inc()
		}
		// Increment token metrics per-attempt so per-model accounting is accurate
		// even across fallback chains (different models may be tried).
		if attempt.TokensTotal > 0 {
			p.metrics.TokensPromptTotal.WithLabelValues(p.inner.Name(), attempt.ModelName, role).Add(float64(attempt.TokensPrompt))
			p.metrics.TokensCompletionTotal.WithLabelValues(p.inner.Name(), attempt.ModelName, role).Add(float64(attempt.TokensCompletion))
			p.metrics.TokensGrandTotal.WithLabelValues(p.inner.Name(), attempt.ModelName, role).Add(float64(attempt.TokensTotal))
		}
	}
}
