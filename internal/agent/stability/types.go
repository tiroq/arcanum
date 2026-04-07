package stability

import (
	"time"

	"github.com/google/uuid"
)

// Mode represents the current stability mode of the system.
type Mode string

const (
	ModeNormal    Mode = "normal"
	ModeThrottled Mode = "throttled"
	ModeSafeMode  Mode = "safe_mode"
)

// State is the current stability state of the autonomous system.
type State struct {
	ID                 uuid.UUID `json:"id"`
	Mode               Mode      `json:"mode"`
	ThrottleMultiplier float64   `json:"throttle_multiplier"`
	BlockedActionTypes []string  `json:"blocked_action_types"`
	Reason             string    `json:"reason"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// IsActionBlocked returns true if the given action type is currently blocked.
func (s *State) IsActionBlocked(actionType string) bool {
	for _, blocked := range s.BlockedActionTypes {
		if blocked == actionType {
			return true
		}
	}
	return false
}

// Finding identifies which stability rule fired.
type Finding string

const (
	FindingNoopLoop           Finding = "noop_loop_detected"
	FindingLowValueLoop       Finding = "low_value_loop_detected"
	FindingCycleInstability   Finding = "cycle_instability_detected"
	FindingRetryAmplification Finding = "retry_amplification_detected"
	FindingStabilityRecovered Finding = "stability_recovered"
)

// DetectionResult is the output of one stability evaluation pass.
type DetectionResult struct {
	Findings  []DetectionFinding `json:"findings"`
	Timestamp time.Time          `json:"timestamp"`
}

// DetectionFinding is a single stability signal.
type DetectionFinding struct {
	Finding    Finding        `json:"finding"`
	ActionType string         `json:"action_type,omitempty"`
	Detail     map[string]any `json:"detail"`
}
