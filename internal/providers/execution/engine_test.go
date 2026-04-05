package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

type mockProvider struct {
	name      string
	calls     []providers.GenerateRequest
	responses []providers.GenerateResponse
	errors    []error
	callIndex int
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Generate(_ context.Context, req providers.GenerateRequest) (providers.GenerateResponse, error) {
	m.calls = append(m.calls, req)
	idx := m.callIndex
	m.callIndex++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return providers.GenerateResponse{}, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return providers.GenerateResponse{
		Content: "default response",
		Model:   req.Model,
	}, nil
}

func (m *mockProvider) HealthCheck(_ context.Context) error { return nil }

func newTestEngine(mp *mockProvider) *ExecutionEngine {
	return NewExecutionEngine(mp, zap.NewNop())
}

func TestEngine_SingleCandidateSuccess(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		responses: []providers.GenerateResponse{
			{Content: `{"result": "ok"}`, Model: "model-a", TokensTotal: 100},
		},
	}
	engine := newTestEngine(mp)

	candidates := []profile.ModelCandidate{{ModelName: "model-a"}}
	req := providers.GenerateRequest{ModelRole: providers.RoleFast}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.Equal(t, OutcomeSuccess, result.Outcome)
	assert.Equal(t, `{"result": "ok"}`, result.Response.Content)
	assert.Equal(t, 0, result.Trace.WinnerIndex)
	require.Len(t, result.Trace.Attempts, 1)
	assert.Equal(t, "success", result.Trace.Attempts[0].Outcome)
	require.Len(t, mp.calls, 1)
	assert.Equal(t, "model-a", mp.calls[0].Model)
}

func TestEngine_FallbackToSecondCandidate(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		errors: []error{
			errors.New("ollama: unexpected status 500: internal error"),
			nil,
		},
		responses: []providers.GenerateResponse{
			{},
			{Content: "fallback response", Model: "model-b"},
		},
	}
	engine := newTestEngine(mp)
	engine.SetMaxRetries(0)

	candidates := []profile.ModelCandidate{
		{ModelName: "model-a"},
		{ModelName: "model-b"},
	}
	req := providers.GenerateRequest{ModelRole: providers.RoleDefault}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.Equal(t, OutcomeFallback, result.Outcome)
	assert.Equal(t, "fallback response", result.Response.Content)
	assert.Equal(t, 1, result.Trace.WinnerIndex)
	require.Len(t, result.Trace.Attempts, 2)
	assert.Equal(t, "failed", result.Trace.Attempts[0].Outcome)
	assert.Equal(t, FailureServerError, result.Trace.Attempts[0].FailureClass)
	assert.Equal(t, "success", result.Trace.Attempts[1].Outcome)
}

func TestEngine_AllCandidatesExhausted(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		errors: []error{
			errors.New("ollama: unexpected status 500: error"),
			errors.New("context deadline exceeded"),
		},
	}
	engine := newTestEngine(mp)
	engine.SetMaxRetries(0)

	candidates := []profile.ModelCandidate{
		{ModelName: "model-a"},
		{ModelName: "model-b"},
	}
	req := providers.GenerateRequest{ModelRole: providers.RoleFast}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exhausted all 2 candidates")

	assert.Equal(t, OutcomeExhausted, result.Outcome)
	assert.Equal(t, -1, result.Trace.WinnerIndex)
	require.Len(t, result.Trace.Attempts, 2)
}

func TestEngine_UnknownErrorFallsThrough(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		errors: []error{
			errors.New("response blocked by content filter"),
			nil,
		},
		responses: []providers.GenerateResponse{
			{},
			{Content: "recovered", Model: "model-b"},
		},
	}
	engine := newTestEngine(mp)
	engine.SetMaxRetries(0)

	candidates := []profile.ModelCandidate{
		{ModelName: "model-a"},
		{ModelName: "model-b"},
	}
	req := providers.GenerateRequest{}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.Equal(t, OutcomeFallback, result.Outcome)
	assert.Equal(t, FailureUnknown, result.Trace.Attempts[0].FailureClass)
	assert.Equal(t, "recovered", result.Response.Content)
}

func TestEngine_RetryOnConnection(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		errors: []error{
			errors.New("dial tcp: connection refused"),
			nil,
		},
		responses: []providers.GenerateResponse{
			{},
			{Content: "recovered", Model: "model-a"},
		},
	}
	engine := newTestEngine(mp)
	engine.SetMaxRetries(1)

	candidates := []profile.ModelCandidate{{ModelName: "model-a"}}
	req := providers.GenerateRequest{}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.Equal(t, OutcomeSuccess, result.Outcome)
	assert.Equal(t, "recovered", result.Response.Content)
	require.Len(t, mp.calls, 2)
	require.Len(t, result.Trace.Attempts, 2)
	assert.Equal(t, "failed", result.Trace.Attempts[0].Outcome)
	assert.Equal(t, "success", result.Trace.Attempts[1].Outcome)
}

