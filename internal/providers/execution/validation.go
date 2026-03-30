package execution

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Validator checks a provider response for correctness beyond basic success/failure.
type Validator interface {
	Name() string
	Validate(content string) error
}

// ValidationPolicy defines which validators to run and how to handle failures.
type ValidationPolicy struct {
	Validators []Validator
	FailAction FallbackAction
}

// DefaultValidationPolicy returns a policy with no validators (accept any response).
func DefaultValidationPolicy() ValidationPolicy {
	return ValidationPolicy{
		Validators: nil,
		FailAction: ActionNextCandidate,
	}
}

// Run executes all validators in order. Returns the first error encountered, or nil.
func (p ValidationPolicy) Run(content string) error {
	for _, v := range p.Validators {
		if err := v.Validate(content); err != nil {
			return &ValidationError{
				Reason: fmt.Sprintf("validator %q: %s", v.Name(), err.Error()),
			}
		}
	}
	return nil
}

// JSONValidator checks that the response content is valid JSON.
type JSONValidator struct{}

func (v JSONValidator) Name() string { return "json" }

func (v JSONValidator) Validate(content string) error {
	content = strings.TrimSpace(content)
	if !json.Valid([]byte(content)) {
		return fmt.Errorf("response is not valid JSON")
	}
	return nil
}

// NonEmptyValidator checks that the response content is not empty or whitespace-only.
type NonEmptyValidator struct{}

func (v NonEmptyValidator) Name() string { return "non_empty" }

func (v NonEmptyValidator) Validate(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("response is empty")
	}
	return nil
}

// MinLengthValidator checks that the response content meets a minimum character length.
type MinLengthValidator struct {
	Min int
}

func (v MinLengthValidator) Name() string { return "min_length" }

func (v MinLengthValidator) Validate(content string) error {
	if len(strings.TrimSpace(content)) < v.Min {
		return fmt.Errorf("response length %d is below minimum %d", len(strings.TrimSpace(content)), v.Min)
	}
	return nil
}
