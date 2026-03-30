package source

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

type hashableTask struct {
	ExternalID  string            `json:"external_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Priority    string            `json:"priority"`
	DueDate     string            `json:"due_date"`
	Labels      []string          `json:"labels"`
	Metadata    map[string]string `json:"metadata"`
}

// ComputeHash produces a stable SHA-256 hex hash of a NormalizedTask's content.
// Same content always produces the same hash regardless of map iteration order.
func ComputeHash(t NormalizedTask) string {
	dueDate := ""
	if t.DueDate != nil {
		dueDate = t.DueDate.UTC().Format("2006-01-02T15:04:05Z")
	}

	labels := make([]string, len(t.Labels))
	copy(labels, t.Labels)
	sort.Strings(labels)

	// Sort metadata keys for determinism.
	metadata := make(map[string]string, len(t.Metadata))
	for k, v := range t.Metadata {
		metadata[k] = v
	}

	h := hashableTask{
		ExternalID:  t.ExternalID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		DueDate:     dueDate,
		Labels:      labels,
		Metadata:    metadata,
	}

	data, _ := json.Marshal(h)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