func TestEngine_RetryExhaustedThenFallback(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		errors: []error{
			errors.New("dial tcp: connection refused"),
			errors.New("dial tcp: connection refused"),
			nil,
		},
		responses: []providers.GenerateResponse{
			{}, {}, {Content: "fallback", Model: "model-b"},
		},
	}
	engine := newTestEngine(mp)
	engine.SetMaxRetries(1)

	candidates := []profile.ModelCandidate{
		{ModelName: "model-a"},
		{ModelName: "model-b"},
	}
	req := providers.GenerateRequest{}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.Equal(t, OutcomeFallback, result.Outcome)
	assert.Equal(t, 1, result.Trace.WinnerIndex)
	require.Len(t, mp.calls, 3)
}

func TestEngine_ValidationFailureFallsBack(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		responses: []providers.GenerateResponse{
			{Content: "not json", Model: "model-a"},
			{Content: `{"valid": true}`, Model: "model-b"},
		},
	}
	engine := newTestEngine(mp)

	candidates := []profile.ModelCandidate{
		{ModelName: "model-a"},
		{ModelName: "model-b"},
	}
	req := providers.GenerateRequest{}

	policy := ValidationPolicy{
		Validators: []Validator{JSONValidator{}},
		FailAction: ActionNextCandidate,
	}

	result, err := engine.Execute(context.Background(), req, candidates, policy)
	require.NoError(t, err)

	assert.Equal(t, OutcomeFallback, result.Outcome)
	assert.Equal(t, `{"valid": true}`, result.Response.Content)
	require.Len(t, result.Trace.Attempts, 2)
	assert.Equal(t, FailureValidation, result.Trace.Attempts[0].FailureClass)
}

func TestEngine_CandidateOverridesRequestFields(t *testing.T) {
	mp := &mockProvider{
		name:      "test",
		responses: []providers.GenerateResponse{{Content: "ok", Model: "specific-model"}},
	}
	engine := newTestEngine(mp)

	candidates := []profile.ModelCandidate{{
		ModelName: "specific-model",
		ThinkMode: profile.ThinkEnabled,
		Timeout:   300 * time.Second,
		JSONMode:  true,
	}}
	req := providers.GenerateRequest{
		Model:     "should-be-overridden",
		ThinkMode: "should-be-overridden",
		Timeout:   60 * time.Second,
		JSONMode:  false,
	}

	_, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	require.Len(t, mp.calls, 1)
	call := mp.calls[0]
	assert.Equal(t, "specific-model", call.Model)
	assert.Equal(t, "thinking", call.ThinkMode)
	assert.Equal(t, 300*time.Second, call.Timeout)
	assert.True(t, call.JSONMode)
}

func TestEngine_NoCandidatesError(t *testing.T) {
	mp := &mockProvider{name: "test"}
	engine := newTestEngine(mp)

	_, err := engine.Execute(context.Background(), providers.GenerateRequest{}, nil, DefaultValidationPolicy())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no candidates provided")
}

func TestEngine_TraceHasValidIDs(t *testing.T) {
	mp := &mockProvider{
		name:      "test",
		responses: []providers.GenerateResponse{{Content: "ok"}},
	}
	engine := newTestEngine(mp)

	candidates := []profile.ModelCandidate{{ModelName: "model-a"}}
	req := providers.GenerateRequest{ModelRole: providers.RolePlanner}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.NotEmpty(t, result.Trace.TraceID)
	assert.Equal(t, "planner", result.Trace.Role)
	assert.False(t, result.Trace.StartedAt.IsZero())
	assert.False(t, result.Trace.FinishedAt.IsZero())
	assert.True(t, result.Trace.TotalDurationMS >= 0)
}

func TestEngine_ThreeCandidateChain(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		errors: []error{
			errors.New("context deadline exceeded"),
			errors.New("ollama: unexpected status 404: model not found"),
			nil,
		},
		responses: []providers.GenerateResponse{
			{}, {}, {Content: "third try", Model: "model-c"},
		},
	}
	engine := newTestEngine(mp)
	engine.SetMaxRetries(0)

	candidates := []profile.ModelCandidate{
		{ModelName: "model-a", ThinkMode: profile.ThinkEnabled, Timeout: 300 * time.Second},
		{ModelName: "model-b"},
		{ModelName: "model-c", ThinkMode: profile.ThinkDisabled},
	}
	req := providers.GenerateRequest{ModelRole: providers.RoleReview}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.Equal(t, OutcomeFallback, result.Outcome)
	assert.Equal(t, 2, result.Trace.WinnerIndex)
	require.Len(t, result.Trace.Attempts, 3)
	assert.Equal(t, FailureTimeout, result.Trace.Attempts[0].FailureClass)
	assert.Equal(t, FailureServerError, result.Trace.Attempts[1].FailureClass)
	assert.Equal(t, "success", result.Trace.Attempts[2].Outcome)
}

