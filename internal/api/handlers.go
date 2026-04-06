package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/contracts/events"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
)

// Handlers holds all HTTP handler dependencies.
type Handlers struct {
	db        *pgxpool.Pool
	publisher *messaging.Publisher
	metrics   *metrics.Metrics
	logger    *zap.Logger
}

// NewHandlers creates Handlers with required dependencies.
func NewHandlers(db *pgxpool.Pool, publisher *messaging.Publisher, m *metrics.Metrics, logger *zap.Logger) *Handlers {
	return &Handlers{db: db, publisher: publisher, metrics: m, logger: logger}
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	reqID, _ := r.Context().Value(ctxKeyRequestID{}).(string)
	writeJSON(w, status, map[string]string{
		"error":      msg,
		"request_id": reqID,
	})
}

// --- Pagination helpers ---

type pagination struct {
	Page    int
	PerPage int
	Offset  int
}

func parsePagination(r *http.Request) pagination {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return pagination{Page: page, PerPage: perPage, Offset: (page - 1) * perPage}
}

// --- Source Connections ---

func (h *Handlers) SourceConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listSourceConnections(w, r)
	case http.MethodPost:
		h.createSourceConnection(w, r)
	default:
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handlers) SourceConnectionByID(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromPath(r, "/api/v1/source-connections/")
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getSourceConnection(w, r, id)
	case http.MethodPut:
		h.updateSourceConnection(w, r, id)
	case http.MethodDelete:
		h.deleteSourceConnection(w, r, id)
	default:
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handlers) listSourceConnections(w http.ResponseWriter, r *http.Request) {
	pg := parsePagination(r)
	const q = `SELECT id, name, provider, config, enabled, last_synced_at, created_at, updated_at FROM source_connections ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := h.db.Query(r.Context(), q, pg.PerPage, pg.Offset)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var conns []models.SourceConnection
	for rows.Next() {
		var c models.SourceConnection
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.Config, &c.Enabled, &c.LastSyncedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			continue
		}
		conns = append(conns, c)
	}
	writeJSON(w, http.StatusOK, conns)
}

func (h *Handlers) createSourceConnection(w http.ResponseWriter, r *http.Request) {
	var req models.SourceConnection
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	now := time.Now().UTC()
	req.ID = uuid.New()
	req.CreatedAt = now
	req.UpdatedAt = now

	const q = `INSERT INTO source_connections (id, name, provider, config, enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`
	if _, err := h.db.Exec(r.Context(), q, req.ID, req.Name, req.Provider, req.Config, req.Enabled, now); err != nil {
		writeError(w, r, http.StatusInternalServerError, "insert failed")
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (h *Handlers) getSourceConnection(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	const q = `SELECT id, name, provider, config, enabled, last_synced_at, created_at, updated_at FROM source_connections WHERE id = $1`
	var c models.SourceConnection
	if err := h.db.QueryRow(r.Context(), q, id).Scan(&c.ID, &c.Name, &c.Provider, &c.Config, &c.Enabled, &c.LastSyncedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *Handlers) updateSourceConnection(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req models.SourceConnection
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	now := time.Now().UTC()
	const q = `UPDATE source_connections SET name=$1, provider=$2, config=$3, enabled=$4, updated_at=$5 WHERE id=$6`
	tag, err := h.db.Exec(r.Context(), q, req.Name, req.Provider, req.Config, req.Enabled, now, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, r, http.StatusNotFound, "not found or update failed")
		return
	}
	req.ID = id
	req.UpdatedAt = now
	writeJSON(w, http.StatusOK, req)
}

func (h *Handlers) deleteSourceConnection(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	const q = `DELETE FROM source_connections WHERE id = $1`
	tag, err := h.db.Exec(r.Context(), q, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Source Tasks ---

func (h *Handlers) SourceTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pg := parsePagination(r)
	statusFilter := r.URL.Query().Get("status")

	var (
		rows pgx.Rows
		err  error
	)
	if statusFilter != "" {
		const q = `SELECT id, source_connection_id, external_id, title, description, raw_payload, content_hash, status, priority, due_at, created_at, updated_at FROM source_tasks WHERE status=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		rows, err = h.db.Query(r.Context(), q, statusFilter, pg.PerPage, pg.Offset)
	} else {
		const q = `SELECT id, source_connection_id, external_id, title, description, raw_payload, content_hash, status, priority, due_at, created_at, updated_at FROM source_tasks ORDER BY created_at DESC LIMIT $1 OFFSET $2`
		rows, err = h.db.Query(r.Context(), q, pg.PerPage, pg.Offset)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var tasks []models.SourceTask
	for rows.Next() {
		var t models.SourceTask
		if err := rows.Scan(&t.ID, &t.SourceConnectionID, &t.ExternalID, &t.Title, &t.Description, &t.RawPayload, &t.ContentHash, &t.Status, &t.Priority, &t.DueAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		tasks = append(tasks, t)
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handlers) SourceTaskRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/source-tasks/")
	parts := strings.SplitN(path, "/", 2)

	id, err := uuid.Parse(parts[0])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}

	if len(parts) == 2 {
		switch parts[1] {
		case "snapshots":
			h.listSnapshots(w, r, id)
		case "resync":
			h.resyncTask(w, r, id)
		default:
			writeError(w, r, http.StatusNotFound, "not found")
		}
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h.getSourceTask(w, r, id)
}

func (h *Handlers) getSourceTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	const q = `SELECT id, source_connection_id, external_id, title, description, raw_payload, content_hash, status, priority, due_at, created_at, updated_at FROM source_tasks WHERE id=$1`
	var t models.SourceTask
	if err := h.db.QueryRow(r.Context(), q, id).Scan(
		&t.ID, &t.SourceConnectionID, &t.ExternalID, &t.Title, &t.Description,
		&t.RawPayload, &t.ContentHash, &t.Status, &t.Priority, &t.DueAt, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *Handlers) listSnapshots(w http.ResponseWriter, r *http.Request, taskID uuid.UUID) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pg := parsePagination(r)
	const q = `SELECT id, source_task_id, snapshot_version, content_hash, raw_payload, snapshot_taken_at FROM source_task_snapshots WHERE source_task_id=$1 ORDER BY snapshot_version DESC LIMIT $2 OFFSET $3`
	rows, err := h.db.Query(r.Context(), q, taskID, pg.PerPage, pg.Offset)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var snaps []models.SourceTaskSnapshot
	for rows.Next() {
		var s models.SourceTaskSnapshot
		if err := rows.Scan(&s.ID, &s.SourceTaskID, &s.SnapshotVersion, &s.ContentHash, &s.RawPayload, &s.SnapshotTakenAt); err != nil {
			continue
		}
		snaps = append(snaps, s)
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (h *Handlers) resyncTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cmd := events.NewTaskResyncCommandEvent(id.String(), "llm_rewrite", 1)
	if err := h.publisher.Publish(r.Context(), subjects.SubjectCommandTaskResync, cmd); err != nil {
		h.logger.Error("publish resync command", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "publish failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// --- Jobs ---

func (h *Handlers) Jobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pg := parsePagination(r)
	statusFilter := r.URL.Query().Get("status")

	var (
		rows pgx.Rows
		err  error
	)
	if statusFilter != "" {
		const q = `SELECT id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, leased_at, lease_expiry, scheduled_at, created_at, updated_at FROM processing_jobs WHERE status=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		rows, err = h.db.Query(r.Context(), q, statusFilter, pg.PerPage, pg.Offset)
	} else {
		const q = `SELECT id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, leased_at, lease_expiry, scheduled_at, created_at, updated_at FROM processing_jobs ORDER BY created_at DESC LIMIT $1 OFFSET $2`
		rows, err = h.db.Query(r.Context(), q, pg.PerPage, pg.Offset)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var jobList []models.ProcessingJob
	for rows.Next() {
		var j models.ProcessingJob
		if err := rows.Scan(&j.ID, &j.SourceTaskID, &j.JobType, &j.Status, &j.Priority, &j.DedupeKey, &j.AttemptCount, &j.MaxAttempts, &j.Payload, &j.LeasedAt, &j.LeaseExpiry, &j.ScheduledAt, &j.CreatedAt, &j.UpdatedAt); err != nil {
			continue
		}
		jobList = append(jobList, j)
	}
	writeJSON(w, http.StatusOK, jobList)
}

func (h *Handlers) JobRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/")
	parts := strings.SplitN(path, "/", 2)

	id, err := uuid.Parse(parts[0])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}

	if len(parts) == 2 && parts[1] == "retry" {
		h.retryJob(w, r, id)
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	const jobQuery = `SELECT id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, leased_at, lease_expiry, scheduled_at, created_at, updated_at FROM processing_jobs WHERE id = $1`
	var job models.ProcessingJob
	if err := h.db.QueryRow(r.Context(), jobQuery, id).Scan(
		&job.ID, &job.SourceTaskID, &job.JobType, &job.Status, &job.Priority,
		&job.DedupeKey, &job.AttemptCount, &job.MaxAttempts, &job.Payload,
		&job.LeasedAt, &job.LeaseExpiry, &job.ScheduledAt, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handlers) retryJob(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cmd := events.NewJobRetryCommandEvent(id.String())
	if err := h.publisher.Publish(r.Context(), subjects.SubjectCommandJobRetry, cmd); err != nil {
		h.logger.Error("publish retry command", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "publish failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// --- Proposals ---

func (h *Handlers) Proposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pg := parsePagination(r)
	statusFilter := r.URL.Query().Get("status")

	var (
		rows pgx.Rows
		err  error
	)
	if statusFilter != "" {
		const q = `SELECT id, source_task_id, job_id, proposal_type, approval_status, human_review_required, proposal_payload, approved_by, auto_approved, reviewed_at, created_at, updated_at FROM suggestion_proposals WHERE approval_status=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		rows, err = h.db.Query(r.Context(), q, statusFilter, pg.PerPage, pg.Offset)
	} else {
		const q = `SELECT id, source_task_id, job_id, proposal_type, approval_status, human_review_required, proposal_payload, approved_by, auto_approved, reviewed_at, created_at, updated_at FROM suggestion_proposals ORDER BY created_at DESC LIMIT $1 OFFSET $2`
		rows, err = h.db.Query(r.Context(), q, pg.PerPage, pg.Offset)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var proposals []models.SuggestionProposal
	for rows.Next() {
		var p models.SuggestionProposal
		if err := rows.Scan(&p.ID, &p.SourceTaskID, &p.JobID, &p.ProposalType, &p.ApprovalStatus, &p.HumanReviewRequired, &p.ProposalPayload, &p.ApprovedBy, &p.AutoApproved, &p.ReviewedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		proposals = append(proposals, p)
	}
	writeJSON(w, http.StatusOK, proposals)
}

func (h *Handlers) ProposalRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/proposals/")
	parts := strings.SplitN(path, "/", 2)

	id, err := uuid.Parse(parts[0])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}

	if len(parts) == 2 {
		switch parts[1] {
		case "approve":
			h.approveProposal(w, r, id)
			return
		case "reject":
			h.rejectProposal(w, r, id)
			return
		}
	}

	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h.getProposal(w, r, id)
}

func (h *Handlers) getProposal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	const q = `SELECT id, source_task_id, job_id, proposal_type, approval_status, human_review_required, proposal_payload, approved_by, auto_approved, reviewed_at, created_at, updated_at FROM suggestion_proposals WHERE id=$1`
	var p models.SuggestionProposal
	if err := h.db.QueryRow(r.Context(), q, id).Scan(
		&p.ID, &p.SourceTaskID, &p.JobID, &p.ProposalType, &p.ApprovalStatus,
		&p.HumanReviewRequired, &p.ProposalPayload, &p.ApprovedBy, &p.AutoApproved, &p.ReviewedAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handlers) approveProposal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	const q = `UPDATE suggestion_proposals SET approval_status='approved', reviewed_at=$1, updated_at=$1 WHERE id=$2 AND approval_status='pending'`
	tag, err := h.db.Exec(r.Context(), q, now, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, r, http.StatusBadRequest, "proposal cannot be approved")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (h *Handlers) rejectProposal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	const q = `UPDATE suggestion_proposals SET approval_status='rejected', reviewed_at=$1, updated_at=$1 WHERE id=$2 AND approval_status='pending'`
	tag, err := h.db.Exec(r.Context(), q, now, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, r, http.StatusBadRequest, "proposal cannot be rejected")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// --- Processor Runs ---

func (h *Handlers) ProcessorRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pg := parsePagination(r)
	const q = `SELECT id, job_id, attempt_number, outcome, started_at, finished_at, duration_ms, error_message, result_payload, worker_id FROM processing_runs ORDER BY started_at DESC LIMIT $1 OFFSET $2`
	rows, err := h.db.Query(r.Context(), q, pg.PerPage, pg.Offset)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var runs []models.ProcessingRun
	for rows.Next() {
		var run models.ProcessingRun
		if err := rows.Scan(&run.ID, &run.JobID, &run.AttemptNumber, &run.Outcome, &run.StartedAt, &run.FinishedAt, &run.DurationMs, &run.ErrorMessage, &run.ResultPayload, &run.WorkerID); err != nil {
			continue
		}
		runs = append(runs, run)
	}
	writeJSON(w, http.StatusOK, runs)
}

func (h *Handlers) ProcessorRunByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, ok := parseIDFromPath(r, "/api/v1/processor-runs/")
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}
	const q = `SELECT id, job_id, attempt_number, outcome, started_at, finished_at, duration_ms, error_message, result_payload, worker_id FROM processing_runs WHERE id=$1`
	var run models.ProcessingRun
	if err := h.db.QueryRow(r.Context(), q, id).Scan(&run.ID, &run.JobID, &run.AttemptNumber, &run.Outcome, &run.StartedAt, &run.FinishedAt, &run.DurationMs, &run.ErrorMessage, &run.ResultPayload, &run.WorkerID); err != nil {
		writeError(w, r, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// --- Audit Events ---

func (h *Handlers) AuditEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pg := parsePagination(r)
	const q = `SELECT id, entity_type, entity_id, event_type, actor_type, actor_id, payload, occurred_at FROM audit_events ORDER BY occurred_at DESC LIMIT $1 OFFSET $2`
	rows, err := h.db.Query(r.Context(), q, pg.PerPage, pg.Offset)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var evts []models.AuditEvent
	for rows.Next() {
		var e models.AuditEvent
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.EventType, &e.ActorType, &e.ActorID, &e.Payload, &e.OccurredAt); err != nil {
			continue
		}
		evts = append(evts, e)
	}
	writeJSON(w, http.StatusOK, evts)
}

// --- Metrics Summary ---

func (h *Handlers) MetricsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	summary := map[string]interface{}{
		"jobs_queued":    h.countByStatus(r.Context(), "queued"),
		"jobs_running":   h.countByStatus(r.Context(), "running"),
		"jobs_succeeded": h.countByStatus(r.Context(), "succeeded"),
		"jobs_failed":    h.countByStatus(r.Context(), "failed"),
		"jobs_dead":      h.countByStatus(r.Context(), "dead_letter"),
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handlers) countByStatus(ctx context.Context, status string) int64 {
	var count int64
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM processing_jobs WHERE status=$1`, status).Scan(&count) //nolint:errcheck
	return count
}

// --- Agent Timeline ---

// AgentTimeline returns the full event journal and derived episodic memories
// for a given correlation_id (= job_id).
// GET /api/v1/agent/timeline/{id}
func (h *Handlers) AgentTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/agent/timeline/")
	idStr = strings.TrimSuffix(idStr, "/")
	correlationID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid correlation_id")
		return
	}

	type agentEventRow struct {
		EventID       uuid.UUID       `json:"event_id"`
		EventType     string          `json:"event_type"`
		Source        string          `json:"source"`
		Timestamp     time.Time       `json:"timestamp"`
		CorrelationID *uuid.UUID      `json:"correlation_id,omitempty"`
		CausationID   *uuid.UUID      `json:"causation_id,omitempty"`
		Priority      int             `json:"priority"`
		Confidence    float64         `json:"confidence"`
		Payload       json.RawMessage `json:"payload"`
		Tags          []string        `json:"tags"`
	}

	const eventsQ = `
		SELECT event_id, event_type, source, timestamp,
		       correlation_id, causation_id, priority, confidence, payload, tags
		FROM agent_events
		WHERE correlation_id = $1
		ORDER BY timestamp ASC`

	eRows, err := h.db.Query(r.Context(), eventsQ, correlationID)
	if err != nil {
		h.logger.Error("query agent events", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "failed to query events")
		return
	}
	defer eRows.Close()

	evts := make([]agentEventRow, 0)
	for eRows.Next() {
		var e agentEventRow
		if err := eRows.Scan(
			&e.EventID, &e.EventType, &e.Source, &e.Timestamp,
			&e.CorrelationID, &e.CausationID,
			&e.Priority, &e.Confidence, &e.Payload, &e.Tags,
		); err != nil {
			h.logger.Error("scan agent event", zap.Error(err))
			writeError(w, r, http.StatusInternalServerError, "scan failed")
			return
		}
		evts = append(evts, e)
	}
	if err := eRows.Err(); err != nil {
		writeError(w, r, http.StatusInternalServerError, "events rows error")
		return
	}

	type memRow struct {
		ID            uuid.UUID  `json:"id"`
		EventID       uuid.UUID  `json:"event_id"`
		CorrelationID *uuid.UUID `json:"correlation_id,omitempty"`
		Summary       string     `json:"summary"`
		Salience      float64    `json:"salience"`
		CreatedAt     time.Time  `json:"created_at"`
	}

	const memQ = `
		SELECT id, event_id, correlation_id, summary, salience, created_at
		FROM agent_memory_episodic
		WHERE correlation_id = $1
		ORDER BY salience DESC, created_at ASC`

	mRows, err := h.db.Query(r.Context(), memQ, correlationID)
	if err != nil {
		h.logger.Error("query agent memories", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "failed to query memory")
		return
	}
	defer mRows.Close()

	memories := make([]memRow, 0)
	for mRows.Next() {
		var m memRow
		if err := mRows.Scan(
			&m.ID, &m.EventID, &m.CorrelationID,
			&m.Summary, &m.Salience, &m.CreatedAt,
		); err != nil {
			h.logger.Error("scan memory row", zap.Error(err))
			writeError(w, r, http.StatusInternalServerError, "scan failed")
			return
		}
		memories = append(memories, m)
	}
	if err := mRows.Err(); err != nil {
		writeError(w, r, http.StatusInternalServerError, "memory rows error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"correlation_id": correlationID,
		"events":         evts,
		"memory":         memories,
	})
}

// --- Helpers ---

func parseIDFromPath(r *http.Request, prefix string) (uuid.UUID, bool) {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	path = strings.TrimSuffix(path, "/")
	id, err := uuid.Parse(path)
	return id, err == nil
}
