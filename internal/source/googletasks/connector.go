package googletasks

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/source"
)

// GoogleTasksConnector is a stub connector for Google Tasks.
// TODO: Implement OAuth2 + Google Tasks API calls.
type GoogleTasksConnector struct {
	logger *zap.Logger
}

// NewGoogleTasksConnector creates a new GoogleTasksConnector stub.
func NewGoogleTasksConnector(logger *zap.Logger) *GoogleTasksConnector {
	return &GoogleTasksConnector{logger: logger}
}

// Name returns the connector name.
func (c *GoogleTasksConnector) Name() string { return "google_tasks" }

type googleTask struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Notes   string `json:"notes"`
	Status  string `json:"status"`
	Due     string `json:"due"`
}

// FetchTasks returns mock tasks for local development.
func (c *GoogleTasksConnector) FetchTasks(_ context.Context, conn models.SourceConnection) ([]source.RawTask, error) {
	c.logger.Info("fetching google tasks (stub)", zap.String("connection", conn.Name))
	return mockTasks(), nil
}

// NormalizeTask converts a Google Tasks raw task to the internal normalized format.
func (c *GoogleTasksConnector) NormalizeTask(raw source.RawTask) (source.NormalizedTask, error) {
	var gt googleTask
	if err := json.Unmarshal(raw.SourcePayload, &gt); err != nil {
		return source.NormalizedTask{}, err
	}

	normalized := source.NormalizedTask{
		ExternalID:  gt.ID,
		Title:       gt.Title,
		Description: gt.Notes,
		Status:      gt.Status,
		Priority:    "normal",
		Labels:      []string{},
		Metadata:    map[string]string{"source": "google_tasks"},
	}

	if gt.Due != "" {
		t, err := time.Parse(time.RFC3339, gt.Due)
		if err == nil {
			normalized.DueDate = &t
		}
	}

	return normalized, nil
}

func mockTasks() []source.RawTask {
	tasks := []googleTask{
		{ID: "gt-001", Title: "Buy groceries", Notes: "Milk, eggs, bread", Status: "needsAction"},
		{ID: "gt-002", Title: "CALL THE DENTIST", Notes: "Schedule annual checkup", Status: "needsAction"},
		{ID: "gt-003", Title: "Read quarterly report", Notes: "", Status: "needsAction", Due: "2024-12-31T00:00:00Z"},
	}

	rawTasks := make([]source.RawTask, 0, len(tasks))
	for _, t := range tasks {
		payload, _ := json.Marshal(t)
		rawTasks = append(rawTasks, source.RawTask{
			ExternalID:    t.ID,
			EntityType:    "task",
			SourcePayload: payload,
		})
	}
	return rawTasks
}
