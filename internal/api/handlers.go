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

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/actions"
	"github.com/tiroq/arcanum/internal/agent/causal"
	decision_graph "github.com/tiroq/arcanum/internal/agent/decision_graph"
	"github.com/tiroq/arcanum/internal/agent/exploration"
	"github.com/tiroq/arcanum/internal/agent/goals"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	pathcomparison "github.com/tiroq/arcanum/internal/agent/path_comparison"
	pathlearning "github.com/tiroq/arcanum/internal/agent/path_learning"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/agent/policy"
	"github.com/tiroq/arcanum/internal/agent/reflection"
	"github.com/tiroq/arcanum/internal/agent/scheduler"
	"github.com/tiroq/arcanum/internal/agent/stability"
	"github.com/tiroq/arcanum/internal/agent/strategy"
	strategylearning "github.com/tiroq/arcanum/internal/agent/strategy_learning"
	"github.com/tiroq/arcanum/internal/contracts/events"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
)

// Handlers holds all HTTP handler dependencies.
type Handlers struct {
	db                *pgxpool.Pool
	publisher         *messaging.Publisher
	metrics           *metrics.Metrics
	goalEngine        *goals.GoalEngine
	actionEngine      *actions.Engine
	outcomeStore      *outcome.Store
	actionMemoryStore *actionmemory.Store
	adaptivePlanner   *planning.AdaptivePlanner
	decisionJournal   *planning.DecisionJournal
	reflectionEngine  *reflection.Engine
	reflectionStore   *reflection.Store
	scheduler         *scheduler.Scheduler
	schedulerEnabled  bool
	stabilityEngine   *stability.Engine
	policyEngine      *policy.Engine
	causalEngine      *causal.Engine
	explorationEngine *exploration.Engine
	strategyEngine    *strategy.Engine
	strategyLearning  *strategylearning.MemoryStore
	decisionGraph     *decision_graph.GraphPlannerAdapter
	pathMemoryStore   *pathlearning.MemoryStore
	transitionStore   *pathlearning.TransitionStore
	compSnapshotStore *pathcomparison.SnapshotStore
	compOutcomeStore  *pathcomparison.OutcomeStore
	compMemoryStore   *pathcomparison.MemoryStore
	logger            *zap.Logger
}

// NewHandlers creates Handlers with required dependencies.
func NewHandlers(db *pgxpool.Pool, publisher *messaging.Publisher, m *metrics.Metrics, logger *zap.Logger) *Handlers {
	return &Handlers{db: db, publisher: publisher, metrics: m, logger: logger}
}

// WithGoalEngine attaches an optional GoalEngine to the handlers.
func (h *Handlers) WithGoalEngine(ge *goals.GoalEngine) *Handlers {
	h.goalEngine = ge
	return h
}

// WithActionEngine attaches an optional ActionEngine to the handlers.
func (h *Handlers) WithActionEngine(ae *actions.Engine) *Handlers {
	h.actionEngine = ae
	return h
}

// WithOutcomeStore attaches an optional outcome store to the handlers.
func (h *Handlers) WithOutcomeStore(os *outcome.Store) *Handlers {
	h.outcomeStore = os
	return h
}

// WithActionMemoryStore attaches an optional action memory store to the handlers.
func (h *Handlers) WithActionMemoryStore(ms *actionmemory.Store) *Handlers {
	h.actionMemoryStore = ms
	return h
}

// WithAdaptivePlanner attaches an optional adaptive planner for read-only visibility.
func (h *Handlers) WithAdaptivePlanner(ap *planning.AdaptivePlanner) *Handlers {
	h.adaptivePlanner = ap
	return h
}

// WithScheduler attaches an optional autonomous scheduler.
func (h *Handlers) WithScheduler(s *scheduler.Scheduler, enabled bool) *Handlers {
	h.scheduler = s
	h.schedulerEnabled = enabled
	return h
}

// WithDecisionJournal attaches an optional decision journal for durable planning history.
func (h *Handlers) WithDecisionJournal(j *planning.DecisionJournal) *Handlers {
	h.decisionJournal = j
	return h
}

// WithReflectionEngine attaches the reflection engine and its store.
func (h *Handlers) WithReflectionEngine(e *reflection.Engine, s *reflection.Store) *Handlers {
	h.reflectionEngine = e
	h.reflectionStore = s
	return h
}

// WithStabilityEngine attaches the self-stability engine.
func (h *Handlers) WithStabilityEngine(se *stability.Engine) *Handlers {
	h.stabilityEngine = se
	return h
}

// WithPolicyEngine attaches the policy adaptation engine.
func (h *Handlers) WithPolicyEngine(pe *policy.Engine) *Handlers {
	h.policyEngine = pe
	return h
}

// WithCausalEngine attaches the causal reasoning engine.
func (h *Handlers) WithCausalEngine(ce *causal.Engine) *Handlers {
	h.causalEngine = ce
	return h
}

// WithExplorationEngine attaches the exploration engine.
func (h *Handlers) WithExplorationEngine(ee *exploration.Engine) *Handlers {
	h.explorationEngine = ee
	return h
}

