package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
	"github.com/tiroq/arcanum/internal/contracts/events"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/processors"
)

// heartbeatInterval is how often a running job renews its lease.
// Must be well below leaseDuration (5 min) so at least 4 renewals can fire
// before the initial lease expires.
const heartbeatInterval = 60 * time.Second

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

	// jobCtx is the context passed to the processor. Cancelling it signals the
	// processor to abort — it is cancelled by the heartbeat if ownership is lost.
	// audit.JobIDKey is embedded so the AuditedProvider can correlate LLM calls.
	jobCtx, cancelJob := context.WithCancel(ctx)
	defer cancelJob()
	jobCtx = context.WithValue(jobCtx, audit.JobIDKey, job.ID)

	// hbCtx scopes the heartbeat goroutine's lifetime to this RunJob call.
	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()

	// Start lease heartbeat. Renews lease_expiry every heartbeatInterval.
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				renewed, renewErr := w.queue.RenewLease(hbCtx, job.ID, w.workerID)
				if renewErr != nil {
					// Transient DB error — log and wait for next tick.
					w.logger.Warn("lease renewal error",
						zap.String("job_id", job.ID.String()),
						zap.Error(renewErr),
					)
					return
				}
				if !renewed {
					// Ownership was lost — reclaim happened and another worker
					// may have re-leased the job. Cancel the processor context so
					// it can abort. Complete/Fail will be no-ops (ownership guard).
					w.logger.Error("lease ownership lost during execution — aborting job",
						zap.String("job_id", job.ID.String()),
						zap.String("worker_id", w.workerID),
					)
					if w.metrics != nil {
						w.metrics.LeaseRenewalLost.Inc()
					}
					if w.audit != nil {
						//nolint:errcheck
						w.audit.RecordEvent(hbCtx, "job", job.ID, "job.lease_lost", "worker", w.workerID, map[string]any{
							"worker_id": w.workerID,
							"reason":    "lease_expired_and_reclaimed",
						})
					}
					// Emit a bus-visible control alert so the event log captures lease losses
					// even when auditing is unavailable.
					lostEvt := events.NewLeaseLostAlertEvent(job.ID.String(), w.workerID)
					if pubErr := w.publisher.Publish(hbCtx, subjects.SubjectControlAlertLeaseLost, lostEvt); pubErr != nil {
						w.logger.Warn("control: publish lease_lost alert failed", zap.Error(pubErr))
					}
					cancelJob()
					return
				}
				w.logger.Debug("lease renewed",
					zap.String("job_id", job.ID.String()),
				)
				if w.metrics != nil {
					w.metrics.LeaseRenewals.Inc()
				}
			}
		}
	}()

	startedAt := time.Now().UTC()
	result, err := proc.Process(jobCtx, jc)
	// Stop heartbeat now — we are past the processing phase.
	cancelHB()

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
			"provider":          result.ModelProvider,
			"model_role":        result.ModelRole.String(),
			"model_name":        result.ModelName,
			"tokens_prompt":     result.TokensPrompt,
			"tokens_completion": result.TokensCompletion,
			"tokens_total":      result.TokensUsed,
			"timeout_used_s":    result.TimeoutUsed.Seconds(),
			// Explicit fallback signal: true when the provider used its default
			// model as a stand-in because no role-specific model was configured.
			// Stored directly so the optimizer can use it as a first-class signal
			// rather than inferring it from the failure rate proxy.
			"used_fallback":  result.UsedFallback,
			// attempt_number is the 1-based counter for this execution attempt
			// on the job. It acts as fallback depth for multi-attempt analysis.
			"attempt_number": job.AttemptCount + 1,
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
			if w.audit != nil {
				//nolint:errcheck
				w.audit.RecordEvent(ctx, "proposal", proposalID, "proposal.created", "worker", w.workerID, map[string]any{
					"proposal_id":    proposalID.String(),
					"source_task_id": job.SourceTaskID.String(),
					"proposal_type":  result.ProposalType,
					"job_id":         job.ID.String(),
				})
			}
		}
	}

	if err := w.queue.Complete(ctx, job.ID, w.workerID); err != nil {
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
	if err := w.queue.Fail(ctx, jobID, w.workerID, code, msg); err != nil {
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
