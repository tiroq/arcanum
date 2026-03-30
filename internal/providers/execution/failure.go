// Package execution implements the execution engine for candidate chains,
// including failure classification, fallback policies, execution tracing,
// and output validation.
package execution

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// FailureClass categorizes the type of failure encountered during a candidate attempt.
type FailureClass string

const (
	// FailureTimeout indicates the request exceeded its deadline.
	FailureTimeout FailureClass = "timeout"
	// FailureRateLimit indicates the provider returned a rate-limit response (429).
	FailureRateLimit FailureClass = "rate_limit"
	// FailureServerError indicates a provider-side error (5xx, internal error).
	FailureServerError FailureClass = "server_error"
	// FailureConnectionRefused indicates a network-level connectivity failure.
	FailureConnectionRefused FailureClass = "connection_refused"
	// FailureInvalidResponse indicates the response could not be parsed or was malformed.
	FailureInvalidResponse FailureClass = "invalid_response"
	// FailureValidation indicates the response failed output validation.
	FailureValidation FailureClass = "validation"
	// FailureContextOverflow indicates the request exceeded the model's context window.
	FailureContextOverflow FailureClass = "context_overflow"
	// FailureUnknown indicates an unclassifiable error.
	FailureUnknown FailureClass = "unknown"
)

// ValidFailureClasses lists all recognized FailureClass values.
var ValidFailureClasses = []FailureClass{
	FailureTimeout, FailureRateLimit, FailureServerError, FailureConnectionRefused,
	FailureInvalidResponse, FailureValidation, FailureContextOverflow, FailureUnknown,
}

// String returns the string representation of the FailureClass.
func (f FailureClass) String() string {
	return string(f)
}

// FallbackAction specifies what the execution engine should do after a failure.
type FallbackAction string

const (
	// ActionNextCandidate moves to the next candidate in the chain.
	ActionNextCandidate FallbackAction = "next_candidate"
	// ActionRetry retries the same candidate (up to a retry limit).
	ActionRetry FallbackAction = "retry"
	// ActionAbort stops execution immediately and returns the error.
	ActionAbort FallbackAction = "abort"
)

// DefaultFallbackAction returns the default fallback action for a given failure class.
// This encodes the policy: which failures are retryable, which should fallback, and
// which should abort immediately.
func DefaultFallbackAction(fc FailureClass) FallbackAction {
	switch fc {
	case FailureTimeout:
		return ActionNextCandidate
	case FailureRateLimit:
		return ActionRetry
	case FailureServerError:
		return ActionNextCandidate
	case FailureConnectionRefused:
		return ActionRetry
	case FailureInvalidResponse:
		return ActionNextCandidate
	case FailureValidation:
		return ActionNextCandidate
	case FailureContextOverflow:
		return ActionNextCandidate
	default:
		return ActionNextCandidate
	}
}

// ClassifyError inspects an error and returns the appropriate FailureClass.
func ClassifyError(err error) FailureClass {
	if err == nil {
		return FailureUnknown
	}

	var ve *ValidationError
	if asValidationError(err, &ve) {
		return FailureValidation
	}

	msg := err.Error()

	// Check for context deadline/timeout.
	if err == context.DeadlineExceeded || strings.Contains(msg, "context deadline exceeded") {
		return FailureTimeout
	}
	if err == context.Canceled || strings.Contains(msg, "context canceled") {
		return FailureTimeout
	}

	// Check for network errors.
	var netErr net.Error
	if isNetError(err, &netErr) {
		if netErr.Timeout() {
			return FailureTimeout
		}
		return FailureConnectionRefused
	}
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") {
		return FailureConnectionRefused
	}

	// Check for rate limiting (HTTP 429).
	if strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests") {
		return FailureRateLimit
	}

	// Check for context overflow (model context window exceeded).
	if strings.Contains(msg, "context length") || strings.Contains(msg, "context window") || strings.Contains(msg, "too long") {
		return FailureContextOverflow
	}

	// Check for invalid/malformed responses (decode failures, unexpected format).
	if strings.Contains(msg, "decode response") || strings.Contains(msg, "unmarshal") || strings.Contains(msg, "invalid response") {
		return FailureInvalidResponse
	}

	// Check for server errors (5xx).
	if strings.Contains(msg, "500") || strings.Contains(msg, "502") || strings.Contains(msg, "503") ||
		strings.Contains(msg, "unexpected status 5") || strings.Contains(msg, "404") ||
		strings.Contains(msg, "model") && strings.Contains(msg, "not found") {
		return FailureServerError
	}

	return FailureUnknown
}

// isNetError checks if an error (or any in its chain) implements net.Error.
func isNetError(err error, target *net.Error) bool {
	for err != nil {
		if ne, ok := err.(net.Error); ok {
			*target = ne
			return true
		}
		unwrapped := errors_Unwrap(err)
		if unwrapped == err {
			break
		}
		err = unwrapped
	}
	return false
}

// asValidationError checks if an error (or any in its chain) is a *ValidationError.
func asValidationError(err error, target **ValidationError) bool {
	for err != nil {
		if ve, ok := err.(*ValidationError); ok {
			*target = ve
			return true
		}
		unwrapped := errors_Unwrap(err)
		if unwrapped == err {
			break
		}
		err = unwrapped
	}
	return false
}

// errors_Unwrap is a local helper to avoid importing errors package just for Unwrap.
func errors_Unwrap(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return err
	}
	return u.Unwrap()
}

// ValidationError is returned when a response fails output validation.
// It always classifies as FailureValidation.
type ValidationError struct {
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %s", e.Reason)
}