// WithStrategyEngine attaches the strategy engine.
func (h *Handlers) WithStrategyEngine(se *strategy.Engine) *Handlers {
	h.strategyEngine = se
	return h
}

// WithStrategyLearning attaches the strategy learning memory store.
func (h *Handlers) WithStrategyLearning(sl *strategylearning.MemoryStore) *Handlers {
	h.strategyLearning = sl
	return h
}

// WithDecisionGraph attaches the decision graph planner adapter.
func (h *Handlers) WithDecisionGraph(dg *decision_graph.GraphPlannerAdapter) *Handlers {
	h.decisionGraph = dg
	return h
}

// WithPathLearning attaches the path learning memory and transition stores.
func (h *Handlers) WithPathLearning(ms *pathlearning.MemoryStore, ts *pathlearning.TransitionStore) *Handlers {
	h.pathMemoryStore = ms
	h.transitionStore = ts
	return h
}

// WithPathComparison attaches the path comparison stores.
func (h *Handlers) WithPathComparison(ss *pathcomparison.SnapshotStore, os *pathcomparison.OutcomeStore, ms *pathcomparison.MemoryStore) *Handlers {
	h.compSnapshotStore = ss
	h.compOutcomeStore = os
	h.compMemoryStore = ms
	return h
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
		"jobs_queued":          h.countByStatus(r.Context(), "queued"),
		"jobs_leased":          h.countByStatus(r.Context(), "leased"),
		"jobs_retry_scheduled": h.countByStatus(r.Context(), "retry_scheduled"),
		"jobs_succeeded":       h.countByStatus(r.Context(), "succeeded"),
		"jobs_failed":          h.countByStatus(r.Context(), "failed"),
		"jobs_dead":            h.countByStatus(r.Context(), "dead_letter"),
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

// --- Agent Goals ---

// AgentGoals returns advisory goals derived from current system state.
// GET /api/v1/agent/goals
func (h *Handlers) AgentGoals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.goalEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "goal engine not configured")
		return
	}

	result, err := h.goalEngine.Evaluate(r.Context())
	if err != nil {
		h.logger.Error("goal engine evaluation failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "goal evaluation failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"goals": result,
	})
}

// --- Agent Actions ---

// AgentActions returns the latest action audit trail.
// GET /api/v1/agent/actions
func (h *Handlers) AgentActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pg := parsePagination(r)
	const query = `
		SELECT id, entity_type, entity_id, event_type, actor_type, actor_id, payload, occurred_at
		FROM audit_events
		WHERE entity_type = 'action'
		ORDER BY occurred_at DESC
		LIMIT $1 OFFSET $2`
	rows, err := h.db.Query(r.Context(), query, pg.PerPage, pg.Offset)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var auditRows []models.AuditEvent
	for rows.Next() {
		var ae models.AuditEvent
		if err := rows.Scan(&ae.ID, &ae.EntityType, &ae.EntityID, &ae.EventType,
			&ae.ActorType, &ae.ActorID, &ae.Payload, &ae.OccurredAt); err != nil {
			continue
		}
		auditRows = append(auditRows, ae)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"actions": auditRows,
	})
}

// RunActions triggers a single action engine cycle.
// POST /api/v1/agent/run-actions
func (h *Handlers) RunActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.actionEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "action engine not configured")
		return
	}

	report, err := h.actionEngine.RunCycle(r.Context())
	if err != nil {
		h.logger.Error("action engine cycle failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "action cycle failed")
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// AgentOutcomes returns stored action outcome evaluations.
// GET /api/v1/agent/outcomes?action_id=...&target_id=...
func (h *Handlers) AgentOutcomes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.outcomeStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "outcome store not configured")
		return
	}

	pg := parsePagination(r)
	f := outcome.ListFilter{
		Limit:  pg.PerPage,
		Offset: pg.Offset,
	}

	if raw := r.URL.Query().Get("action_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			f.ActionID = &id
		}
	}
	if raw := r.URL.Query().Get("target_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			f.TargetID = &id
		}
	}

	outcomes, err := h.outcomeStore.List(r.Context(), f)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"outcomes": outcomes,
	})
}

