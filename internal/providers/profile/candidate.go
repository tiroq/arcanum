// Package profile defines execution profiles, model candidates, and think-mode
// configuration for role-based LLM execution strategies.
package profile

import (
	"fmt"
	"time"
)

// ThinkMode controls the thinking/reasoning behavior of a model candidate.
type ThinkMode string

const (
	// ThinkDefault defers to the provider's default thinking behavior.
	ThinkDefault ThinkMode = ""
	// ThinkEnabled explicitly enables extended thinking/reasoning.
	ThinkEnabled ThinkMode = "thinking"
	// ThinkDisabled explicitly disables extended thinking/reasoning.
	ThinkDisabled ThinkMode = "nothinking"
)

// ValidThinkModes lists all recognized ThinkMode values (excluding ThinkDefault).
var ValidThinkModes = []ThinkMode{ThinkEnabled, ThinkDisabled}

// IsValid returns true if the ThinkMode is recognized.
func (m ThinkMode) IsValid() bool {
	switch m {
	case ThinkDefault, ThinkEnabled, ThinkDisabled:
		return true
	}
	return false
}

// String returns the string representation of the ThinkMode.
func (m ThinkMode) String() string {
	if m == ThinkDefault {
		return "default"
	}
	return string(m)
}

// ParseThinkMode parses a string into a ThinkMode, returning an error for unrecognized values.
func ParseThinkMode(s string) (ThinkMode, error) {
	switch s {
	case "", "default":
		return ThinkDefault, nil
	case "thinking", "think", "on":
		return ThinkEnabled, nil
	case "nothinking", "nothink", "off":
		return ThinkDisabled, nil
	default:
		return ThinkDefault, fmt.Errorf("invalid think mode %q: must be one of thinking, nothinking, on, off, default", s)
	}
}

// ModelCandidate represents a single model in an execution profile's candidate chain.
// Candidates are tried in order; if one fails, the next is attempted according to
// the fallback policy.
type ModelCandidate struct {
	// ModelName is the Ollama model identifier (e.g., "qwen2.5:7b-instruct").
	ModelName string

	// ThinkMode controls thinking/reasoning behavior for this candidate.
	ThinkMode ThinkMode

	// Timeout is the per-candidate timeout. Zero means use the role's default timeout.
	Timeout time.Duration

	// JSONMode indicates whether to request structured JSON output from this candidate.
	JSONMode bool
}

// Validate checks that the ModelCandidate has required fields and valid values.
func (c ModelCandidate) Validate() error {
	if c.ModelName == "" {
		return fmt.Errorf("model candidate: model name is required")
	}
	if !c.ThinkMode.IsValid() {
		return fmt.Errorf("model candidate %q: invalid think mode %q", c.ModelName, c.ThinkMode)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("model candidate %q: timeout must be non-negative, got %v", c.ModelName, c.Timeout)
	}
	return nil
}
