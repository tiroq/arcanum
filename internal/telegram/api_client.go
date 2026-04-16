package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIClient talks to the api-gateway HTTP API.
type APIClient struct {
	baseURL    string
	adminToken string
	client     *http.Client
}

// NewAPIClient creates a new API client for the api-gateway.
func NewAPIClient(baseURL, adminToken string) *APIClient {
	return &APIClient{
		baseURL:    baseURL,
		adminToken: adminToken,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *APIClient) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Token", c.adminToken)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *APIClient) post(ctx context.Context, path string, payload interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Token", c.adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// GetAutonomyState returns the current autonomy runtime state.
func (c *APIClient) GetAutonomyState(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/autonomy/state")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetAutonomyMode changes the autonomy mode.
func (c *APIClient) SetAutonomyMode(ctx context.Context, mode string) error {
	_, err := c.post(ctx, "/api/v1/agent/autonomy/set-mode", map[string]string{"mode": mode})
	return err
}

// StartAutonomy starts the autonomy orchestrator.
func (c *APIClient) StartAutonomy(ctx context.Context) error {
	_, err := c.post(ctx, "/api/v1/agent/autonomy/start", nil)
	return err
}

// StopAutonomy stops the autonomy orchestrator.
func (c *APIClient) StopAutonomy(ctx context.Context) error {
	_, err := c.post(ctx, "/api/v1/agent/autonomy/stop", nil)
	return err
}

// GetGoals returns a list of active goals.
func (c *APIClient) GetGoals(ctx context.Context) ([]map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/goals")
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetSubgoals returns all subgoals.
func (c *APIClient) GetSubgoals(ctx context.Context) ([]map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/goals/subgoals")
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetTaskQueue returns the current task queue.
func (c *APIClient) GetTaskQueue(ctx context.Context) ([]map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/tasks/queue")
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetTasks returns all tasks.
func (c *APIClient) GetTasks(ctx context.Context) ([]map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/tasks")
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetAutonomyReports returns recent autonomy reports.
func (c *APIClient) GetAutonomyReports(ctx context.Context) ([]map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/autonomy/reports")
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetObjectiveSummary returns the current objective function summary.
func (c *APIClient) GetObjectiveSummary(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/objective/summary")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetVector returns the current system vector.
func (c *APIClient) GetVector(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/vector")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetVector updates the system vector.
func (c *APIClient) SetVector(ctx context.Context, v map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.post(ctx, "/api/v1/agent/vector/set", v)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ApproveActuationDecision approves an actuation decision.
func (c *APIClient) ApproveActuationDecision(ctx context.Context, id string) error {
	_, err := c.post(ctx, "/api/v1/agent/actuation/approve/"+id, nil)
	return err
}

// RejectActuationDecision rejects an actuation decision.
func (c *APIClient) RejectActuationDecision(ctx context.Context, id string) error {
	_, err := c.post(ctx, "/api/v1/agent/actuation/reject/"+id, nil)
	return err
}

// GetActuationDecisions returns actuation decisions.
func (c *APIClient) GetActuationDecisions(ctx context.Context) ([]map[string]interface{}, error) {
	data, err := c.get(ctx, "/api/v1/agent/actuation/decisions")
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