// AgentActionMemory returns accumulated action memory with feedback signals.
// GET /api/v1/agent/action-memory
func (h *Handlers) AgentActionMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.actionMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "action memory store not configured")
		return
	}

	records, err := h.actionMemoryStore.List(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	type memoryEntry struct {
		ActionType     string  `json:"action_type"`
		TargetType     string  `json:"target_type"`
		TotalRuns      int     `json:"total_runs"`
		SuccessRuns    int     `json:"success_runs"`
		FailureRuns    int     `json:"failure_runs"`
		NeutralRuns    int     `json:"neutral_runs"`
		SuccessRate    float64 `json:"success_rate"`
		FailureRate    float64 `json:"failure_rate"`
		Recommendation string  `json:"recommendation"`
	}

	entries := make([]memoryEntry, 0, len(records))
	for _, rec := range records {
		fb := actionmemory.GenerateFeedback(&rec)
		entries = append(entries, memoryEntry{
			ActionType:     rec.ActionType,
			TargetType:     rec.TargetType,
			TotalRuns:      rec.TotalRuns,
			SuccessRuns:    rec.SuccessRuns,
			FailureRuns:    rec.FailureRuns,
			NeutralRuns:    rec.NeutralRuns,
			SuccessRate:    rec.SuccessRate,
			FailureRate:    rec.FailureRate,
			Recommendation: string(fb.Recommendation),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"memory": entries,
	})
}

// AgentContextMemory returns contextual action memory with feedback signals.
// GET /api/v1/agent/action-memory/context
func (h *Handlers) AgentContextMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.actionMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "action memory store not configured")
		return
	}

	records, err := h.actionMemoryStore.ListContextRecords(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	type contextEntry struct {
		ActionType     string  `json:"action_type"`
		GoalType       string  `json:"goal_type"`
		JobType        string  `json:"job_type"`
		FailureBucket  string  `json:"failure_bucket"`
		BacklogBucket  string  `json:"backlog_bucket"`
		TotalRuns      int     `json:"total_runs"`
		SuccessRuns    int     `json:"success_runs"`
		FailureRuns    int     `json:"failure_runs"`
		NeutralRuns    int     `json:"neutral_runs"`
		SuccessRate    float64 `json:"success_rate"`
		FailureRate    float64 `json:"failure_rate"`
		Recommendation string  `json:"recommendation"`
	}

	entries := make([]contextEntry, 0, len(records))
	for _, rec := range records {
		amr := actionmemory.ActionMemoryRecord{
			ActionType:  rec.ActionType,
			TotalRuns:   rec.TotalRuns,
			SuccessRuns: rec.SuccessRuns,
			FailureRuns: rec.FailureRuns,
			NeutralRuns: rec.NeutralRuns,
			SuccessRate: rec.SuccessRate,
			FailureRate: rec.FailureRate,
		}
		fb := actionmemory.GenerateFeedback(&amr)
		entries = append(entries, contextEntry{
			ActionType:     rec.ActionType,
			GoalType:       rec.GoalType,
			JobType:        rec.JobType,
			FailureBucket:  rec.FailureBucket,
			BacklogBucket:  rec.BacklogBucket,
			TotalRuns:      rec.TotalRuns,
			SuccessRuns:    rec.SuccessRuns,
			FailureRuns:    rec.FailureRuns,
			NeutralRuns:    rec.NeutralRuns,
			SuccessRate:    rec.SuccessRate,
			FailureRate:    rec.FailureRate,
			Recommendation: string(fb.Recommendation),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"context_memory": entries,
	})
}

// AgentProviderContextMemory returns provider-context action memory with feedback signals.
// GET /api/v1/agent/action-memory/provider-context
// Optional query params: provider_name, action_type
func (h *Handlers) AgentProviderContextMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.actionMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "action memory store not configured")
		return
	}

	providerName := r.URL.Query().Get("provider_name")
	actionType := r.URL.Query().Get("action_type")

	records, err := h.actionMemoryStore.ListProviderContextRecords(r.Context(), providerName, actionType)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	type providerContextEntry struct {
		ActionType     string  `json:"action_type"`
		GoalType       string  `json:"goal_type"`
		JobType        string  `json:"job_type"`
		ProviderName   string  `json:"provider_name"`
		ModelRole      string  `json:"model_role"`
		FailureBucket  string  `json:"failure_bucket"`
		BacklogBucket  string  `json:"backlog_bucket"`
		TotalRuns      int     `json:"total_runs"`
		SuccessRuns    int     `json:"success_runs"`
		FailureRuns    int     `json:"failure_runs"`
		NeutralRuns    int     `json:"neutral_runs"`
		SuccessRate    float64 `json:"success_rate"`
		FailureRate    float64 `json:"failure_rate"`
		Recommendation string  `json:"recommendation"`
	}

	entries := make([]providerContextEntry, 0, len(records))
	for _, rec := range records {
		amr := actionmemory.ActionMemoryRecord{
			ActionType:  rec.ActionType,
			TotalRuns:   rec.TotalRuns,
			SuccessRuns: rec.SuccessRuns,
			FailureRuns: rec.FailureRuns,
			NeutralRuns: rec.NeutralRuns,
			SuccessRate: rec.SuccessRate,
			FailureRate: rec.FailureRate,
		}
		fb := actionmemory.GenerateFeedback(&amr)
		entries = append(entries, providerContextEntry{
			ActionType:     rec.ActionType,
			GoalType:       rec.GoalType,
			JobType:        rec.JobType,
			ProviderName:   rec.ProviderName,
			ModelRole:      rec.ModelRole,
			FailureBucket:  rec.FailureBucket,
			BacklogBucket:  rec.BacklogBucket,
			TotalRuns:      rec.TotalRuns,
			SuccessRuns:    rec.SuccessRuns,
			FailureRuns:    rec.FailureRuns,
			NeutralRuns:    rec.NeutralRuns,
			SuccessRate:    rec.SuccessRate,
			FailureRate:    rec.FailureRate,
			Recommendation: string(fb.Recommendation),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider_context_memory": entries,
	})
}

