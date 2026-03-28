package orchestrator

// ValidTransitions defines the allowed job status transitions.
var ValidTransitions = map[string][]string{
	"queued":          {"leased", "dead_letter"},
	"leased":          {"running", "failed", "dead_letter"},
	"running":         {"succeeded", "failed", "retry_scheduled", "dead_letter"},
	"retry_scheduled": {"queued"},
	"failed":          {"retry_scheduled", "dead_letter"},
	"succeeded":       {},
	"dead_letter":     {},
}

// CanTransition returns true if transitioning from -> to is a valid job state transition.
func CanTransition(from, to string) bool {
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
