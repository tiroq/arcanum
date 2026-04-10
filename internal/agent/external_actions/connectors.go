package externalactions

import (
	"encoding/json"
	"fmt"
	"time"
)

// --- Noop Connector ---

// NoopConnector is a testing connector that always succeeds without side effects.
type NoopConnector struct {
	enabled bool
}

// NewNoopConnector creates a new noop connector.
func NewNoopConnector() *NoopConnector {
	return &NoopConnector{enabled: true}
}

func (c *NoopConnector) Name() string                    { return ConnectorNoop }
func (c *NoopConnector) Supports(actionType string) bool { return true }
func (c *NoopConnector) Enabled() bool                   { return c.enabled }

func (c *NoopConnector) Execute(payload json.RawMessage) (ExecutionResult, error) {
	return ExecutionResult{
		Success:         true,
		ExternalID:      "noop-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		ResponsePayload: json.RawMessage(`{"connector":"noop","status":"executed"}`),
		Mode:            ModeExecute,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

func (c *NoopConnector) DryRun(payload json.RawMessage) (ExecutionResult, error) {
	return ExecutionResult{
		Success:         true,
		ExternalID:      "",
		ResponsePayload: json.RawMessage(`{"connector":"noop","status":"dry_run","would_execute":true}`),
		Mode:            ModeDryRun,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

// --- Log Connector ---

// LogConnector writes action details to audit/log without external side effects.
type LogConnector struct {
	enabled bool
	log     []LogEntry
}

// LogEntry represents a logged action.
type LogEntry struct {
	ActionType string          `json:"action_type"`
	Payload    json.RawMessage `json:"payload"`
	Mode       string          `json:"mode"`
	Timestamp  time.Time       `json:"timestamp"`
}

// NewLogConnector creates a new log connector.
func NewLogConnector() *LogConnector {
	return &LogConnector{enabled: true}
}

func (c *LogConnector) Name() string                    { return ConnectorLog }
func (c *LogConnector) Supports(actionType string) bool { return true }
func (c *LogConnector) Enabled() bool                   { return c.enabled }

func (c *LogConnector) Execute(payload json.RawMessage) (ExecutionResult, error) {
	entry := LogEntry{
		Payload:   payload,
		Mode:      ModeExecute,
		Timestamp: time.Now().UTC(),
	}
	c.log = append(c.log, entry)

	resp, _ := json.Marshal(map[string]string{
		"connector": ConnectorLog,
		"status":    "logged",
		"entry_num": fmt.Sprintf("%d", len(c.log)),
	})

	return ExecutionResult{
		Success:         true,
		ExternalID:      fmt.Sprintf("log-%d", len(c.log)),
		ResponsePayload: resp,
		Mode:            ModeExecute,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

func (c *LogConnector) DryRun(payload json.RawMessage) (ExecutionResult, error) {
	resp, _ := json.Marshal(map[string]string{
		"connector":     ConnectorLog,
		"status":        "dry_run",
		"would_log":     "true",
		"current_count": fmt.Sprintf("%d", len(c.log)),
	})
	return ExecutionResult{
		Success:         true,
		ResponsePayload: resp,
		Mode:            ModeDryRun,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

// GetLog returns all logged entries (for testing/observability).
func (c *LogConnector) GetLog() []LogEntry { return c.log }

// --- HTTP Connector ---

// HTTPConnector is a generic HTTP API connector.
// It does NOT make real HTTP calls — it validates and prepares the request.
// Real execution requires a transport layer injected at runtime.
type HTTPConnector struct {
	enabled   bool
	transport HTTPTransport
}

// HTTPTransport defines the interface for actual HTTP execution.
// This is injected to keep the connector testable and sandboxed.
type HTTPTransport interface {
	Do(method, url string, headers map[string]string, body []byte) (statusCode int, respBody []byte, err error)
}

// HTTPPayload is the expected payload format for the HTTP connector.
type HTTPPayload struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// NewHTTPConnector creates a new HTTP connector.
func NewHTTPConnector(transport HTTPTransport) *HTTPConnector {
	return &HTTPConnector{enabled: true, transport: transport}
}

func (c *HTTPConnector) Name() string { return ConnectorHTTP }
func (c *HTTPConnector) Supports(actionType string) bool {
	return actionType == ActionTriggerAPI || actionType == ActionCreateTask
}
func (c *HTTPConnector) Enabled() bool { return c.enabled }

func (c *HTTPConnector) Execute(payload json.RawMessage) (ExecutionResult, error) {
	var hp HTTPPayload
	if err := json.Unmarshal(payload, &hp); err != nil {
		return ExecutionResult{Success: false, ErrorMessage: "invalid http payload: " + err.Error(), Mode: ModeExecute}, err
	}
	if hp.URL == "" || hp.Method == "" {
		return ExecutionResult{
			Success:      false,
			ErrorMessage: "url and method are required",
			Mode:         ModeExecute,
		}, fmt.Errorf("url and method are required")
	}

	if c.transport == nil {
		return ExecutionResult{
			Success:      false,
			ErrorMessage: "no http transport configured",
			Mode:         ModeExecute,
		}, fmt.Errorf("no http transport configured")
	}

	statusCode, respBody, err := c.transport.Do(hp.Method, hp.URL, hp.Headers, hp.Body)
	if err != nil {
		return ExecutionResult{
			Success:      false,
			ErrorMessage: err.Error(),
			Mode:         ModeExecute,
			ExecutedAt:   time.Now().UTC(),
		}, err
	}

	success := statusCode >= 200 && statusCode < 300
	resp, _ := json.Marshal(map[string]interface{}{
		"status_code": statusCode,
		"body":        string(respBody),
	})

	return ExecutionResult{
		Success:         success,
		ExternalID:      fmt.Sprintf("http-%d-%d", statusCode, time.Now().UnixNano()),
		ResponsePayload: resp,
		Mode:            ModeExecute,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

func (c *HTTPConnector) DryRun(payload json.RawMessage) (ExecutionResult, error) {
	var hp HTTPPayload
	if err := json.Unmarshal(payload, &hp); err != nil {
		return ExecutionResult{Success: false, ErrorMessage: "invalid http payload: " + err.Error(), Mode: ModeDryRun}, err
	}
	if hp.URL == "" || hp.Method == "" {
		return ExecutionResult{
			Success:      false,
			ErrorMessage: "url and method are required",
			Mode:         ModeDryRun,
		}, fmt.Errorf("url and method are required")
	}

	resp, _ := json.Marshal(map[string]interface{}{
		"connector":    ConnectorHTTP,
		"status":       "dry_run",
		"would_call":   hp.URL,
		"method":       hp.Method,
		"header_count": len(hp.Headers),
	})

	return ExecutionResult{
		Success:         true,
		ResponsePayload: resp,
		Mode:            ModeDryRun,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

// --- Email Draft Connector ---

// EmailDraftConnector creates email drafts without sending.
// This is a safe connector — it NEVER sends email, only produces drafts.
type EmailDraftConnector struct {
	enabled bool
	drafts  []EmailDraft
}

// EmailDraft represents a composed email draft.
type EmailDraft struct {
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
	DraftID string    `json:"draft_id"`
	Created time.Time `json:"created"`
}

// EmailPayload is the expected payload for the email draft connector.
type EmailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// NewEmailDraftConnector creates a new email draft connector.
func NewEmailDraftConnector() *EmailDraftConnector {
	return &EmailDraftConnector{enabled: true}
}

func (c *EmailDraftConnector) Name() string { return ConnectorEmailDraft }
func (c *EmailDraftConnector) Supports(actionType string) bool {
	return actionType == ActionDraftMessage || actionType == ActionSendMessage
}
func (c *EmailDraftConnector) Enabled() bool { return c.enabled }

func (c *EmailDraftConnector) Execute(payload json.RawMessage) (ExecutionResult, error) {
	var ep EmailPayload
	if err := json.Unmarshal(payload, &ep); err != nil {
		return ExecutionResult{Success: false, ErrorMessage: "invalid email payload: " + err.Error(), Mode: ModeExecute}, err
	}
	if ep.To == "" || ep.Subject == "" {
		return ExecutionResult{
			Success:      false,
			ErrorMessage: "to and subject are required",
			Mode:         ModeExecute,
		}, fmt.Errorf("to and subject are required")
	}

	draftID := fmt.Sprintf("draft-%d", time.Now().UnixNano())
	draft := EmailDraft{
		To:      ep.To,
		Subject: ep.Subject,
		Body:    ep.Body,
		DraftID: draftID,
		Created: time.Now().UTC(),
	}
	c.drafts = append(c.drafts, draft)

	resp, _ := json.Marshal(map[string]string{
		"connector": ConnectorEmailDraft,
		"status":    "draft_created",
		"draft_id":  draftID,
		"note":      "email NOT sent — draft only",
	})

	return ExecutionResult{
		Success:         true,
		ExternalID:      draftID,
		ResponsePayload: resp,
		Mode:            ModeExecute,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

func (c *EmailDraftConnector) DryRun(payload json.RawMessage) (ExecutionResult, error) {
	var ep EmailPayload
	if err := json.Unmarshal(payload, &ep); err != nil {
		return ExecutionResult{Success: false, ErrorMessage: "invalid email payload: " + err.Error(), Mode: ModeDryRun}, err
	}

	resp, _ := json.Marshal(map[string]string{
		"connector":    ConnectorEmailDraft,
		"status":       "dry_run",
		"would_draft":  "true",
		"to":           ep.To,
		"subject":      ep.Subject,
		"body_preview": truncate(ep.Body, 100),
	})

	return ExecutionResult{
		Success:         true,
		ResponsePayload: resp,
		Mode:            ModeDryRun,
		ExecutedAt:      time.Now().UTC(),
	}, nil
}

// GetDrafts returns all created email drafts (for observability).
func (c *EmailDraftConnector) GetDrafts() []EmailDraft { return c.drafts }

// truncate shortens a string to maxLen.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