// AgentWeightedMemory returns weighted feedback analysis across all memory layers.
// GET /api/v1/agent/action-memory/weighted
// Optional query params: action_type, provider_name
func (h *Handlers) AgentWeightedMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.actionMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "action memory store not configured")
		return
	}

	filterAction := r.URL.Query().Get("action_type")
	filterProvider := r.URL.Query().Get("provider_name")

	now := time.Now().UTC()

	// Load all three layers.
	globalRecords, err := h.actionMemoryStore.List(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query global memory failed")
		return
	}
	contextRecords, err := h.actionMemoryStore.ListContextRecords(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query context memory failed")
		return
	}
	providerRecords, err := h.actionMemoryStore.ListProviderContextRecords(r.Context(), filterProvider, filterAction)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query provider-context memory failed")
		return
	}

	// Build global feedback map.
	globalFeedback := make(map[string]actionmemory.ActionFeedback, len(globalRecords))
	for _, rec := range globalRecords {
		fb := actionmemory.GenerateFeedback(&rec)
		globalFeedback[rec.ActionType] = fb
	}

	// Collect unique action types to analyze.
	actionTypes := make(map[string]bool)
	for _, r := range globalRecords {
		actionTypes[r.ActionType] = true
	}
	for _, r := range contextRecords {
		actionTypes[r.ActionType] = true
	}
	for _, r := range providerRecords {
		actionTypes[r.ActionType] = true
	}

	type weightedEntry struct {
		ActionType    string                          `json:"action_type"`
		Best          *actionmemory.WeightedFeedback  `json:"best"`
		AllCandidates []actionmemory.WeightedFeedback `json:"all_candidates"`
	}

	var results []weightedEntry
	for at := range actionTypes {
		if filterAction != "" && at != filterAction {
			continue
		}

		candidates := actionmemory.GatherWeightedCandidates(
			providerRecords, contextRecords, globalFeedback,
			at, "", // no specific goal type
			filterProvider, "",
			"", "",
			now,
		)
		best, all := actionmemory.ResolveWeightedFeedback(candidates)
		results = append(results, weightedEntry{
			ActionType:    at,
			Best:          best,
			AllCandidates: all,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"weighted_memory": results,
		"timestamp":       now,
	})
}

// AgentHierarchicalMemory returns hierarchical feedback analysis across all memory layers.
// GET /api/v1/agent/action-memory/hierarchical
// Optional query params: action_type, provider_name
func (h *Handlers) AgentHierarchicalMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.actionMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "action memory store not configured")
		return
	}

	filterAction := r.URL.Query().Get("action_type")
	filterProvider := r.URL.Query().Get("provider_name")

	now := time.Now().UTC()

	globalRecords, err := h.actionMemoryStore.List(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query global memory failed")
		return
	}
	contextRecords, err := h.actionMemoryStore.ListContextRecords(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query context memory failed")
		return
	}
	providerRecords, err := h.actionMemoryStore.ListProviderContextRecords(r.Context(), filterProvider, filterAction)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "query provider-context memory failed")
		return
	}

	globalFeedback := make(map[string]actionmemory.ActionFeedback, len(globalRecords))
	for _, rec := range globalRecords {
		fb := actionmemory.GenerateFeedback(&rec)
		globalFeedback[rec.ActionType] = fb
	}

	actionTypes := make(map[string]bool)
	for _, r := range globalRecords {
		actionTypes[r.ActionType] = true
	}
	for _, r := range contextRecords {
		actionTypes[r.ActionType] = true
	}
	for _, r := range providerRecords {
		actionTypes[r.ActionType] = true
	}

	type hierarchicalEntry struct {
		ActionType    string                               `json:"action_type"`
		Best          *actionmemory.HierarchicalCandidate  `json:"best"`
		AllCandidates []actionmemory.HierarchicalCandidate `json:"all_candidates"`
	}

	var results []hierarchicalEntry
	for at := range actionTypes {
		if filterAction != "" && at != filterAction {
			continue
		}

		candidates := actionmemory.GatherHierarchicalCandidates(
			providerRecords, contextRecords, globalFeedback,
			at, "",
			filterProvider, "",
			"", "",
			now,
		)
		best, all := actionmemory.ResolveHierarchicalFeedback(candidates)
		results = append(results, hierarchicalEntry{
			ActionType:    at,
			Best:          best,
			AllCandidates: all,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"hierarchical_memory": results,
		"timestamp":           now,
	})
}

