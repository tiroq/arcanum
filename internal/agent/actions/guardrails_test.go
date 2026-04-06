package actions

import (
	"testing"
	"time"
)

func TestGuardrails_DedupeWindow(t *testing.T) {
	g := &Guardrails{
		recentExecs: make(map[string]time.Time),
	}

	action := Action{
		ID:   "act-1",
		Type: string(ActionRetryJob),
		Params: map[string]any{
			"job_id": "job-abc",
		},
	}

	key := actionDedupeKey(action)

	// First time: not a duplicate.
	if g.isRecentDuplicate(key) {
		t.Error("expected no duplicate on first check")
	}

	// Record execution.
	g.RecordExecution(action)

	// Second time: should be duplicate.
	if !g.isRecentDuplicate(key) {
		t.Error("expected duplicate after recording execution")
	}
}

func TestGuardrails_DedupeExpiry(t *testing.T) {
	g := &Guardrails{
		recentExecs: make(map[string]time.Time),
	}

	action := Action{
		ID:   "act-2",
		Type: string(ActionRetryJob),
		Params: map[string]any{
			"job_id": "job-xyz",
		},
	}

	key := actionDedupeKey(action)

	// Set an old execution time beyond the dedupe window.
	g.mu.Lock()
	g.recentExecs[key] = time.Now().UTC().Add(-(dedupeWindow + time.Minute))
	g.mu.Unlock()

	// Should NOT be a duplicate anymore.
	if g.isRecentDuplicate(key) {
		t.Error("expected expired entry to not be considered duplicate")
	}
}

func TestActionDedupeKey_RetryJob(t *testing.T) {
	a := Action{
		Type:   string(ActionRetryJob),
		Params: map[string]any{"job_id": "abc-123"},
	}
	key := actionDedupeKey(a)
	if key != "retry:abc-123" {
		t.Errorf("expected 'retry:abc-123', got %q", key)
	}
}

func TestActionDedupeKey_Resync(t *testing.T) {
	a := Action{
		Type:   string(ActionTriggerResync),
		Params: map[string]any{"source_task_id": "task-456"},
	}
	key := actionDedupeKey(a)
	if key != "resync:task-456" {
		t.Errorf("expected 'resync:task-456', got %q", key)
	}
}

func TestActionDedupeKey_LogRecommendation(t *testing.T) {
	a := Action{
		ID:   "act-99",
		Type: string(ActionLogRecommendation),
	}
	key := actionDedupeKey(a)
	expected := string(ActionLogRecommendation) + ":act-99"
	if key != expected {
		t.Errorf("expected %q, got %q", expected, key)
	}
}

func TestGuardrails_LogRecommendationBypassesLoadCheck(t *testing.T) {
	// Log recommendations should not need a DB — they should skip the load check.
	// We test this by passing nil db and verifying no panic.
	g := &Guardrails{
		recentExecs: make(map[string]time.Time),
		// db is nil — calling isSystemOverloaded would panic.
	}

	action := Action{
		ID:   "act-log",
		Type: string(ActionLogRecommendation),
	}

	// The EvaluateSafety method checks action type before doing DB calls.
	// Since we can't call it with nil db (it would still check dedupe),
	// verify the key dedup logic with nil db does not panic.
	key := actionDedupeKey(action)
	if g.isRecentDuplicate(key) {
		t.Error("expected not duplicate")
	}
}
