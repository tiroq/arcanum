package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

const defaultMaxRetries = 1

// ExecutionEngine executes a candidate chain against a Provider, handling
// fallback, retry, validation, and trace recording.
type ExecutionEngine struct {
	provider   providers.Provider
	logger     *zap.Logger
	maxRetries int
}

// NewExecutionEngine creates an engine that delegates to the given provider.
func NewExecutionEngine(provider providers.Provider, logger *zap.Logger) *ExecutionEngine {
	return &ExecutionEngine{
		provider:   provider,
		logger:     logger,
		maxRetries: defaultMaxRetries,
	}
}

// SetMaxRetries overrides the per-candidate retry limit (default 1).
func (e *ExecutionEngine) SetMaxRetries(n int) {
	if n < 0 {
		n = 0
	}
	e.maxRetries = n
}

// ExecuteResult holds the output of a candidate chain execution.
type ExecuteResult struct {
	Response providers.GenerateResponse
	Trace    *ExecutionTrace
	Outcome  ExecutionOutcome
}

// Execute runs the candidate chain for the given request, trying each candidate
// in order with fallback and retry logic. The request's Model, ThinkMode, Timeout,
// and JSONMode fields are overridden per-candidate.
func (e *ExecutionEngine) Execute(
	ctx context.Context,
	req providers.GenerateRequest,
	candidates []profile.ModelCandidate,
	validation ValidationPolicy,
) (ExecuteResult, error) {
	if len(candidates) == 0 {
		return ExecuteResult{}, fmt.Errorf("execution engine: no candidates provided")
	}

	traceID := uuid.New().String()
	trace := NewExecutionTrace(traceID, string(req.ModelRole))

	var lastErr error

	for i, candidate := range candidates {
		retries := 0
		for {
			resp, err := e.tryCandidate(ctx, req, candidate, i, trace, validation)
			if err == nil {
				outcome := OutcomeSuccess
				if i > 0 {
					outcome = OutcomeFallback
				}
				trace.Finalize(outcome, i)

				return ExecuteResult{
					Response: resp,
					Trace:    trace,
					Outcome:  outcome,
				}, nil
			}

			lastErr = err
			fc := ClassifyError(err)
			action := DefaultFallbackAction(fc)

			if fc == FailureValidation {
				action = validation.FailAction
			}

			if action == ActionAbort {
				trace.Finalize(OutcomeAborted, -1)
				return ExecuteResult{
					Trace:   trace,
					Outcome: OutcomeAborted,
				}, fmt.Errorf("execution aborted at candidate %d (%s): %w", i, candidate.ModelName, err)
			}

			if action == ActionRetry && retries < e.maxRetries {
				retries++
				e.logger.Debug("retrying candidate",
					zap.Int("candidate_index", i),
					zap.String("model", candidate.ModelName),
					zap.Int("retry", retries),
					zap.String("failure_class", fc.String()),
				)
				continue
			}

			e.logger.Debug("moving to next candidate",
				zap.Int("candidate_index", i),
				zap.String("model", candidate.ModelName),
				zap.String("failure_class", fc.String()),
				zap.String("action", string(action)),
			)
			break
		}
	}

	trace.Finalize(OutcomeExhausted, -1)
	return ExecuteResult{
		Trace:   trace,
		Outcome: OutcomeExhausted,
	}, fmt.Errorf("execution exhausted all %d candidates: last error: %w", len(candidates), lastErr)
}

func (e *ExecutionEngine) tryCandidate(
	ctx context.Context,
	req providers.GenerateRequest,
	candidate profile.ModelCandidate,
	index int,
	trace *ExecutionTrace,
	validation ValidationPolicy,
) (providers.GenerateResponse, error) {
	startedAt := time.Now().UTC()
	attempt := NewCandidateAttempt(index, candidate, startedAt)

	candidateReq := req
	candidateReq.Model = candidate.ModelName
	candidateReq.ThinkMode = string(candidate.ThinkMode)
	if candidate.Timeout > 0 {
		candidateReq.Timeout = candidate.Timeout
	}
	if candidate.JSONMode {
		candidateReq.JSONMode = true
	}

	resp, err := e.provider.Generate(ctx, candidateReq)
	finishedAt := time.Now().UTC()

	if err != nil {
		fc := ClassifyError(err)
		action := DefaultFallbackAction(fc)
		attempt.CompleteWithError(err, fc, action, finishedAt)
		trace.RecordAttempt(attempt)
		return providers.GenerateResponse{}, err
	}

	if valErr := validation.Run(resp.Content); valErr != nil {
		fc := FailureValidation
		action := validation.FailAction
		attempt.CompleteWithError(valErr, fc, action, finishedAt)
		trace.RecordAttempt(attempt)
		return providers.GenerateResponse{}, valErr
	}

	attempt.Complete("success", finishedAt)
	attempt.TokensPrompt = resp.TokensPrompt
	attempt.TokensTotal = resp.TokensTotal
	trace.RecordAttempt(attempt)

	return resp, nil
}
