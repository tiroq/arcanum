package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutionOutcome_String(t *testing.T) {
	assert.Equal(t, "success", OutcomeSuccess.String())
	assert.Equal(t, "fallback", OutcomeFallback.String())
	assert.Equal(t, "exhausted", OutcomeExhausted.String())
	assert.Equal(t, "aborted", OutcomeAborted.String())
}

func TestExecutionOutcome_IsTerminal(t *testing.T) {
	for _, o := range ValidExecutionOutcomes {
		assert.True(t, o.IsTerminal(), "expected %q to be terminal", o)
	}
	assert.False(t, ExecutionOutcome("in_progress").IsTerminal())
}

func TestExecutionOutcome_IsSuccess(t *testing.T) {
	assert.True(t, OutcomeSuccess.IsSuccess())
	assert.True(t, OutcomeFallback.IsSuccess())
	assert.False(t, OutcomeExhausted.IsSuccess())
	assert.False(t, OutcomeAborted.IsSuccess())
}
