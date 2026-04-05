package profile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProfile_SingleCandidate(t *testing.T) {
	candidates, err := ParseProfile("qwen2.5:7b-instruct?think=on&timeout=240&json=true")
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, "qwen2.5:7b-instruct", c.ModelName)
	assert.Equal(t, ThinkEnabled, c.ThinkMode)
	assert.Equal(t, 240*time.Second, c.Timeout)
	assert.True(t, c.JSONMode)
}

func TestParseProfile_MultipleCandidate(t *testing.T) {
	dsl := "qwen2.5:7b-instruct?think=on&timeout=240|llama3.2:3b?think=off&timeout=60"
	candidates, err := ParseProfile(dsl)
	require.NoError(t, err)
	require.Len(t, candidates, 2)

	assert.Equal(t, "qwen2.5:7b-instruct", candidates[0].ModelName)
	assert.Equal(t, ThinkEnabled, candidates[0].ThinkMode)
	assert.Equal(t, 240*time.Second, candidates[0].Timeout)

	assert.Equal(t, "llama3.2:3b", candidates[1].ModelName)
	assert.Equal(t, ThinkDisabled, candidates[1].ThinkMode)
	assert.Equal(t, 60*time.Second, candidates[1].Timeout)
}

func TestParseProfile_ModelNameOnly(t *testing.T) {
	candidates, err := ParseProfile("llama3.2:3b")
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	assert.Equal(t, "llama3.2:3b", candidates[0].ModelName)
	assert.Equal(t, ThinkDefault, candidates[0].ThinkMode)
	assert.Equal(t, time.Duration(0), candidates[0].Timeout)
	assert.False(t, candidates[0].JSONMode)
}

func TestParseProfile_WithSpaces(t *testing.T) {
	dsl := "  qwen2.5:7b-instruct?think=thinking&timeout=120 | llama3.2:3b  "
	candidates, err := ParseProfile(dsl)
	require.NoError(t, err)
	require.Len(t, candidates, 2)

	assert.Equal(t, "qwen2.5:7b-instruct", candidates[0].ModelName)
	assert.Equal(t, ThinkEnabled, candidates[0].ThinkMode)
	assert.Equal(t, 120*time.Second, candidates[0].Timeout)

	assert.Equal(t, "llama3.2:3b", candidates[1].ModelName)
}

func TestParseProfile_Empty(t *testing.T) {
	_, err := ParseProfile("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty profile DSL")
}

func TestParseProfile_InvalidThinkMode(t *testing.T) {
	_, err := ParseProfile("llama3.2:3b?think=bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid think mode")
}

func TestParseProfile_InvalidTimeout(t *testing.T) {
	_, err := ParseProfile("llama3.2:3b?timeout=abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout")
}

func TestParseProfile_InvalidJSON(t *testing.T) {
	_, err := ParseProfile("llama3.2:3b?json=maybe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid json value")
}

func TestParseProfile_UnknownOption(t *testing.T) {
	_, err := ParseProfile("llama3.2:3b?foo=bar")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown option")
}

func TestParseProfile_MissingEquals(t *testing.T) {
	_, err := ParseProfile("llama3.2:3b?think")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing '='")
}

func TestParseProfile_NegativeTimeout(t *testing.T) {
	_, err := ParseProfile("llama3.2:3b?timeout=-10")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be non-negative")
}

func TestParseProfile_EmptyModelName(t *testing.T) {
	_, err := ParseProfile("?think=thinking")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing model name")
}

func TestParseProfile_TrailingPipe(t *testing.T) {
	candidates, err := ParseProfile("llama3.2:3b|")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "llama3.2:3b", candidates[0].ModelName)
}

func TestParseProfileOrSingle_PlainModelName(t *testing.T) {
	candidates, err := ParseProfileOrSingle("llama3.2:3b")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "llama3.2:3b", candidates[0].ModelName)
	assert.Equal(t, ThinkDefault, candidates[0].ThinkMode)
}

func TestParseProfileOrSingle_DSL(t *testing.T) {
	candidates, err := ParseProfileOrSingle("qwen2.5:7b-instruct?think=on|llama3.2:3b")
	require.NoError(t, err)
	require.Len(t, candidates, 2)
}

func TestParseProfileOrSingle_WithParams(t *testing.T) {
	candidates, err := ParseProfileOrSingle("llama3.2:3b?think=on")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, ThinkEnabled, candidates[0].ThinkMode)
}

func TestParseProfileOrSingle_Empty(t *testing.T) {
	_, err := ParseProfileOrSingle("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty profile string")
}

func TestParseProfile_ThreeCandidate(t *testing.T) {
	dsl := "model-a?think=thinking&timeout=300|model-b?timeout=120|model-c?think=nothinking&json=true"
	candidates, err := ParseProfile(dsl)
	require.NoError(t, err)
	require.Len(t, candidates, 3)

	assert.Equal(t, "model-a", candidates[0].ModelName)
	assert.Equal(t, ThinkEnabled, candidates[0].ThinkMode)
	assert.Equal(t, 300*time.Second, candidates[0].Timeout)

	assert.Equal(t, "model-b", candidates[1].ModelName)
	assert.Equal(t, ThinkDefault, candidates[1].ThinkMode)
	assert.Equal(t, 120*time.Second, candidates[1].Timeout)

	assert.Equal(t, "model-c", candidates[2].ModelName)
	assert.Equal(t, ThinkDisabled, candidates[2].ThinkMode)
	assert.True(t, candidates[2].JSONMode)
}
func TestParseProfile_ProviderKey(t *testing.T) {
	// provider= sets the target backend; other fields still work alongside it.
	candidates, err := ParseProfile("gpt-4o-mini?provider=openrouter&timeout=60&json=true")
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	c := candidates[0]
	assert.Equal(t, "gpt-4o-mini", c.ModelName)
	assert.Equal(t, "openrouter", c.ProviderName)
	assert.Equal(t, 60*time.Second, c.Timeout)
	assert.True(t, c.JSONMode)
}

func TestParseProfile_ProviderKeyInChain(t *testing.T) {
	// Candidate chain with mixed providers.
	dsl := "qwen2.5:7b-instruct?provider=ollama&timeout=240|gpt-4o-mini?provider=openrouter&timeout=30"
	candidates, err := ParseProfile(dsl)
	require.NoError(t, err)
	require.Len(t, candidates, 2)

	assert.Equal(t, "ollama", candidates[0].ProviderName)
	assert.Equal(t, "openrouter", candidates[1].ProviderName)
	assert.Equal(t, "gpt-4o-mini", candidates[1].ModelName)
}

func TestParseProfile_EmptyProviderValue(t *testing.T) {
	_, err := ParseProfile("llama3.2:3b?provider=")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name must not be empty")
}