func TestEngine_SetMaxRetriesNegative(t *testing.T) {
	mp := &mockProvider{name: "test"}
	engine := newTestEngine(mp)
	engine.SetMaxRetries(-5)
	assert.Equal(t, 0, engine.maxRetries)
}

func TestEngine_ValidationAbort(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		responses: []providers.GenerateResponse{
			{Content: "not json", Model: "model-a"},
			{Content: "also not json", Model: "model-b"},
		},
	}
	engine := newTestEngine(mp)

	candidates := []profile.ModelCandidate{
		{ModelName: "model-a"},
		{ModelName: "model-b"},
	}

	policy := ValidationPolicy{
		Validators: []Validator{JSONValidator{}},
		FailAction: ActionAbort,
	}

	result, err := engine.Execute(context.Background(), providers.GenerateRequest{}, candidates, policy)
	require.Error(t, err)

	// ActionAbort means stop immediately on first validation failure — no fallback to next candidate
	assert.Equal(t, OutcomeAborted, result.Outcome)
	require.Len(t, result.Trace.Attempts, 1)
	assert.Equal(t, "model-a", result.Trace.Attempts[0].ModelName)
}

func TestEngine_CrossProviderRouting(t *testing.T) {
	// Primary provider (ollama) fails; second candidate routes to secondary (openrouter)
	// via the registry. The engine must call the correct provider per candidate.
	ollama := &mockProvider{
		name:   "ollama",
		errors: []error{errors.New("ollama: unexpected status 503: service unavailable")},
	}
	openrouter := &mockProvider{
		name: "openrouter",
		responses: []providers.GenerateResponse{
			{Content: "from openrouter", Model: "gpt-4o-mini", Provider: "openrouter"},
		},
	}

	reg := providers.NewProviderRegistry()
	reg.Register("ollama", ollama)
	reg.Register("openrouter", openrouter)

	engine := NewExecutionEngine(ollama, zap.NewNop()).WithRegistry(reg)
	engine.SetMaxRetries(0)

	candidates := []profile.ModelCandidate{
		{ModelName: "qwen2.5:7b-instruct", ProviderName: "ollama"},
		{ModelName: "gpt-4o-mini", ProviderName: "openrouter"},
	}
	req := providers.GenerateRequest{ModelRole: providers.RoleDefault}

	result, err := engine.Execute(context.Background(), req, candidates, DefaultValidationPolicy())
	require.NoError(t, err)

	assert.Equal(t, OutcomeFallback, result.Outcome)
	assert.Equal(t, "from openrouter", result.Response.Content)

	// Ollama was tried once (failed), openrouter once (succeeded)
	assert.Len(t, ollama.calls, 1)
	assert.Len(t, openrouter.calls, 1)
	assert.Equal(t, "qwen2.5:7b-instruct", ollama.calls[0].Model)
	assert.Equal(t, "gpt-4o-mini", openrouter.calls[0].Model)

	// Trace records provider names per attempt
	require.Len(t, result.Trace.Attempts, 2)
	assert.Equal(t, "ollama", result.Trace.Attempts[0].ProviderName)
	assert.Equal(t, "openrouter", result.Trace.Attempts[1].ProviderName)
}

func TestEngine_CrossProviderMissingFromRegistry(t *testing.T) {
	// Candidate references a provider not in registry → engine falls back to primary.
	ollama := &mockProvider{
		name:      "ollama",
		responses: []providers.GenerateResponse{{Content: "from primary fallback", Model: "qwen"}},
	}

	reg := providers.NewProviderRegistry()
	reg.Register("ollama", ollama)

	engine := NewExecutionEngine(ollama, zap.NewNop()).WithRegistry(reg)

	candidates := []profile.ModelCandidate{
		{ModelName: "gpt-4o", ProviderName: "nonexistent"},
	}

	result, err := engine.Execute(context.Background(), providers.GenerateRequest{}, candidates, DefaultValidationPolicy())
	require.NoError(t, err)
	assert.Equal(t, OutcomeSuccess, result.Outcome)
	assert.Equal(t, "from primary fallback", result.Response.Content)
	// Primary (ollama) was called because "nonexistent" was not found in registry
	assert.Len(t, ollama.calls, 1)
}
