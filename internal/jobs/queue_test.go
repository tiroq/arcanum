package jobs

import (
	"testing"

	"github.com/google/uuid"
)

func TestEnqueueParams_DedupeKey(t *testing.T) {
	key := "conn:ext-1:abc123"
	params := EnqueueParams{
		SourceTaskID: uuid.New(),
		JobType:      "llm_rewrite",
		Priority:     0,
		DedupeKey:    &key,
		MaxAttempts:  3,
	}

	if params.DedupeKey == nil {
		t.Error("expected non-nil DedupeKey")
	}
	if *params.DedupeKey != key {
		t.Errorf("expected %q, got %q", key, *params.DedupeKey)
	}
	if params.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", params.MaxAttempts)
	}
}
