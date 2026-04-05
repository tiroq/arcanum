package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

func testMetrics(t *testing.T) *metrics.Metrics {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	m, err := metrics.NewMetrics(reg)
	require.NoError(t, err)
	return m
}

func TestExecutingProvider_Name(t *testing.T) {
	mp := &mockProvider{name: "ollama"}
	ep := NewExecutingProvider(mp, nil, nil, zaptest.NewLogger(t))
	assert.Equal(t, "ollama", ep.Name())
}

func TestExecutingProvider_HealthCheck(t *testing.T) {
	mp := &mockProvider{name: "ollama"}
	ep := NewExecutingProvider(mp, nil, nil, zaptest.NewLogger(t))
	assert.NoError(t, ep.HealthCheck(context.Background()))
}

func TestExecutingProvider_Passthrough(t *testing.T) {
	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: "hello", Model: "qwen2.5"},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "qwen2.5"},
		},
	}
	m := testMetrics(t)
	ep := NewExecutingProvider(mp, profiles, m, zaptest.NewLogger(t))

	resp, err := ep.Generate(context.Background(), providers.GenerateRequest{
		ModelRole: providers.RoleDefault,
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Content)
	assert.Nil(t, ep.LastTrace(), "passthrough should not produce a trace")
	assert.Nil(t, resp.ExecutionTrace, "passthrough should not produce trace in response")
}

func TestExecutingProvider_CandidateChainFallback(t *testing.T) {
	mp := &mockProvider{
		name: "ollama",
		errors: []error{
			fmt.Errorf("connection refused"),
			fmt.Errorf("connection refused"),
			nil,
		},
		responses: []providers.GenerateResponse{
			{},
			{},
			{Content: "from fallback", Model: "qwen2.5:1.5b"},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleFast: []profile.ModelCandidate{
			{ModelName: "qwen2.5", ThinkMode: profile.ThinkEnabled},
			{ModelName: "qwen2.5:1.5b"},
		},
	}
	m := testMetrics(t)
	ep := NewExecutingProvider(mp, profiles, m, zaptest.NewLogger(t))

	resp, err := ep.Generate(context.Background(), providers.GenerateRequest{
		ModelRole: providers.RoleFast,
	})
	require.NoError(t, err)
	assert.Equal(t, "from fallback", resp.Content)

	trace := ep.LastTrace()
	require.NotNil(t, trace)
	assert.Equal(t, OutcomeFallback, trace.Outcome)
	require.NotNil(t, resp.ExecutionTrace, "response should carry trace JSON")
}

func TestExecutingProvider_ResponseTraceIsValidJSON(t *testing.T) {
	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: `{"ok":true}`, Model: "model-a"},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "model-a", ThinkMode: profile.ThinkEnabled},
		},
	}
	ep := NewExecutingProvider(mp, profiles, nil, zaptest.NewLogger(t))

	resp, err := ep.Generate(context.Background(), providers.GenerateRequest{
		ModelRole: providers.RoleDefault,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.ExecutionTrace)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.ExecutionTrace, &parsed))
	assert.NotEmpty(t, parsed["trace_id"])
	assert.Equal(t, "success", parsed["outcome"])
	attempts, ok := parsed["attempts"].([]interface{})
	require.True(t, ok)
	assert.Len(t, attempts, 1)
}

func TestExecutingProvider_NoCandidatesError(t *testing.T) {
	mp := &mockProvider{name: "ollama"}
	profiles := profile.RoleProfiles{}
	ep := NewExecutingProvider(mp, profiles, nil, zaptest.NewLogger(t))

	_, err := ep.Generate(context.Background(), providers.GenerateRequest{
		ModelRole: providers.RolePlanner,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no candidates")
}

func TestExecutingProvider_JSONModeValidation(t *testing.T) {
	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: "not json", Model: "model-a"},
			{Content: `{"ok":true}`, Model: "model-b"},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "model-a", JSONMode: true},
			{ModelName: "model-b", JSONMode: true},
		},
	}
	m := testMetrics(t)
	ep := NewExecutingProvider(mp, profiles, m, zaptest.NewLogger(t))

	resp, err := ep.Generate(context.Background(), providers.GenerateRequest{
		ModelRole: providers.RoleDefault,
	})
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, resp.Content)

	trace := ep.LastTrace()
	require.NotNil(t, trace)
	assert.Equal(t, OutcomeFallback, trace.Outcome)
}

