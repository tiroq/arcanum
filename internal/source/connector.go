package source

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tiroq/arcanum/internal/db/models"
)

// RawTask is the raw upstream representation of a task.
type RawTask struct {
	ExternalID    string
	EntityType    string
	SourcePayload json.RawMessage
}

// NormalizedTask is the normalized internal representation.
type NormalizedTask struct {
	ExternalID  string
	Title       string
	Description string
	Status      string
	Priority    string
	DueDate     *time.Time
	Labels      []string
	Metadata    map[string]string
	Hash        string // deterministic content hash
}

// Connector is the abstraction for upstream source integrations.
type Connector interface {
	Name() string
	FetchTasks(ctx context.Context, conn models.SourceConnection) ([]RawTask, error)
	NormalizeTask(raw RawTask) (NormalizedTask, error)
}
