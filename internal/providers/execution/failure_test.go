package execution

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFailureClass_String(t *testing.T) {
	assert.Equal(t, "timeout", FailureTimeout.String())
	assert.Equal(t, "rate_limit", FailureRateLimit.String())
	assert.Equal(t, "server_error", FailureServerError.String())
	assert.Equal(t, "connection_refused", FailureConnectionRefused.String())
	assert.Equal(t, "invalid_response", FailureInvalidResponse.String())
	assert.Equal(t, "validation", FailureValidation.String())
	assert.Equal(t, "context_overflow", FailureContextOverflow.String())
	assert.Equal(t, "unknown", FailureUnknown.String())
}

func TestDefaultFallbackAction(t *testing.T) {
	tests := []struct {
		fc   FailureClass
		want FallbackAction
	}{
		{FailureTimeout, ActionNextCandidate},
		{FailureRateLimit, ActionRetry},
		{FailureServerError, ActionNextCandidate},
		{FailureConnectionRefused, ActionRetry},
		{FailureInvalidResponse, ActionNextCandidate},
		{FailureValidation, ActionNextCandidate},
		{FailureContextOverflow, ActionNextCandidate},
		{FailureUnknown, ActionNextCandidate},
	}
	for _, tt := range tests {
		t.Run(tt.fc.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, DefaultFallbackAction(tt.fc))
		})
	}
}

func TestClassifyError_Nil(t *testing.T) {
	assert.Equal(t, FailureUnknown, ClassifyError(nil))
}

func TestClassifyError_DeadlineExceeded(t *testing.T) {
	assert.Equal(t, FailureTimeout, ClassifyError(context.DeadlineExceeded))
}

func TestClassifyError_ContextCanceled(t *testing.T) {
	assert.Equal(t, FailureTimeout, ClassifyError(context.Canceled))
}

func TestClassifyError_WrappedDeadline(t *testing.T) {
	err := fmt.Errorf("ollama: execute request: %w", context.DeadlineExceeded)
	assert.Equal(t, FailureTimeout, ClassifyError(err))
}

func TestClassifyError_ConnectionRefused(t *testing.T) {
	err := errors.New("dial tcp 127.0.0.1:11434: connection refused")
	assert.Equal(t, FailureConnectionRefused, ClassifyError(err))
}

func TestClassifyError_NoSuchHost(t *testing.T) {
	err := errors.New("dial tcp: lookup badhost: no such host")
	assert.Equal(t, FailureConnectionRefused, ClassifyError(err))
}

func TestClassifyError_RateLimit_429(t *testing.T) {
	err := errors.New("ollama: unexpected status 429: too many requests")
	assert.Equal(t, FailureRateLimit, ClassifyError(err))
}

func TestClassifyError_RateLimit_Message(t *testing.T) {
	err := errors.New("rate limit exceeded, please retry later")
	assert.Equal(t, FailureRateLimit, ClassifyError(err))
}

func TestClassifyError_ServerError_404(t *testing.T) {
	err := errors.New("ollama: unexpected status 404: model not found")
	assert.Equal(t, FailureServerError, ClassifyError(err))
}

func TestClassifyError_ServerError_500(t *testing.T) {
	err := errors.New("ollama: unexpected status 500: internal server error")
	assert.Equal(t, FailureServerError, ClassifyError(err))
}

func TestClassifyError_ServerError_503(t *testing.T) {
	err := errors.New("ollama: unexpected status 503: service unavailable")
	assert.Equal(t, FailureServerError, ClassifyError(err))
}

func TestClassifyError_ContextOverflow(t *testing.T) {
	err := errors.New("model context length exceeded: input is 8192 tokens but max is 4096")
	assert.Equal(t, FailureContextOverflow, ClassifyError(err))
}

func TestClassifyError_ContextOverflow_Window(t *testing.T) {
	err := errors.New("request exceeds context window limit")
	assert.Equal(t, FailureContextOverflow, ClassifyError(err))
}

func TestClassifyError_InvalidResponse(t *testing.T) {
	err := errors.New("ollama: decode response: unexpected EOF")
	assert.Equal(t, FailureInvalidResponse, ClassifyError(err))
}

func TestClassifyError_InvalidResponse_Unmarshal(t *testing.T) {
	err := errors.New("json unmarshal error in response body")
	assert.Equal(t, FailureInvalidResponse, ClassifyError(err))
}

func TestClassifyError_ContentFilter_NowUnknown(t *testing.T) {
	// Content filter errors no longer have a dedicated class; they classify as unknown.
	err := errors.New("response blocked by content filter")
	assert.Equal(t, FailureUnknown, ClassifyError(err))
}

func TestClassifyError_Unknown(t *testing.T) {
	err := errors.New("something unexpected happened")
	assert.Equal(t, FailureUnknown, ClassifyError(err))
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{Reason: "missing required field"}
	assert.Equal(t, "validation failed: missing required field", err.Error())
}

func TestClassifyError_ValidationError(t *testing.T) {
	err := &ValidationError{Reason: "invalid JSON"}
	assert.Equal(t, FailureValidation, ClassifyError(err))
}