func TestExecutingProvider_LastTraceOverwrittenPerCall(t *testing.T) {
	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: "first", Model: "a"},
			{Content: "second", Model: "a"},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleFast: []profile.ModelCandidate{
			{ModelName: "a", ThinkMode: profile.ThinkEnabled},
		},
	}
	ep := NewExecutingProvider(mp, profiles, nil, zaptest.NewLogger(t))

	_, err := ep.Generate(context.Background(), providers.GenerateRequest{ModelRole: providers.RoleFast})
	require.NoError(t, err)
	trace1 := ep.LastTrace()
	require.NotNil(t, trace1)

	_, err = ep.Generate(context.Background(), providers.GenerateRequest{ModelRole: providers.RoleFast})
	require.NoError(t, err)
	trace2 := ep.LastTrace()
	require.NotNil(t, trace2)

	assert.NotEqual(t, trace1.TraceID, trace2.TraceID)
}

func TestExecutingProvider_PassthroughBypassedByReqJSONMode(t *testing.T) {
	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: `{"ok":true}`, Model: "model-a"},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "model-a"},
		},
	}
	ep := NewExecutingProvider(mp, profiles, nil, zaptest.NewLogger(t))

	resp, err := ep.Generate(context.Background(), providers.GenerateRequest{
		ModelRole: providers.RoleDefault,
		JSONMode:  true,
	})
	require.NoError(t, err)
	assert.NotNil(t, ep.LastTrace(), "should go through engine when req.JSONMode=true")
	assert.NotNil(t, resp.ExecutionTrace, "response should carry trace when engine is used")
}

func TestExecutingProvider_TraceOnErrorPath(t *testing.T) {
	mp := &mockProvider{
		name: "ollama",
		errors: []error{
			fmt.Errorf("ollama: unexpected status 500: internal error"),
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleFast: []profile.ModelCandidate{
			{ModelName: "model-a", ThinkMode: profile.ThinkEnabled},
		},
	}
	ep := NewExecutingProvider(mp, profiles, nil, zaptest.NewLogger(t))

	resp, err := ep.Generate(context.Background(), providers.GenerateRequest{
		ModelRole: providers.RoleFast,
	})
	require.Error(t, err)
	assert.NotNil(t, resp.ExecutionTrace, "error response should still carry execution trace")

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.ExecutionTrace, &parsed))
	assert.NotEmpty(t, parsed["trace_id"])
}

// ─── token metric tests ───────────────────────────────────────────────────────

// readCounter reads the current value of a labeled counter from a registry.
func readCounter(t *testing.T, reg *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			match := true
			for _, lp := range m.GetLabel() {
				if expected, ok := labels[lp.GetName()]; ok && lp.GetValue() != expected {
					match = false
					break
				}
			}
			if match {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func TestExecutingProvider_TokenMetrics_Passthrough(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	m, err := metrics.NewMetrics(reg)
	require.NoError(t, err)

	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: "hi", Model: "qwen3:1.7b", TokensPrompt: 15, TokensCompletion: 7, TokensTotal: 22},
		},
	}
	// Single candidate with no think/JSON overrides → passthrough path.
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "qwen3:1.7b"},
		},
	}
	ep := NewExecutingProvider(mp, profiles, m, zaptest.NewLogger(t))

	_, err = ep.Generate(context.Background(), providers.GenerateRequest{ModelRole: providers.RoleDefault})
	require.NoError(t, err)

	labels := prometheus.Labels{"provider": "ollama", "model": "qwen3:1.7b", "role": "default"}
	assert.Equal(t, float64(15), readCounter(t, reg, "runeforge_tokens_prompt_total", labels))
	assert.Equal(t, float64(7), readCounter(t, reg, "runeforge_tokens_completion_total", labels))
	assert.Equal(t, float64(22), readCounter(t, reg, "runeforge_tokens_total", labels))
}

func TestExecutingProvider_TokenMetrics_ChainSuccess(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	m, err := metrics.NewMetrics(reg)
	require.NoError(t, err)

	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: "result", Model: "qwen3:1.7b", TokensPrompt: 30, TokensCompletion: 12, TokensTotal: 42},
		},
	}
	// Two candidates + think mode → goes through engine.
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "qwen3:1.7b", ThinkMode: profile.ThinkEnabled},
			{ModelName: "qwen3:8b"},
		},
	}
	ep := NewExecutingProvider(mp, profiles, m, zaptest.NewLogger(t))

	_, err = ep.Generate(context.Background(), providers.GenerateRequest{ModelRole: providers.RoleDefault})
	require.NoError(t, err)

	labels := prometheus.Labels{"provider": "ollama", "model": "qwen3:1.7b", "role": "default"}
	assert.Equal(t, float64(30), readCounter(t, reg, "runeforge_tokens_prompt_total", labels))
	assert.Equal(t, float64(12), readCounter(t, reg, "runeforge_tokens_completion_total", labels))
	assert.Equal(t, float64(42), readCounter(t, reg, "runeforge_tokens_total", labels))
}

