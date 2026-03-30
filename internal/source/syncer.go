package source

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/contracts/events"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/jobs"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
)

// Syncer orchestrates fetching, normalizing, change-detecting, and job creation.
type Syncer struct {
	connector Connector
	detector  *ChangeDetector
	jobQueue  *jobs.Queue
	publisher *messaging.Publisher
	metrics   *metrics.Metrics
	logger    *zap.Logger
}

// NewSyncer creates a new Syncer.
func NewSyncer(
	connector Connector,
	detector *ChangeDetector,
	queue *jobs.Queue,
	publisher *messaging.Publisher,
	m *metrics.Metrics,
	logger *zap.Logger,
) *Syncer {
	return &Syncer{
		connector: connector,
		detector:  detector,
		jobQueue:  queue,
		publisher: publisher,
		metrics:   m,
		logger:    logger,
	}
}

// SyncConnection fetches tasks from a source connection and creates jobs for new/changed tasks.
func (s *Syncer) SyncConnection(ctx context.Context, conn models.SourceConnection) error {
	rawTasks, err := s.connector.FetchTasks(ctx, conn)
	if err != nil {
		return fmt.Errorf("fetch tasks from %s: %w", conn.Name, err)
	}

	s.logger.Info("fetched tasks", zap.String("connection", conn.Name), zap.Int("count", len(rawTasks)))
	if s.metrics != nil {
		s.metrics.TasksFetched.Add(float64(len(rawTasks)))
	}

	for _, raw := range rawTasks {
		normalized, err := s.connector.NormalizeTask(raw)
		if err != nil {
			s.logger.Warn("normalize task failed",
				zap.String("external_id", raw.ExternalID),
				zap.Error(err),
			)
			continue
		}

		normalized.Hash = ComputeHash(normalized)

		detection, err := s.detector.Detect(ctx, conn.ID, normalized)
		if err != nil {
			s.logger.Error("detect change failed",
				zap.String("external_id", raw.ExternalID),
				zap.Error(err),
			)
			continue
		}

		if detection.ChangeType == "unchanged" {
			continue
		}

		if s.metrics != nil {
			s.metrics.TasksChanged.Inc()
		}

		dedupeKey := fmt.Sprintf("%s:%s:%s", conn.ID.String(), raw.ExternalID, detection.NewHash)
		job, err := s.jobQueue.Enqueue(ctx, jobs.EnqueueParams{
			SourceTaskID: detection.SourceTaskID,
			JobType:      "llm_rewrite",
			Priority:     0,
			DedupeKey:    &dedupeKey,
			MaxAttempts:  3,
		})
		if err != nil {
			s.logger.Error("enqueue job failed",
				zap.String("external_id", raw.ExternalID),
				zap.Error(err),
			)
			continue
		}

		if job == nil {
			// Duplicate – job already active.
			continue
		}

		if s.metrics != nil {
			s.metrics.JobsCreated.Inc()
		}

		// Publish the appropriate event based on change type.
		if detection.ChangeType == "new" {
			evt := events.NewSourceTaskDetectedEvent(
				detection.SourceTaskID.String(),
				conn.ID.String(),
				raw.ExternalID,
				detection.ChangeType,
				time.Now().UTC(),
			)
			if err := s.publisher.Publish(ctx, subjects.SubjectSourceTaskDetected, evt); err != nil {
				s.logger.Warn("publish detected event failed", zap.Error(err))
			}
		} else {
			evt := events.NewSourceTaskChangedEvent(
				detection.SourceTaskID.String(),
				detection.PreviousHash,
				detection.NewHash,
				time.Now().UTC(),
			)
			if err := s.publisher.Publish(ctx, subjects.SubjectSourceTaskChanged, evt); err != nil {
				s.logger.Warn("publish changed event failed", zap.Error(err))
			}
		}

		jobEvt := events.NewJobCreatedEvent(
			job.ID.String(),
			detection.SourceTaskID.String(),
			job.JobType,
			job.Priority,
			dedupeKey,
		)
		if err := s.publisher.Publish(ctx, subjects.SubjectJobCreated, jobEvt); err != nil {
			s.logger.Warn("publish job created event failed", zap.Error(err))
		}
	}

	return nil
}
