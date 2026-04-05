package execution

import (
	"encoding/json"
	"time"

	"github.com/tiroq/arcanum/internal/providers/profile"
)

// CandidateAttempt records the result of trying a single candidate in the chain.
type CandidateAttempt struct {
	CandidateIndex   int            `json:"candidate_index"`
	ModelName        string         `json:"model_name"`
	ThinkMode        string         `json:"think_mode"`
	StartedAt        time.Time      `json:"started_at"`
	FinishedAt       time.Time      `json:"finished_at"`
	DurationMS       int64          `json:"duration_ms"`
	Outcome          string         `json:"outcome"`
	FailureClass     FailureClass   `json:"failure_class,omitempty"`
	FallbackAction   FallbackAction `json:"fallback_action,omitempty"`
	ErrorMessage     string         `json:"error_message,omitempty"`
	TokensPrompt     int            `json:"tokens_prompt,omitempty"`
	TokensCompletion int            `json:"tokens_completion,omitempty"`
	TokensTotal      int            `json:"tokens_total,omitempty"`
}

// ExecutionTrace records the full execution of a candidate chain for a single Generate call.
type ExecutionTrace struct {
	TraceID         string             `json:"trace_id"`
	Role            string             `json:"role"`
	Outcome         ExecutionOutcome   `json:"outcome"`
	Attempts        []CandidateAttempt `json:"attempts"`
	WinnerIndex     int                `json:"winner_index"`
	TotalDurationMS int64              `json:"total_duration_ms"`
	StartedAt       time.Time          `json:"started_at"`
	FinishedAt      time.Time          `json:"finished_at"`
	// Accumulated token counts across all attempts (including retries and fallbacks).
	TotalTokensPrompt     int `json:"total_tokens_prompt,omitempty"`
	TotalTokensCompletion int `json:"total_tokens_completion,omitempty"`
	TotalTokensTotal      int `json:"total_tokens_total,omitempty"`
}

// NewExecutionTrace creates a new trace with the given ID and role.
func NewExecutionTrace(traceID, role string) *ExecutionTrace {
	return &ExecutionTrace{
		TraceID:     traceID,
		Role:        role,
		Attempts:    make([]CandidateAttempt, 0, 4),
		WinnerIndex: -1,
		StartedAt:   time.Now().UTC(),
	}
}

// RecordAttempt appends a completed candidate attempt to the trace and
// accumulates token counts for cross-attempt totals (retries + fallbacks).
func (t *ExecutionTrace) RecordAttempt(attempt CandidateAttempt) {
	t.Attempts = append(t.Attempts, attempt)
	t.TotalTokensPrompt += attempt.TokensPrompt
	t.TotalTokensCompletion += attempt.TokensCompletion
	t.TotalTokensTotal += attempt.TokensTotal
}

// Finalize marks the trace as complete with the given outcome.
func (t *ExecutionTrace) Finalize(outcome ExecutionOutcome, winnerIndex int) {
	t.Outcome = outcome
	t.WinnerIndex = winnerIndex
	t.FinishedAt = time.Now().UTC()
	t.TotalDurationMS = t.FinishedAt.Sub(t.StartedAt).Milliseconds()
}

// ToJSON serializes the trace to JSON for embedding in result_payload.
func (t *ExecutionTrace) ToJSON() (json.RawMessage, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// NewCandidateAttempt creates an attempt record from a candidate and start time.
func NewCandidateAttempt(index int, candidate profile.ModelCandidate, startedAt time.Time) CandidateAttempt {
	return CandidateAttempt{
		CandidateIndex: index,
		ModelName:      candidate.ModelName,
		ThinkMode:      candidate.ThinkMode.String(),
		StartedAt:      startedAt,
	}
}

// Complete fills in the finish fields of an attempt after a provider call completes.
func (a *CandidateAttempt) Complete(outcome string, finishedAt time.Time) {
	a.FinishedAt = finishedAt
	a.DurationMS = finishedAt.Sub(a.StartedAt).Milliseconds()
	a.Outcome = outcome
}

// CompleteWithError fills in failure fields for a failed attempt.
func (a *CandidateAttempt) CompleteWithError(err error, fc FailureClass, action FallbackAction, finishedAt time.Time) {
	a.FinishedAt = finishedAt
	a.DurationMS = finishedAt.Sub(a.StartedAt).Milliseconds()
	a.Outcome = "failed"
	a.FailureClass = fc
	a.FallbackAction = action
	if err != nil {
		a.ErrorMessage = err.Error()
	}
}
