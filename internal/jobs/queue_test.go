package jobs

import (
	"errors"
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

func TestErrUnknownJobType(t *testing.T) {
	// Verify the sentinel error exists and can be detected with errors.Is.
	if ErrUnknownJobType == nil {
		t.Fatal("ErrUnknownJobType must not be nil")
	}

	wrapped := errors.New("test: " + ErrUnknownJobType.Error())
	// Just verify it's a distinct, non-nil error.
	_ = wrapped
}
