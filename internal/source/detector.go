package source

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// DetectionResult holds the outcome of change detection for a single task.
type DetectionResult struct {
	ChangeType   string // "new", "changed", "unchanged"
	PreviousHash string
	NewHash      string
	SourceTaskID uuid.UUID
	SnapshotID   uuid.UUID
}

// ChangeDetector detects new and changed tasks by comparing content hashes.
type ChangeDetector struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

// NewChangeDetector creates a new ChangeDetector.
func NewChangeDetector(db *pgxpool.Pool, logger *zap.Logger) *ChangeDetector {
	return &ChangeDetector{db: db, logger: logger}
}

// Detect upserts a SourceTask and compares the current hash with the stored hash.
// Returns a DetectionResult describing whether the task is new, changed, or unchanged.
func (d *ChangeDetector) Detect(ctx context.Context, connID uuid.UUID, task NormalizedTask) (DetectionResult, error) {
	now := time.Now().UTC()

	var descPtr *string
	if task.Description != "" {
		descPtr = &task.Description
	}

	// Serialize the normalized task as the stored payload.
	normalizedPayload, err := json.Marshal(task)
	if err != nil {
		return DetectionResult{}, fmt.Errorf("marshal normalized payload: %w", err)
	}

	// Upsert source task. On conflict update all mutable fields so the row mirrors the upstream source.
	const upsertTask = `
		INSERT INTO source_tasks (id, source_connection_id, external_id, title, description, raw_payload, content_hash, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		ON CONFLICT (source_connection_id, external_id)
		DO UPDATE SET
		    title        = EXCLUDED.title,
		    description  = EXCLUDED.description,
		    raw_payload  = EXCLUDED.raw_payload,
		    status       = EXCLUDED.status,
		    updated_at   = EXCLUDED.updated_at
		RETURNING id, content_hash`

	newID := uuid.New()
	var taskID uuid.UUID
	var storedHash string

	err = d.db.QueryRow(ctx, upsertTask,
		newID, connID, task.ExternalID, task.Title, descPtr, normalizedPayload, task.Hash, task.Status, now,
	).Scan(&taskID, &storedHash)
	if err != nil {
		return DetectionResult{}, fmt.Errorf("upsert source task: %w", err)
	}

	result := DetectionResult{
		SourceTaskID: taskID,
		NewHash:      task.Hash,
		PreviousHash: storedHash,
	}

	// If taskID equals the newID we generated, the row was just inserted (new task).
	if taskID == newID {
		result.ChangeType = "new"
		result.PreviousHash = ""
	} else if storedHash == task.Hash {
		result.ChangeType = "unchanged"
		return result, nil
	} else {
		result.ChangeType = "changed"
	}

	// Determine the new snapshot version.
	var snapshotVersion int
	const getVersion = `SELECT COALESCE(MAX(snapshot_version), 0) FROM source_task_snapshots WHERE source_task_id = $1`
	if err := d.db.QueryRow(ctx, getVersion, taskID).Scan(&snapshotVersion); err != nil {
		return DetectionResult{}, fmt.Errorf("get snapshot version: %w", err)
	}
	snapshotVersion++

	snapshotID := uuid.New()
	const insertSnapshot = `
		INSERT INTO source_task_snapshots (id, source_task_id, snapshot_version, content_hash, raw_payload, snapshot_taken_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := d.db.Exec(ctx, insertSnapshot,
		snapshotID, taskID, snapshotVersion, task.Hash, normalizedPayload, now,
	); err != nil {
		return DetectionResult{}, fmt.Errorf("insert snapshot: %w", err)
	}

	const updateTask = `UPDATE source_tasks SET content_hash = $1, updated_at = $2 WHERE id = $3`
	if _, err := d.db.Exec(ctx, updateTask, task.Hash, now, taskID); err != nil {
		return DetectionResult{}, fmt.Errorf("update source task hash: %w", err)
	}

	result.SnapshotID = snapshotID
	return result, nil
}
