package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/contracts/events"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/processors"
)

// RunJob executes a single job end-to-end.
func (w *Worker) RunJob(ctx context.Context, job *models.ProcessingJob) error {
	w.logger.Info("running job",
		zap.String("job_id", job.ID.String()),
		zap.String("job_type", job.JobType),
	)

	proc, err := w.registry.FindFor(job.JobType)
	if err != nil {
		return w.failJob(ctx, job.ID, "NO_PROCESSOR", err.Error())
	}

	// Build job context: load snapshot payload.
	snapshotPayload, err := w.loadSnapshotPayload(ctx, job.SourceTaskID)
	if err != nil {
		w.logger.Warn("could not load snapshot payload", zap.Error(err))
		snapshotPayload = json.RawMessage("{}")
	}

	jc := processors.JobContext{
		JobID:           job.ID,
		SourceTaskID:    job.SourceTaskID,
		JobType:         job.JobType,
		SnapshotPayload: snapshotPayload,
	}

	startedAt := time.Now().UTC()
	result, err := proc.Process(ctx, jc)
	finishedAt := time.Now().UTC()
	durationMS := finishedAt.Sub(startedAt).Milliseconds()

	runID := uuid.New()
	outcome := result.Outcome
	if err != nil {
		outcome = models.RunOutcomeError
	}

	errMsg := result.ErrorMessage
	if err != nil && errMsg == "" {
		errMsg = err.Error()
	}

	var errMsgPtr *string
	if errMsg != "" {
		errMsgPtr = &errMsg
	}

	// Build result payload with model role and resolved model info for audit.
	runResultPayload := result.OutputPayload
	if result.ModelProvider != "" || result.ModelRole != "" {
		meta := map[string]interface{}{
			"provider":       result.ModelProvider,
			"model_role":     result.ModelRole.String(),
			"model_name":     result.ModelName,
			"tokens_used":    result.TokensUsed,
			"timeout_used_s": result.TimeoutUsed.Seconds(),
		}
		if result.OutputPayload != nil {
			meta["output"] = json.RawMessage(result.OutputPayload)
		}
		if result.ExecutionTrace != nil {
			meta["execution_trace"] = json.RawMessage(result.ExecutionTrace)
		}
		if enriched, mErr := json.Marshal(meta); mErr == nil {
			runResultPayload = enriched
		}
	}

	// Persist ProcessingRun.
	const insertRun = `
		INSERT INTO processing_runs (id, job_id, attempt_number, outcome, started_at, finished_at, duration_ms, error_message, result_payload, worker_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	if _, dbErr := w.db.Exec(ctx, insertRun,
		runID, job.ID, job.AttemptCount+1, outcome,
		startedAt, finishedAt, durationMS,
		errMsgPtr, runResultPayload, w.workerID,
	); dbErr != nil {
		w.logger.Error("persist processing run failed", zap.Error(dbErr))
	}

	if outcome != models.RunOutcomeSuccess {
		return w.failJob(ctx, job.ID, "PROCESSING_FAILED", errMsg)
	}

	// Create SuggestionProposal if there's an output payload.
	if len(result.OutputPayload) > 0 && result.ProposalType != "" {
		proposalID := uuid.New()
		const insertProposal = `
			INSERT INTO suggestion_proposals (id, source_task_id, job_id, proposal_type, approval_status, human_review_required, proposal_payload, auto_approved, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`
		now := time.Now().UTC()
		if _, dbErr := w.db.Exec(ctx, insertProposal,
			proposalID, job.SourceTaskID, job.ID,
			result.ProposalType, models.ApprovalStatusPending,
			result.HumanReviewRequired, result.OutputPayload,
			false, now,
		); dbErr != nil {
			w.logger.Error("persist proposal failed", zap.Error(dbErr))
		} else {
			evt := events.NewProposalCreatedEvent(
				proposalID.String(),
				job.SourceTaskID.String(),
				result.ProposalType,
				result.HumanReviewRequired,
			)
			if pubErr := w.publisher.Publish(ctx, subjects.SubjectProposalCreated, evt); pubErr != nil {
				w.logger.Warn("publish proposal created failed", zap.Error(pubErr))
			}
		}
	}

	if err := w.queue.Complete(ctx, job.ID); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	if w.metrics != nil {
		w.metrics.JobsSucceeded.Inc()
	}

	w.logger.Info("job completed",
		zap.String("job_id", job.ID.String()),
		zap.String("outcome", outcome),
		zap.String("model_provider", result.ModelProvider),
		zap.String("model_role", result.ModelRole.String()),
		zap.String("model_name", result.ModelName),
		zap.Int64("duration_ms", durationMS),
	)
	return nil
}

func (w *Worker) failJob(ctx context.Context, jobID uuid.UUID, code, msg string) error {
	if err := w.queue.Fail(ctx, jobID, code, msg); err != nil {
		return fmt.Errorf("fail job %s: %w", jobID, err)
	}
	if w.metrics != nil {
		w.metrics.JobsFailed.Inc()
	}
	return nil
}

func (w *Worker) loadSnapshotPayload(ctx context.Context, sourceTaskID uuid.UUID) (json.RawMessage, error) {
	const query = `
		SELECT s.raw_payload
		FROM source_task_snapshots s
		WHERE s.source_task_id = $1
		ORDER BY s.snapshot_version DESC
		LIMIT 1`
	var payload json.RawMessage
	if err := w.db.QueryRow(ctx, query, sourceTaskID).Scan(&payload); err != nil {
		return nil, fmt.Errorf("load snapshot: %w", err)
	}
	return payload, nil
}