func TestExecutingProvider_TokenMetrics_FallbackAccumulates(t *testing.T) {
	// First candidate exhausts its retries (2 connection_refused), then falls back
	// to the second candidate which succeeds. Only the successful attempt reports tokens.
	reg := prometheus.NewPedanticRegistry()
	m, err := metrics.NewMetrics(reg)
	require.NoError(t, err)

	mp := &mockProvider{
		name: "ollama",
		errors: []error{
			fmt.Errorf("connection refused"), // candidate 0, attempt 1
			fmt.Errorf("connection refused"), // candidate 0, retry 1 (maxRetries=1)
			nil,                              // candidate 1, attempt 1 — success
		},
		responses: []providers.GenerateResponse{
			{},
			{},
			{Content: "ok", Model: "qwen3:8b", TokensPrompt: 20, TokensCompletion: 5, TokensTotal: 25},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "qwen3:1.7b", ThinkMode: profile.ThinkEnabled},
			{ModelName: "qwen3:8b", ThinkMode: profile.ThinkEnabled},
		},
	}
	ep := NewExecutingProvider(mp, profiles, m, zaptest.NewLogger(t))

	_, err = ep.Generate(context.Background(), providers.GenerateRequest{ModelRole: providers.RoleDefault})
	require.NoError(t, err)

	// First model failed twice with connection errors — no LLM work done, zero tokens.
	first := prometheus.Labels{"provider": "ollama", "model": "qwen3:1.7b", "role": "default"}
	assert.Equal(t, float64(0), readCounter(t, reg, "runeforge_tokens_total", first))

	// Second model succeeded — should have its tokens.
	second := prometheus.Labels{"provider": "ollama", "model": "qwen3:8b", "role": "default"}
	assert.Equal(t, float64(20), readCounter(t, reg, "runeforge_tokens_prompt_total", second))
	assert.Equal(t, float64(5), readCounter(t, reg, "runeforge_tokens_completion_total", second))
	assert.Equal(t, float64(25), readCounter(t, reg, "runeforge_tokens_total", second))
}

func TestExecutingProvider_TokenMetrics_ValidationFailureCounted(t *testing.T) {
	// First candidate returns non-JSON (validation fails) with tokens.
	// Second candidate succeeds. Both should report tokens since both did work.
	reg := prometheus.NewPedanticRegistry()
	m, err := metrics.NewMetrics(reg)
	require.NoError(t, err)

	mp := &mockProvider{
		name: "ollama",
		responses: []providers.GenerateResponse{
			{Content: "not-json", Model: "qwen3:1.7b", TokensPrompt: 10, TokensCompletion: 3, TokensTotal: 13},
			{Content: `{"ok":true}`, Model: "qwen3:8b", TokensPrompt: 18, TokensCompletion: 8, TokensTotal: 26},
		},
	}
	profiles := profile.RoleProfiles{
		providers.RoleDefault: []profile.ModelCandidate{
			{ModelName: "qwen3:1.7b", JSONMode: true},
			{ModelName: "qwen3:8b", JSONMode: true},
		},
	}
	ep := NewExecutingProvider(mp, profiles, m, zaptest.NewLogger(t))

	_, err = ep.Generate(context.Background(), providers.GenerateRequest{ModelRole: providers.RoleDefault})
	require.NoError(t, err)

	// First candidate failed validation but still did work — tokens should be counted.
	first := prometheus.Labels{"provider": "ollama", "model": "qwen3:1.7b", "role": "default"}
	assert.Equal(t, float64(10), readCounter(t, reg, "runeforge_tokens_prompt_total", first))
	assert.Equal(t, float64(3), readCounter(t, reg, "runeforge_tokens_completion_total", first))
	assert.Equal(t, float64(13), readCounter(t, reg, "runeforge_tokens_total", first))

	// Second candidate succeeded.
	second := prometheus.Labels{"provider": "ollama", "model": "qwen3:8b", "role": "default"}
	assert.Equal(t, float64(18), readCounter(t, reg, "runeforge_tokens_prompt_total", second))
	assert.Equal(t, float64(26), readCounter(t, reg, "runeforge_tokens_total", second))
}
