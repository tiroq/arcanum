package execution

// ExecutionOutcome represents the final result of a candidate chain execution.
type ExecutionOutcome string

const (
	// OutcomeSuccess indicates a candidate succeeded and passed validation.
	OutcomeSuccess ExecutionOutcome = "success"
	// OutcomeFallback indicates a later candidate succeeded after earlier ones failed.
	OutcomeFallback ExecutionOutcome = "fallback"
	// OutcomeExhausted indicates all candidates were tried and none succeeded.
	OutcomeExhausted ExecutionOutcome = "exhausted"
	// OutcomeAborted indicates execution was aborted due to a non-recoverable failure.
	OutcomeAborted ExecutionOutcome = "aborted"
)

// ValidExecutionOutcomes lists all recognized outcome values.
var ValidExecutionOutcomes = []ExecutionOutcome{
	OutcomeSuccess, OutcomeFallback, OutcomeExhausted, OutcomeAborted,
}

// IsTerminal returns true if this outcome represents a final state (no more retries).
func (o ExecutionOutcome) IsTerminal() bool {
	switch o {
	case OutcomeSuccess, OutcomeFallback, OutcomeExhausted, OutcomeAborted:
		return true
	}
	return false
}

// IsSuccess returns true if the execution produced a valid result.
func (o ExecutionOutcome) IsSuccess() bool {
	return o == OutcomeSuccess || o == OutcomeFallback
}

// String returns the string representation of the ExecutionOutcome.
func (o ExecutionOutcome) String() string {
	return string(o)
}
