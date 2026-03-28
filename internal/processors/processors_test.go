package processors

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestRulesOnlyProcessor_CanHandle(t *testing.T) {
	p := NewRulesOnlyProcessor()
	if !p.CanHandle("rules_classify") {
		t.Error("expected CanHandle to return true for 'rules_classify'")
	}
	if p.CanHandle("other") {
		t.Error("expected CanHandle to return false for 'other'")
	}
}

func TestRulesOnlyProcessor_Process_AllCaps(t *testing.T) {
	p := NewRulesOnlyProcessor()

	payload, _ := json.Marshal(map[string]string{"title": "HELLO WORLD"})
	jc := JobContext{
		JobID:           uuid.New(),
		SourceTaskID:    uuid.New(),
		JobType:         "rules_classify",
		SnapshotPayload: payload,
	}

	result, err := p.Process(context.Background(), jc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != "success" {
		t.Errorf("expected outcome 'success', got %q", result.Outcome)
	}

	var out map[string]string
	if err := json.Unmarshal(result.OutputPayload, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out["result"] != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", out["result"])
	}
}

func TestProcessorRegistry_FindFor(t *testing.T) {
	r := NewRegistry()
	r.Register(NewRulesOnlyProcessor())

	proc, err := r.FindFor("rules_classify")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.Name() != "rules_only" {
		t.Errorf("expected 'rules_only', got %q", proc.Name())
	}
}

func TestProcessorRegistry_FindFor_Unknown(t *testing.T) {
	r := NewRegistry()
	_, err := r.FindFor("unknown_type")
	if err == nil {
		t.Error("expected error for unknown job type, got nil")
	}
}

func TestCompositeProcessor_Process(t *testing.T) {
	comp := NewCompositeProcessor("test_composite", NewRulesOnlyProcessor())

	payload, _ := json.Marshal(map[string]string{"title": "HELLO"})
	jc := JobContext{
		JobID:           uuid.New(),
		SourceTaskID:    uuid.New(),
		JobType:         "composite",
		SnapshotPayload: payload,
	}

	result, err := comp.Process(context.Background(), jc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != "success" {
		t.Errorf("expected 'success', got %q: %s", result.Outcome, result.ErrorMessage)
	}
}
