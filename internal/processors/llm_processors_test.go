package processors

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/tiroq/arcanum/internal/prompts"
	"github.com/tiroq/arcanum/internal/providers"
)

type stubProvider struct {
	resp providers.GenerateResponse
	err  error
}

func (s *stubProvider) Name() string                        { return "stub" }
func (s *stubProvider) HealthCheck(_ context.Context) error { return nil }
func (s *stubProvider) Generate(_ context.Context, _ providers.GenerateRequest) (providers.GenerateResponse, error) {
	return s.resp, s.err
}

func TestLLMRewriteProcessor_ExecutionTracePassthrough(t *testing.T) {
	traceJSON := json.RawMessage(`{"trace_id":"abc","outcome":"success"}`)

	stub := &stubProvider{
		resp: providers.GenerateResponse{
			Content:        `{"title":"Better Title","description":"Better Desc","reasoning":"test"}`,
			Model:          "test-model",
			TokensTotal:    42,
			ExecutionTrace: traceJSON,
		},
	}

	reg := providers.NewProviderRegistry()
	reg.Register("stub", stub)

	loader := prompts.NewTemplateLoader("../../prompts")
	logger := zaptest.NewLogger(t)

	proc := NewLLMRewriteProcessor(reg, loader, logger, nil, "stub")

	payload, _ := json.Marshal(map[string]string{"title": "Old Title", "description": "Old Desc"})
	jc := JobContext{
		JobID:           uuid.New(),
		SourceTaskID:    uuid.New(),
		JobType:         "llm_rewrite",
		SnapshotPayload: payload,
	}

	result, err := proc.Process(context.Background(), jc)
	require.NoError(t, err)
	assert.Equal(t, "success", result.Outcome)
	assert.Equal(t, "test-model", result.ModelName)
	assert.Equal(t, json.RawMessage(traceJSON), result.ExecutionTrace)
}

func TestLLMRoutingProcessor_ExecutionTracePassthrough(t *testing.T) {
	traceJSON := json.RawMessage(`{"trace_id":"xyz","outcome":"fallback"}`)

	stub := &stubProvider{
		resp: providers.GenerateResponse{
			Content:        `{"suggested_list":"Work","confidence":0.95,"reasoning":"test"}`,
			Model:          "test-model",
			TokensTotal:    30,
			ExecutionTrace: traceJSON,
		},
	}

	reg := providers.NewProviderRegistry()
	reg.Register("stub", stub)

	loader := prompts.NewTemplateLoader("../../prompts")
	logger := zaptest.NewLogger(t)

	proc := NewLLMRoutingProcessor(reg, loader, logger, nil, "stub")

	payload, _ := json.Marshal(map[string]string{"title": "Buy groceries", "description": "Need milk and eggs"})
	jc := JobContext{
		JobID:           uuid.New(),
		SourceTaskID:    uuid.New(),
		JobType:         "llm_routing",
		SnapshotPayload: payload,
	}

	result, err := proc.Process(context.Background(), jc)
	require.NoError(t, err)
	assert.Equal(t, "success", result.Outcome)
	assert.Equal(t, "test-model", result.ModelName)
	assert.Equal(t, json.RawMessage(traceJSON), result.ExecutionTrace)
}

func TestLLMRewriteProcessor_NilTracePassthrough(t *testing.T) {
	stub := &stubProvider{
		resp: providers.GenerateResponse{
			Content:     `{"title":"Better","description":"Better","reasoning":"x"}`,
			Model:       "model",
			TokensTotal: 10,
		},
	}

	reg := providers.NewProviderRegistry()
	reg.Register("stub", stub)

	loader := prompts.NewTemplateLoader("../../prompts")
	proc := NewLLMRewriteProcessor(reg, loader, zaptest.NewLogger(t), nil, "stub")

	payload, _ := json.Marshal(map[string]string{"title": "T", "description": "D"})
	jc := JobContext{
		JobID:           uuid.New(),
		SourceTaskID:    uuid.New(),
		JobType:         "llm_rewrite",
		SnapshotPayload: payload,
	}

	result, err := proc.Process(context.Background(), jc)
	require.NoError(t, err)
	assert.Equal(t, "success", result.Outcome)
	assert.Nil(t, result.ExecutionTrace)
}