// AgentPlanningDecisions returns the most recent adaptive planning decisions.
// GET /api/v1/agent/planning-decisions
func (h *Handlers) AgentPlanningDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.adaptivePlanner == nil {
		writeError(w, r, http.StatusServiceUnavailable, "adaptive planner not configured")
		return
	}

	decisions := h.adaptivePlanner.LastDecisions()

	type candidateEntry struct {
		ActionType   string   `json:"action_type"`
		Score        float64  `json:"score"`
		Confidence   float64  `json:"confidence"`
		Reasoning    []string `json:"reasoning"`
		Rejected     bool     `json:"rejected"`
		RejectReason string   `json:"reject_reason,omitempty"`
	}
	type decisionEntry struct {
		GoalID             string           `json:"goal_id"`
		GoalType           string           `json:"goal_type"`
		SelectedActionType string           `json:"selected_action_type"`
		Explanation        string           `json:"explanation"`
		PlannedAt          string           `json:"planned_at"`
		Candidates         []candidateEntry `json:"candidates"`
	}

	entries := make([]decisionEntry, 0, len(decisions))
	for _, d := range decisions {
		candidates := make([]candidateEntry, 0, len(d.Candidates))
		for _, c := range d.Candidates {
			candidates = append(candidates, candidateEntry{
				ActionType:   c.ActionType,
				Score:        c.Score,
				Confidence:   c.Confidence,
				Reasoning:    c.Reasoning,
				Rejected:     c.Rejected,
				RejectReason: c.RejectReason,
			})
		}
		entries = append(entries, decisionEntry{
			GoalID:             d.GoalID,
			GoalType:           d.GoalType,
			SelectedActionType: d.SelectedActionType,
			Explanation:        d.Explanation,
			PlannedAt:          d.PlannedAt.Format(time.RFC3339),
			Candidates:         candidates,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"decisions": entries,
	})
}

// SchedulerStart starts the autonomous scheduler.
func (h *Handlers) SchedulerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.scheduler == nil {
		writeError(w, r, http.StatusServiceUnavailable, "scheduler not configured")
		return
	}
	h.scheduler.Start()
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// SchedulerStop stops the autonomous scheduler.
func (h *Handlers) SchedulerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.scheduler == nil {
		writeError(w, r, http.StatusServiceUnavailable, "scheduler not configured")
		return
	}
	h.scheduler.Stop()
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// SchedulerStatus returns the current scheduler state.
func (h *Handlers) SchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.scheduler == nil {
		writeError(w, r, http.StatusServiceUnavailable, "scheduler not configured")
		return
	}
	status := h.scheduler.GetStatus(h.schedulerEnabled)
	writeJSON(w, http.StatusOK, status)
}

// TriggerReflection triggers a reflection cycle (POST /api/v1/agent/reflect).
func (h *Handlers) TriggerReflection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.reflectionEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "reflection engine not configured")
		return
	}

	report, err := h.reflectionEngine.Reflect(r.Context())
	if err != nil {
		h.logger.Error("reflection_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "reflection failed")
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// ListReflections lists recent reflection findings (GET /api/v1/agent/reflections).
func (h *Handlers) ListReflections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.reflectionStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "reflection store not configured")
		return
	}

	pg := parsePagination(r)
	findings, err := h.reflectionStore.ListRecent(r.Context(), pg.PerPage)
	if err != nil {
		h.logger.Error("list_reflections_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"findings": findings,
	})
}

// ListJournalDecisions lists durable planning decisions (GET /api/v1/agent/journal).
func (h *Handlers) ListJournalDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.decisionJournal == nil {
		writeError(w, r, http.StatusServiceUnavailable, "decision journal not configured")
		return
	}

	pg := parsePagination(r)
	decisions, err := h.decisionJournal.ListRecent(r.Context(), pg.PerPage)
	if err != nil {
		h.logger.Error("list_journal_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"decisions": decisions,
	})
}

// StabilityStatus returns the current stability state (GET /api/v1/agent/stability).
func (h *Handlers) StabilityStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.stabilityEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "stability engine not configured")
		return
	}

	st, err := h.stabilityEngine.GetState(r.Context())
	if err != nil {
		h.logger.Error("stability_status_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, st)
}

// StabilityReset resets stability to normal mode (POST /api/v1/agent/stability/reset).
func (h *Handlers) StabilityReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.stabilityEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "stability engine not configured")
		return
	}

	st, err := h.stabilityEngine.Reset(r.Context())
	if err != nil {
		h.logger.Error("stability_reset_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "reset failed")
		return
	}

	writeJSON(w, http.StatusOK, st)
}

// StabilityEvaluate forces one stability evaluation pass (POST /api/v1/agent/stability/evaluate).
func (h *Handlers) StabilityEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.stabilityEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "stability engine not configured")
		return
	}

	st, result, err := h.stabilityEngine.Evaluate(r.Context())
	if err != nil {
		h.logger.Error("stability_evaluate_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "evaluate failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"state":     st,
		"detection": result,
	})
}

// PolicyState returns the current active policy values (GET /api/v1/agent/policy).
func (h *Handlers) PolicyState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.policyEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "policy engine not configured")
		return
	}

	st, err := h.policyEngine.GetState(r.Context())
	if err != nil {
		h.logger.Error("policy_state_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, st)
}

// PolicyChanges returns the policy change history (GET /api/v1/agent/policy/changes).
func (h *Handlers) PolicyChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.policyEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "policy engine not configured")
		return
	}

	pg := parsePagination(r)
	changes, err := h.policyEngine.ListChanges(r.Context(), pg.PerPage)
	if err != nil {
		h.logger.Error("policy_changes_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"changes": changes,
	})
}

