package actions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Executor performs concrete actions by calling the existing API layer.
// It NEVER mutates the database directly — all changes flow through
// the standard API → bus → handler pipeline.
type Executor struct {
	apiBaseURL string
	apiToken   string
	client     *http.Client
	logger     *zap.Logger
}

// NewExecutor creates an Executor targeting the given API base URL.
func NewExecutor(apiBaseURL, apiToken string, logger *zap.Logger) *Executor {
	return &Executor{
		apiBaseURL: apiBaseURL,
		apiToken:   apiToken,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// ExecuteAction dispatches a single action to the appropriate API endpoint.
// Returns a structured ActionResult.
func (e *Executor) ExecuteAction(ctx context.Context, action Action) ActionResult {
	start := time.Now()

	e.logger.Info("action_execute_start",
		zap.String("action_id", action.ID),
		zap.String("type", action.Type),
		zap.Any("params", action.Params),
	)

	var err error
	switch action.Type {
	case string(ActionRetryJob):
		err = e.retryJob(ctx, action)
	case string(ActionTriggerResync):
		err = e.triggerResync(ctx, action)
	case string(ActionLogRecommendation):
		// No-op: audit event is emitted by the engine, not the executor.
		return ActionResult{
			ActionID: action.ID,
			Status:   StatusExecuted,
			Duration: time.Since(start),
		}
	default:
		return ActionResult{
			ActionID: action.ID,
			Status:   StatusFailed,
			Error:    fmt.Sprintf("unknown action type: %s", action.Type),
			Duration: time.Since(start),
		}
	}

	if err != nil {
		e.logger.Error("action_execute_failed",
			zap.String("action_id", action.ID),
			zap.String("type", action.Type),
			zap.Error(err),
		)
		return ActionResult{
			ActionID: action.ID,
			Status:   StatusFailed,
			Error:    err.Error(),
			Duration: time.Since(start),
		}
	}

	e.logger.Info("action_execute_success",
		zap.String("action_id", action.ID),
		zap.String("type", action.Type),
		zap.Duration("duration", time.Since(start)),
	)

	return ActionResult{
		ActionID: action.ID,
		Status:   StatusExecuted,
		Duration: time.Since(start),
	}
}

// retryJob calls POST /api/v1/jobs/{id}/retry
func (e *Executor) retryJob(ctx context.Context, action Action) error {
	jobID, ok := action.Params["job_id"].(string)
	if !ok || jobID == "" {
		return fmt.Errorf("missing job_id param")
	}
	url := fmt.Sprintf("%s/api/v1/jobs/%s/retry", e.apiBaseURL, jobID)
	return e.doPost(ctx, url)
}

// triggerResync calls POST /api/v1/source-tasks/{id}/resync
func (e *Executor) triggerResync(ctx context.Context, action Action) error {
	taskID, ok := action.Params["source_task_id"].(string)
	if !ok || taskID == "" {
		return fmt.Errorf("missing source_task_id param")
	}
	url := fmt.Sprintf("%s/api/v1/source-tasks/%s/resync", e.apiBaseURL, taskID)
	return e.doPost(ctx, url)
}

// doPost performs a POST request to the given URL with auth header.
func (e *Executor) doPost(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if e.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiToken)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 300 {
		return fmt.Errorf("api returned status %d", resp.StatusCode)
	}
	return nil
}
