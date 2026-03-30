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
