package actions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestExecutor_RetryJob(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/jobs/job-123/retry" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong auth header: %s", r.Header.Get("Authorization"))
		}
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.URL, "test-token", zap.NewNop())
	result := exec.ExecuteAction(context.Background(), Action{
		ID:   "act-1",
		Type: string(ActionRetryJob),
		Params: map[string]any{
			"job_id": "job-123",
		},
	})

	if !called {
		t.Error("expected HTTP call to be made")
	}
	if result.Status != StatusExecuted {
		t.Errorf("expected status %q, got %q", StatusExecuted, result.Status)
	}
}

func TestExecutor_TriggerResync(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/source-tasks/task-456/resync" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.URL, "test-token", zap.NewNop())
	result := exec.ExecuteAction(context.Background(), Action{
		ID:   "act-2",
		Type: string(ActionTriggerResync),
		Params: map[string]any{
			"source_task_id": "task-456",
		},
	})

	if !called {
		t.Error("expected HTTP call to be made")
	}
	if result.Status != StatusExecuted {
		t.Errorf("expected status %q, got %q", StatusExecuted, result.Status)
	}
}

func TestExecutor_LogRecommendation_NoHTTPCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP call expected for log_recommendation")
	}))
	defer srv.Close()

	exec := NewExecutor(srv.URL, "test-token", zap.NewNop())
	result := exec.ExecuteAction(context.Background(), Action{
		ID:   "act-3",
		Type: string(ActionLogRecommendation),
	})

	if result.Status != StatusExecuted {
		t.Errorf("expected status %q, got %q", StatusExecuted, result.Status)
	}
}

func TestExecutor_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.URL, "test-token", zap.NewNop())
	result := exec.ExecuteAction(context.Background(), Action{
		ID:   "act-4",
		Type: string(ActionRetryJob),
		Params: map[string]any{
			"job_id": "job-fail",
		},
	})

	if result.Status != StatusFailed {
		t.Errorf("expected status %q, got %q", StatusFailed, result.Status)
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestExecutor_MissingParam(t *testing.T) {
	exec := NewExecutor("http://localhost:9999", "", zap.NewNop())
	result := exec.ExecuteAction(context.Background(), Action{
		ID:     "act-5",
		Type:   string(ActionRetryJob),
		Params: map[string]any{},
	})

	if result.Status != StatusFailed {
		t.Errorf("expected status %q, got %q", StatusFailed, result.Status)
	}
}

func TestExecutor_UnknownActionType(t *testing.T) {
	exec := NewExecutor("http://localhost:9999", "", zap.NewNop())
	result := exec.ExecuteAction(context.Background(), Action{
		ID:   "act-6",
		Type: "unknown_type",
	})

	if result.Status != StatusFailed {
		t.Errorf("expected status %q, got %q", StatusFailed, result.Status)
	}
}

func TestExecutor_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.URL, "", zap.NewNop())
	// Use a cancelled context to simulate timeout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := exec.ExecuteAction(ctx, Action{
		ID:   "act-7",
		Type: string(ActionRetryJob),
		Params: map[string]any{
			"job_id": "job-timeout",
		},
	})

	if result.Status != StatusFailed {
		t.Errorf("expected status %q, got %q", StatusFailed, result.Status)
	}
}
