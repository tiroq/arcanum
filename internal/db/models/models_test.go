package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestAllExportedStructFields_HaveJSONTags ensures no field on any externally
// serialized model struct relies on Go's default PascalCase JSON encoding.
func TestAllExportedStructFields_HaveJSONTags(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(SourceConnection{}),
		reflect.TypeOf(SourceTask{}),
		reflect.TypeOf(SourceTaskSnapshot{}),
		reflect.TypeOf(ProcessingJob{}),
		reflect.TypeOf(ProcessingRun{}),
		reflect.TypeOf(SuggestionProposal{}),
		reflect.TypeOf(WritebackOperation{}),
		reflect.TypeOf(AuditEvent{}),
	}

	for _, rt := range types {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "" || tag == "-" {
				t.Errorf("%s.%s: missing json tag", rt.Name(), f.Name)
				continue
			}
			name := strings.SplitN(tag, ",", 2)[0]
			if name != strings.ToLower(name) {
				t.Errorf("%s.%s: json tag %q is not snake_case", rt.Name(), f.Name, name)
			}
		}
	}
}

// TestProcessingJob_JSONKeys_AreSnakeCase verifies the actual serialised JSON
// output uses snake_case keys.
func TestProcessingJob_JSONKeys_AreSnakeCase(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()
	job := ProcessingJob{
		ID:           id,
		SourceTaskID: uuid.New(),
		JobType:      "llm_rewrite",
		Status:       JobStatusQueued,
		Priority:     1,
		AttemptCount: 0,
		MaxAttempts:  3,
		Payload:      json.RawMessage(`{}`),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	requiredKeys := []string{
		"id", "source_task_id", "job_type", "status", "priority",
		"attempt_count", "max_attempts", "payload", "created_at", "updated_at",
	}
	for _, key := range requiredKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q in JSON output, got keys: %v", key, keysOf(m))
		}
	}

	// Ensure no PascalCase keys leaked.
	for key := range m {
		if key != strings.ToLower(key) {
			t.Errorf("PascalCase key leaked in JSON: %q", key)
		}
	}
}

// TestIsKnownJobType verifies the known job type lookup.
func TestIsKnownJobType(t *testing.T) {
	known := []string{"llm_rewrite", "llm_routing", "rules_classify", "composite"}
	for _, jt := range known {
		if !IsKnownJobType(jt) {
			t.Errorf("expected %q to be a known job type", jt)
		}
	}

	unknown := []string{"unknown", "bad_type", "", "LLM_REWRITE"}
	for _, jt := range unknown {
		if IsKnownJobType(jt) {
			t.Errorf("expected %q to NOT be a known job type", jt)
		}
	}
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
