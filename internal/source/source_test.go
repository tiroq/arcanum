package source

import (
	"testing"
	"time"
)

func TestComputeHash_Stable(t *testing.T) {
	task := NormalizedTask{
		ExternalID:  "ext-1",
		Title:       "Test Task",
		Description: "A description",
		Status:      "open",
		Priority:    "normal",
		Labels:      []string{"work", "urgent"},
		Metadata:    map[string]string{"source": "test"},
	}

	h1 := ComputeHash(task)
	h2 := ComputeHash(task)

	if h1 != h2 {
		t.Errorf("expected same hash, got %q and %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got len %d", len(h1))
	}
}

func TestComputeHash_Different(t *testing.T) {
	due := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a := NormalizedTask{
		ExternalID: "ext-1",
		Title:      "Task A",
		DueDate:    &due,
	}
	b := NormalizedTask{
		ExternalID: "ext-1",
		Title:      "Task B",
		DueDate:    &due,
	}

	if ComputeHash(a) == ComputeHash(b) {
		t.Error("expected different hashes for different content")
	}
}