// PolicyEvaluate triggers evaluation of past policy changes (POST /api/v1/agent/policy/evaluate).
func (h *Handlers) PolicyEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.policyEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "policy engine not configured")
		return
	}

	result, err := h.policyEngine.EvaluateChanges(r.Context())
	if err != nil {
		h.logger.Error("policy_evaluate_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "evaluate failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// --- Causal Reasoning Handlers ---

// CausalAttributions returns recent causal attributions (GET /api/v1/agent/causal).
func (h *Handlers) CausalAttributions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.causalEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "causal engine not configured")
		return
	}

	pg := parsePagination(r)
	attributions, err := h.causalEngine.ListRecent(r.Context(), pg.PerPage)
	if err != nil {
		h.logger.Error("causal_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"attributions": attributions,
	})
}

// CausalEvaluate triggers a causal analysis pass (POST /api/v1/agent/causal/evaluate).
func (h *Handlers) CausalEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.causalEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "causal engine not configured")
		return
	}

	result, err := h.causalEngine.RunAnalysis(r.Context())
	if err != nil {
		h.logger.Error("causal_evaluate_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "analysis failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// CausalBySubject returns attributions for a specific subject (GET /api/v1/agent/causal/{subject_id}).
func (h *Handlers) CausalBySubject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.causalEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "causal engine not configured")
		return
	}

	subjectID, ok := parseIDFromPath(r, "/api/v1/agent/causal/")
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid subject_id")
		return
	}

	attributions, err := h.causalEngine.ListBySubject(r.Context(), subjectID)
	if err != nil {
		h.logger.Error("causal_by_subject_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"attributions": attributions,
	})
}

// --- Agent Exploration ---

// ExplorationStatus returns the current exploration budget and last decision.
func (h *Handlers) ExplorationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.explorationEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "exploration engine not configured")
		return
	}

	result := map[string]any{
		"enabled": true,
	}

	lastDecision := h.explorationEngine.LastDecision()
	if lastDecision != nil {
		result["last_decision"] = lastDecision
	}

	writeJSON(w, http.StatusOK, result)
}

// ExplorationHistory returns recent exploration events from the audit trail.
func (h *Handlers) ExplorationHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	p := parsePagination(r)

	rows, err := h.db.Query(r.Context(), `
		SELECT id, entity_type, entity_id, event_type, actor_type, actor_id,
		       payload, occurred_at
		FROM audit_events
		WHERE entity_type = 'exploration'
		ORDER BY occurred_at DESC
		LIMIT $1 OFFSET $2
	`, p.PerPage, p.Offset)
	if err != nil {
		h.logger.Error("exploration_history_query_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var events []map[string]any
	for rows.Next() {
		var (
			id, entityID                              uuid.UUID
			entityType, eventType, actorType, actorID string
			payload                                   json.RawMessage
			occurredAt                                time.Time
		)
		if err := rows.Scan(&id, &entityType, &entityID, &eventType,
			&actorType, &actorID, &payload, &occurredAt); err != nil {
			continue
		}
		events = append(events, map[string]any{
			"id":          id,
			"entity_type": entityType,
			"entity_id":   entityID,
			"event_type":  eventType,
			"actor_type":  actorType,
			"actor_id":    actorID,
			"payload":     json.RawMessage(payload),
			"occurred_at": occurredAt,
		})
	}
	if events == nil {
		events = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"page":   p.Page,
	})
}

// --- Strategy handlers ---

// StrategyStatus returns the current strategy engine state and last decision.
func (h *Handlers) StrategyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.strategyEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "strategy engine not configured")
		return
	}

	result := map[string]any{
		"enabled": true,
	}

	lastDecision := h.strategyEngine.LastDecision()
	if lastDecision != nil {
		result["last_decision"] = lastDecision
	}

	writeJSON(w, http.StatusOK, result)
}

// StrategyHistory returns recent strategy events from the audit trail.
func (h *Handlers) StrategyHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	p := parsePagination(r)

	rows, err := h.db.Query(r.Context(), `
		SELECT id, entity_type, entity_id, event_type, actor_type, actor_id,
		       payload, occurred_at
		FROM audit_events
		WHERE entity_type = 'strategy'
		ORDER BY occurred_at DESC
		LIMIT $1 OFFSET $2
	`, p.PerPage, p.Offset)
	if err != nil {
		h.logger.Error("strategy_history_query_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var events []map[string]any
	for rows.Next() {
		var (
			id, entityID                              uuid.UUID
			entityType, eventType, actorType, actorID string
			payload                                   json.RawMessage
			occurredAt                                time.Time
		)
		if err := rows.Scan(&id, &entityType, &entityID, &eventType,
			&actorType, &actorID, &payload, &occurredAt); err != nil {
			continue
		}
		events = append(events, map[string]any{
			"id":          id,
			"entity_type": entityType,
			"entity_id":   entityID,
			"event_type":  eventType,
			"actor_type":  actorType,
			"actor_id":    actorID,
			"payload":     json.RawMessage(payload),
			"occurred_at": occurredAt,
		})
	}
	if events == nil {
		events = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"page":   p.Page,
	})
}

