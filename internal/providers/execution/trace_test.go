package execution

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

func TestNewExecutionTrace(t *testing.T) {
	tr := NewExecutionTrace("trace-001", "fast")

	assert.Equal(t, "trace-001", tr.TraceID)
	assert.Equal(t, "fast", tr.Role)
	assert.Empty(t, tr.Attempts)
	assert.Equal(t, -1, tr.WinnerIndex)
	assert.False(t, tr.StartedAt.IsZero())
}

func TestExecutionTrace_RecordAttempt(t *testing.T) {
	tr := NewExecutionTrace("trace-002", "default")

	attempt := CandidateAttempt{
		CandidateIndex: 0,
		ModelName:      "llama3.2:3b",
		ThinkMode:      "default",
		Outcome:        "success",
	}
	tr.RecordAttempt(attempt)

	require.Len(t, tr.Attempts, 1)
	assert.Equal(t, "llama3.2:3b", tr.Attempts[0].ModelName)
}

func TestExecutionTrace_Finalize(t *testing.T) {
	tr := NewExecutionTrace("trace-003", "planner")

	tr.RecordAttempt(CandidateAttempt{CandidateIndex: 0, Outcome: "failed"})
	tr.RecordAttempt(CandidateAttempt{CandidateIndex: 1, Outcome: "success"})
	tr.Finalize(OutcomeFallback, 1)

	assert.Equal(t, OutcomeFallback, tr.Outcome)
	assert.Equal(t, 1, tr.WinnerIndex)
	assert.False(t, tr.FinishedAt.IsZero())
	assert.True(t, tr.TotalDurationMS >= 0)
}

func TestExecutionTrace_ToJSON(t *testing.T) {
	tr := NewExecutionTrace("trace-004", "review")
	tr.RecordAttempt(CandidateAttempt{
		CandidateIndex: 0,
		ModelName:      "qwen2.5:7b-instruct",
		Outcome:        "success",
	})
	tr.Finalize(OutcomeSuccess, 0)

	data, err := tr.ToJSON()
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "trace-004", parsed["trace_id"])
	assert.Equal(t, "success", parsed["outcome"])
}

func TestNewCandidateAttempt(t *testing.T) {
	candidate := profile.ModelCandidate{
		ModelName: "llama3.2:3b",
		ThinkMode: profile.ThinkEnabled,
	}
	start := time.Now().UTC()
	attempt := NewCandidateAttempt(2, candidate, start)

	assert.Equal(t, 2, attempt.CandidateIndex)
	assert.Equal(t, "llama3.2:3b", attempt.ModelName)
	assert.Equal(t, "thinking", attempt.ThinkMode)
	assert.Equal(t, start, attempt.StartedAt)
}

func TestCandidateAttempt_Complete(t *testing.T) {
	start := time.Now().UTC()
	attempt := CandidateAttempt{
		CandidateIndex: 0,
		ModelName:      "test-model",
		StartedAt:      start,
	}

	finish := start.Add(150 * time.Millisecond)
	attempt.Complete("success", finish)

	assert.Equal(t, "success", attempt.Outcome)
	assert.Equal(t, finish, attempt.FinishedAt)
	assert.True(t, attempt.DurationMS >= 0)
}

func TestCandidateAttempt_CompleteWithError(t *testing.T) {
	start := time.Now().UTC()
	attempt := CandidateAttempt{
		CandidateIndex: 0,
		ModelName:      "test-model",
		StartedAt:      start,
	}

	finish := start.Add(2 * time.Second)
	err := errors.New("connection refused")
	attempt.CompleteWithError(err, FailureConnectionRefused, ActionRetry, finish)

	assert.Equal(t, "failed", attempt.Outcome)
	assert.Equal(t, FailureConnectionRefused, attempt.FailureClass)
	assert.Equal(t, ActionRetry, attempt.FallbackAction)
	assert.Equal(t, "connection refused", attempt.ErrorMessage)
	assert.Equal(t, finish, attempt.FinishedAt)
}

func TestCandidateAttempt_CompleteWithError_NilError(t *testing.T) {
	start := time.Now().UTC()
	attempt := CandidateAttempt{
		CandidateIndex: 0,
		ModelName:      "test-model",
		StartedAt:      start,
	}

	finish := start.Add(1 * time.Second)
	attempt.CompleteWithError(nil, FailureValidation, ActionNextCandidate, finish)

	assert.Equal(t, "failed", attempt.Outcome)
	assert.Empty(t, attempt.ErrorMessage)
}

func TestExecutionTrace_FullLifecycle(t *testing.T) {
	tr := NewExecutionTrace("trace-full", "fast")

	c0 := profile.ModelCandidate{ModelName: "model-a", ThinkMode: profile.ThinkEnabled}
	c1 := profile.ModelCandidate{ModelName: "model-b", ThinkMode: profile.ThinkDisabled}

	start0 := time.Now().UTC()
	a0 := NewCandidateAttempt(0, c0, start0)
	a0.CompleteWithError(errors.New("timeout"), FailureTimeout, ActionNextCandidate, start0.Add(3*time.Second))
	tr.RecordAttempt(a0)

	start1 := time.Now().UTC()
	a1 := NewCandidateAttempt(1, c1, start1)
	a1.Complete("success", start1.Add(500*time.Millisecond))
	a1.TokensPrompt = 100
	a1.TokensTotal = 250
	tr.RecordAttempt(a1)

	tr.Finalize(OutcomeFallback, 1)

	assert.Equal(t, OutcomeFallback, tr.Outcome)
	assert.Equal(t, 1, tr.WinnerIndex)
	require.Len(t, tr.Attempts, 2)
	assert.Equal(t, "failed", tr.Attempts[0].Outcome)
	assert.Equal(t, "success", tr.Attempts[1].Outcome)
	assert.Equal(t, 250, tr.Attempts[1].TokensTotal)

	data, err := tr.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}
