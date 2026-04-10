package selfextension

// Validator checks whether a sandbox run meets all requirements for deployment.
// Validation is deterministic and based on objective criteria.
type Validator struct{}

// NewValidator creates a new Validator.
func NewValidator() *Validator {
	return &Validator{}
}

// ValidationResult captures the outcome of validating a sandbox run.
type ValidationResult struct {
	Valid   bool     `json:"valid"`
	Reasons []string `json:"reasons,omitempty"`
}

// Validate checks a sandbox run against its spec and returns a validation result.
// A component is valid if:
//   - all tests pass
//   - no runtime errors
//   - meets defined constraints
//   - does not exceed resource limits
func (v *Validator) Validate(run SandboxRun, spec ComponentSpec) ValidationResult {
	var reasons []string

	// Check execution result.
	if run.ExecutionResult != ResultSuccess {
		reasons = append(reasons, "execution_failed")
	}

	// Check all tests passed.
	for _, tr := range run.TestResults {
		if !tr.Passed {
			reasons = append(reasons, "test_failed:"+tr.Name)
		}
	}

	// Check no runtime errors.
	if run.Metrics.Errors > 0 {
		reasons = append(reasons, "runtime_errors_detected")
	}

	// Check correctness flag.
	if !run.Metrics.CorrectnessOK {
		reasons = append(reasons, "correctness_check_failed")
	}

	// Check resource limits.
	if run.Metrics.LatencyMs > int64(SandboxMaxTimeoutSec*1000) {
		reasons = append(reasons, "exceeded_timeout")
	}
	if run.Metrics.MemoryUsedMB > float64(SandboxMaxMemoryMB) {
		reasons = append(reasons, "exceeded_memory_limit")
	}

	// Check test coverage: must have at least as many test results as requirements.
	if len(run.TestResults) < len(spec.TestRequirements) {
		reasons = append(reasons, "insufficient_test_coverage")
	}

	return ValidationResult{
		Valid:   len(reasons) == 0,
		Reasons: reasons,
	}
}