// StrategyPlans returns persisted strategy plans with optional filtering.
func (h *Handlers) StrategyPlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.strategyEngine == nil {
		writeError(w, r, http.StatusServiceUnavailable, "strategy engine not configured")
		return
	}

	store := h.strategyEngine.GetStore()
	if store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "strategy store not configured")
		return
	}

	p := parsePagination(r)

	// Filter by goal_id if provided.
	goalID := r.URL.Query().Get("goal_id")
	selectedOnly := r.URL.Query().Get("selected") == "true"

	var plans []strategy.StrategyPlan
	var err error

	switch {
	case goalID != "":
		plans, err = store.ListByGoal(r.Context(), goalID)
	case selectedOnly:
		plans, err = store.ListSelected(r.Context(), p.PerPage, p.Offset)
	default:
		plans, err = store.ListRecent(r.Context(), p.PerPage, p.Offset)
	}

	if err != nil {
		h.logger.Error("strategy_plans_query_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	if plans == nil {
		plans = []strategy.StrategyPlan{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"plans": plans,
		"page":  p.Page,
	})
}

// StrategyMemoryList returns strategy memory records with recommendations.
func (h *Handlers) StrategyMemoryList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.strategyLearning == nil {
		writeError(w, r, http.StatusServiceUnavailable, "strategy learning not configured")
		return
	}

	records, err := h.strategyLearning.ListMemory(r.Context())
	if err != nil {
		h.logger.Error("strategy_memory_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	// Attach feedback recommendation to each record.
	type recordWithFeedback struct {
		strategylearning.StrategyMemoryRecord
		Recommendation string `json:"recommendation"`
	}
	result := make([]recordWithFeedback, 0, len(records))
	for _, rec := range records {
		fb := strategylearning.GenerateFeedback(&rec)
		result = append(result, recordWithFeedback{
			StrategyMemoryRecord: rec,
			Recommendation:       string(fb.Recommendation),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": result,
	})
}

// StrategyOutcomesList returns recent strategy outcomes.
func (h *Handlers) StrategyOutcomesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.strategyLearning == nil {
		writeError(w, r, http.StatusServiceUnavailable, "strategy learning not configured")
		return
	}

	p := parsePagination(r)

	outcomes, err := h.strategyLearning.ListOutcomes(r.Context(), p.PerPage, p.Offset)
	if err != nil {
		h.logger.Error("strategy_outcomes_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	if outcomes == nil {
		outcomes = []strategylearning.StrategyOutcome{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"outcomes": outcomes,
		"page":     p.Page,
	})
}

// --- Helpers ---

func parseIDFromPath(r *http.Request, prefix string) (uuid.UUID, bool) {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	path = strings.TrimSuffix(path, "/")
	id, err := uuid.Parse(path)
	return id, err == nil
}

// --- Decision Graph handlers ---

// DecisionGraphStatus returns the last decision graph path selection.
func (h *Handlers) DecisionGraphStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if h.decisionGraph == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "not_configured",
			"message": "decision graph engine not wired",
		})
		return
	}

	selection := h.decisionGraph.LastSelection()
	if selection == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "no_evaluation",
			"message": "no decision graph evaluation yet",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"selection": selection,
	})
}

// --- Path Learning handlers (Iteration 21) ---

