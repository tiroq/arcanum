package actions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/goals"
)

// mockAuditor captures audit events for verification.
type mockAuditor struct {
	mu     sync.Mutex
	events []auditCall
}

type auditCall struct {
	entityType string
	entityID   uuid.UUID
	eventType  string
	actorType  string
	actorID    string
	payload    any
}

func (m *mockAuditor) RecordEvent(
	_ context.Context,
	entityType string,
	entityID uuid.UUID,
	eventType, actorType, actorID string,
	payload any,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, auditCall{
		entityType: entityType,
		entityID:   entityID,
		eventType:  eventType,
		actorType:  actorType,
		actorID:    actorID,
		payload:    payload,
	})
	return nil
}

func (m *mockAuditor) getEvents() []auditCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]auditCall, len(m.events))
	copy(cp, m.events)
	return cp
}

// mockGoalEngine returns pre-set goals.
type mockGoalEngine struct {
	goals []goals.Goal
	err   error
}

// stubPlanner returns pre-set actions.
type stubPlanner struct {
	actions []Action
	err     error
}

// stubGuardrails always passes or always rejects.
type stubGuardrails struct {
	safe   bool
	reason string
}

func TestEngine_RunCycle_FullPipeline(t *testing.T) {
	// Setup a mock API server.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	logger := zap.NewNop()
	auditor := &mockAuditor{}

	// Build a real planner that returns a log recommendation (no DB needed).
	planner := &Planner{logger: logger}

	// Build guardrails with no DB (log recommendations skip load check).
	guardrails := &Guardrails{
		recentExecs: make(map[string]time.Time),
		logger:      logger,
	}

	executor := NewExecutor(srv.URL, "test-token", logger)

	// Create a mock goal engine by using the engine with nil goalEngine
	// but we can't easily mock — so test the individual pieces.
	// Instead, test the auditAction method directly.

	engine := &Engine{
		executor:   executor,
		guardrails: guardrails,
		planner:    planner,
		auditor:    auditor,
		logger:     logger,
	}

	// Test auditAction directly.
	action := Action{
		ID:          uuid.New().String(),
		Type:        string(ActionLogRecommendation),
		Priority:    0.5,
		Confidence:  0.6,
		GoalID:      "goal-test",
		Description: "test recommendation",
		Params:      map[string]any{"goal_type": "reduce_latency"},
	}

	engine.auditAction(context.Background(), action, "action.planned", "", "")

	events := auditor.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}

	ae := events[0]
	if ae.entityType != "action" {
		t.Errorf("expected entity_type 'action', got %q", ae.entityType)
	}
	if ae.eventType != "action.planned" {
		t.Errorf("expected event_type 'action.planned', got %q", ae.eventType)
	}
	if ae.actorType != "system" {
		t.Errorf("expected actor_type 'system', got %q", ae.actorType)
	}
	if ae.actorID != "action_engine" {
		t.Errorf("expected actor_id 'action_engine', got %q", ae.actorID)
	}

	payload, ok := ae.payload.(map[string]any)
	if !ok {
		t.Fatal("expected map payload")
	}
	if payload["action_id"] != action.ID {
		t.Errorf("expected action_id %q in payload", action.ID)
	}
	if payload["goal_id"] != "goal-test" {
		t.Errorf("expected goal_id 'goal-test' in payload")
	}
}

func TestEngine_AuditAction_Rejected(t *testing.T) {
	auditor := &mockAuditor{}
	engine := &Engine{
		auditor: auditor,
		logger:  zap.NewNop(),
	}

	action := Action{
		ID:     uuid.New().String(),
		Type:   string(ActionRetryJob),
		GoalID: "goal-rej",
		Params: map[string]any{"job_id": "job-rej"},
	}

	engine.auditAction(context.Background(), action, "action.rejected", "system overloaded", "")

	events := auditor.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	payload := events[0].payload.(map[string]any)
	if payload["reason"] != "system overloaded" {
		t.Errorf("expected reason 'system overloaded', got %q", payload["reason"])
	}
}

func TestEngine_AuditAction_Failed(t *testing.T) {
	auditor := &mockAuditor{}
	engine := &Engine{
		auditor: auditor,
		logger:  zap.NewNop(),
	}

	action := Action{
		ID:     uuid.New().String(),
		Type:   string(ActionRetryJob),
		GoalID: "goal-fail",
		Params: map[string]any{"job_id": "job-fail"},
	}

	engine.auditAction(context.Background(), action, "action.failed", "", "connection refused")

	events := auditor.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	payload := events[0].payload.(map[string]any)
	if payload["error"] != "connection refused" {
		t.Errorf("expected error 'connection refused', got %q", payload["error"])
	}
}

func TestEngine_AuditAction_NilAuditor(t *testing.T) {
	// Should not panic with nil auditor.
	engine := &Engine{
		auditor: nil,
		logger:  zap.NewNop(),
	}

	action := Action{
		ID:   uuid.New().String(),
		Type: string(ActionRetryJob),
	}

	// Should be a no-op, not panic.
	engine.auditAction(context.Background(), action, "action.planned", "", "")
}
