package externalactions

import (
	"encoding/json"
	"time"
)

// --- Status Constants ---

const (
	// Action statuses (explicit state machine).
	StatusCreated        = "created"
	StatusReviewRequired = "review_required"
	StatusReady          = "ready"
	StatusExecuted       = "executed"
	StatusFailed         = "failed"

	// Risk levels.
	RiskLow      = "low"
	RiskMedium   = "medium"
	RiskHigh     = "high"
	RiskCritical = "critical"

	// Execution modes.
	ModeDryRun  = "dry_run"
	ModeExecute = "execute"

	// Action types.
	ActionDraftMessage    = "draft_message"
	ActionSendMessage     = "send_message"
	ActionScheduleMeeting = "schedule_meeting"
	ActionPublishPost     = "publish_post"
	ActionCreateTask      = "create_task"
	ActionTriggerAPI      = "trigger_api"

	// Connector names.
	ConnectorNoop       = "noop"
	ConnectorLog        = "log"
	ConnectorHTTP       = "http"
	ConnectorEmailDraft = "email_draft"

	// Retry limits.
	MaxRetries          = 3
	DefaultRetryBackoff = 5 * time.Second
)

// ValidActionStatuses defines the valid status values.
var ValidActionStatuses = []string{
	StatusCreated, StatusReviewRequired, StatusReady, StatusExecuted, StatusFailed,
}

// ValidTransitions defines allowed state transitions.
var ValidTransitions = map[string][]string{
	StatusCreated:        {StatusReviewRequired, StatusReady},
	StatusReviewRequired: {StatusReady, StatusFailed},
	StatusReady:          {StatusExecuted, StatusFailed},
	// Terminal: executed, failed — no further transitions.
}

// HighRiskActionTypes are action types that always require review.
var HighRiskActionTypes = map[string]bool{
	ActionSendMessage:     true,
	ActionScheduleMeeting: true,
	ActionPublishPost:     true,
}

// IsValidTransition checks whether from→to is an allowed state transition.
func IsValidTransition(from, to string) bool {
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

// IsValidActionType returns true if actionType is a known action type.
func IsValidActionType(actionType string) bool {
	switch actionType {
	case ActionDraftMessage, ActionSendMessage, ActionScheduleMeeting,
		ActionPublishPost, ActionCreateTask, ActionTriggerAPI:
		return true
	}
	return false
}

// --- Entities ---

// ExternalAction represents an action intended for an external system.
type ExternalAction struct {
	ID              string          `json:"id"`
	OpportunityID   string          `json:"opportunity_id"`
	ActionType      string          `json:"action_type"`
	Payload         json.RawMessage `json:"payload"`
	Status          string          `json:"status"`
	ConnectorName   string          `json:"connector_name"`
	IdempotencyKey  string          `json:"idempotency_key"`
	RiskLevel       string          `json:"risk_level"`
	ReviewReason    string          `json:"review_reason,omitempty"`
	RetryCount      int             `json:"retry_count"`
	MaxRetries      int             `json:"max_retries"`
	DryRunCompleted bool            `json:"dry_run_completed"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// ExecutionResult captures the result of a connector execution.
type ExecutionResult struct {
	ID              string          `json:"id"`
	ActionID        string          `json:"action_id"`
	Success         bool            `json:"success"`
	ExternalID      string          `json:"external_id,omitempty"`
	ResponsePayload json.RawMessage `json:"response_payload,omitempty"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	Mode            string          `json:"mode"`
	DurationMs      int64           `json:"duration_ms"`
	ExecutedAt      time.Time       `json:"executed_at"`
}

// ActionPolicy defines the review policy for an action.
type ActionPolicy struct {
	RequiresReview bool   `json:"requires_review"`
	RiskLevel      string `json:"risk_level"`
	Reason         string `json:"reason"`
}

// --- Connector Interface ---

// Connector defines the interface that all external action connectors must implement.
// Connectors are pluggable and sandboxed — core logic never calls external APIs directly.
type Connector interface {
	// Name returns the connector's unique name.
	Name() string
	// Supports returns true if the connector can handle the given action type.
	Supports(actionType string) bool
	// Execute performs the real external action.
	Execute(payload json.RawMessage) (ExecutionResult, error)
	// DryRun simulates the action without side effects.
	DryRun(payload json.RawMessage) (ExecutionResult, error)
	// Enabled returns whether the connector is currently active.
	Enabled() bool
}

// --- Validation ---

// ValidatePayload checks that payload is valid non-empty JSON.
func ValidatePayload(payload json.RawMessage) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var check json.RawMessage
	if err := json.Unmarshal(payload, &check); err != nil {
		return ErrInvalidPayload
	}
	return nil
}

// --- Errors ---

// Sentinel errors for the external actions package.
var (
	ErrEmptyPayload       = errStr("payload must not be empty")
	ErrInvalidPayload     = errStr("payload is not valid JSON")
	ErrConnectorNotFound  = errStr("no connector found for action type")
	ErrConnectorDisabled  = errStr("connector is disabled")
	ErrReviewRequired     = errStr("action requires review before execution")
	ErrInvalidTransition  = errStr("invalid status transition")
	ErrActionNotFound     = errStr("action not found")
	ErrAlreadyExecuted    = errStr("action has already been executed")
	ErrMaxRetriesExceeded = errStr("maximum retries exceeded")
)

type errStr string

func (e errStr) Error() string { return string(e) }