// PathMemoryList returns path memory records with recommendations.
func (h *Handlers) PathMemoryList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.pathMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "path learning not configured")
		return
	}

	goalType := r.URL.Query().Get("goal_type")
	pathSig := r.URL.Query().Get("path_signature")

	var records []pathlearning.PathMemoryRecord
	var err error

	if goalType != "" {
		records, err = h.pathMemoryStore.ListPathMemoryByGoalType(r.Context(), goalType)
	} else {
		records, err = h.pathMemoryStore.ListPathMemory(r.Context())
	}
	if err != nil {
		h.logger.Error("path_memory_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	// Filter by path_signature if provided.
	if pathSig != "" {
		var filtered []pathlearning.PathMemoryRecord
		for _, rec := range records {
			if rec.PathSignature == pathSig {
				filtered = append(filtered, rec)
			}
		}
		records = filtered
	}

	// Attach feedback recommendation to each record.
	type recordWithFeedback struct {
		pathlearning.PathMemoryRecord
		Recommendation string `json:"recommendation"`
	}
	result := make([]recordWithFeedback, 0, len(records))
	for _, rec := range records {
		fb := pathlearning.GeneratePathFeedback(&rec)
		result = append(result, recordWithFeedback{
			PathMemoryRecord: rec,
			Recommendation:   string(fb.Recommendation),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": result,
	})
}

// TransitionMemoryList returns transition memory records with recommendations.
func (h *Handlers) TransitionMemoryList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.transitionStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "path learning not configured")
		return
	}

	goalType := r.URL.Query().Get("goal_type")
	tKey := r.URL.Query().Get("transition_key")

	var records []pathlearning.TransitionMemoryRecord
	var err error

	if goalType != "" {
		records, err = h.transitionStore.ListTransitionMemoryByGoalType(r.Context(), goalType)
	} else {
		records, err = h.transitionStore.ListTransitionMemory(r.Context())
	}
	if err != nil {
		h.logger.Error("transition_memory_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	// Filter by transition_key if provided.
	if tKey != "" {
		var filtered []pathlearning.TransitionMemoryRecord
		for _, rec := range records {
			if rec.TransitionKey == tKey {
				filtered = append(filtered, rec)
			}
		}
		records = filtered
	}

	// Attach feedback recommendation to each record.
	type recordWithFeedback struct {
		pathlearning.TransitionMemoryRecord
		Recommendation string `json:"recommendation"`
	}
	result := make([]recordWithFeedback, 0, len(records))
	for _, rec := range records {
		fb := pathlearning.GenerateTransitionFeedback(&rec)
		result = append(result, recordWithFeedback{
			TransitionMemoryRecord: rec,
			Recommendation:         string(fb.Recommendation),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": result,
	})
}

// PathOutcomesList returns recent evaluated path outcomes.
func (h *Handlers) PathOutcomesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.pathMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "path learning not configured")
		return
	}

	p := parsePagination(r)
	goalType := r.URL.Query().Get("goal_type")

	var outcomes []pathlearning.PathOutcome
	var err error

	if goalType != "" {
		outcomes, err = h.pathMemoryStore.ListPathOutcomesByGoalType(r.Context(), goalType, p.PerPage, p.Offset)
	} else {
		outcomes, err = h.pathMemoryStore.ListPathOutcomes(r.Context(), p.PerPage, p.Offset)
	}
	if err != nil {
		h.logger.Error("path_outcomes_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	if outcomes == nil {
		outcomes = []pathlearning.PathOutcome{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"outcomes": outcomes,
		"page":     p.Page,
	})
}

// --- Path Comparison handlers (Iteration 22) ---

// PathSnapshotsList returns decision snapshots.
func (h *Handlers) PathSnapshotsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.compSnapshotStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "path comparison not configured")
		return
	}

	p := parsePagination(r)
	goalType := r.URL.Query().Get("goal_type")

	var snapshots []pathcomparison.DecisionSnapshot
	var err error

	if goalType != "" {
		snapshots, err = h.compSnapshotStore.ListSnapshotsByGoalType(r.Context(), goalType, p.PerPage, p.Offset)
	} else {
		snapshots, err = h.compSnapshotStore.ListSnapshots(r.Context(), p.PerPage, p.Offset)
	}
	if err != nil {
		h.logger.Error("path_snapshots_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	if snapshots == nil {
		snapshots = []pathcomparison.DecisionSnapshot{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"snapshots": snapshots,
		"page":      p.Page,
	})
}

// PathComparativeList returns comparative outcomes.
func (h *Handlers) PathComparativeList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.compOutcomeStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "path comparison not configured")
		return
	}

	p := parsePagination(r)
	goalType := r.URL.Query().Get("goal_type")

	var outcomes []pathcomparison.ComparativeOutcome
	var err error

	if goalType != "" {
		outcomes, err = h.compOutcomeStore.ListOutcomesByGoalType(r.Context(), goalType, p.PerPage, p.Offset)
	} else {
		outcomes, err = h.compOutcomeStore.ListOutcomes(r.Context(), p.PerPage, p.Offset)
	}
	if err != nil {
		h.logger.Error("path_comparative_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	if outcomes == nil {
		outcomes = []pathcomparison.ComparativeOutcome{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"outcomes": outcomes,
		"page":     p.Page,
	})
}

// PathComparativeMemoryList returns comparative memory records with recommendations.
func (h *Handlers) PathComparativeMemoryList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.compMemoryStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "path comparison not configured")
		return
	}

	goalType := r.URL.Query().Get("goal_type")

	var records []pathcomparison.ComparativeMemoryRecord
	var err error

	if goalType != "" {
		records, err = h.compMemoryStore.ListMemoryByGoalType(r.Context(), goalType)
	} else {
		records, err = h.compMemoryStore.ListMemory(r.Context())
	}
	if err != nil {
		h.logger.Error("path_comparative_memory_list_failed", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, "query failed")
		return
	}

	type recordWithFeedback struct {
		pathcomparison.ComparativeMemoryRecord
		Recommendation string `json:"recommendation"`
	}
	result := make([]recordWithFeedback, 0, len(records))
	for _, rec := range records {
		fb := pathcomparison.GenerateComparativeFeedback(&rec)
		result = append(result, recordWithFeedback{
			ComparativeMemoryRecord: rec,
			Recommendation:          string(fb.Recommendation),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": result,
	})
}
