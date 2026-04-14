package taskorchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- Store interfaces for testability ---

// TaskStoreInterface abstracts task persistence.
type TaskStoreInterface interface {
	Insert(ctx context.Context, t OrchestratedTask) error
	Get(ctx context.Context, id string) (OrchestratedTask, error)
	Update(ctx context.Context, t OrchestratedTask) error
	List(ctx context.Context, limit int) ([]OrchestratedTask, error)
	ListByStatus(ctx context.Context, status TaskStatus, limit int) ([]OrchestratedTask, error)
	CountByStatus(ctx context.Context, status TaskStatus) (int, error)
	FindByActuationDecision(ctx context.Context, decisionID string) (string, error)
	SetActuationDecisionID(ctx context.Context, taskID, decisionID string) error
	SetExecutionTaskID(ctx context.Context, taskID, execTaskID string) error
	SetOutcome(ctx context.Context, taskID, outcomeType, lastError string, attemptCount int) error
}

// QueueStoreInterface abstracts queue persistence.
type QueueStoreInterface interface {
	Upsert(ctx context.Context, e TaskQueueEntry) error
	Remove(ctx context.Context, taskID string) error
	List(ctx context.Context, limit int) ([]TaskQueueEntry, error)
	Count(ctx context.Context) (int, error)
}

// --- PostgreSQL TaskStore ---

// TaskStore handles persistence of orchestrated tasks.
type TaskStore struct {
	pool *pgxpool.Pool
}

// NewTaskStore creates a new TaskStore.
func NewTaskStore(pool *pgxpool.Pool) *TaskStore {
	return &TaskStore{pool: pool}
}

// Insert persists a new orchestrated task.
func (s *TaskStore) Insert(ctx context.Context, t OrchestratedTask) error {
	const q = `
		INSERT INTO agent_orchestrated_tasks
			(id, source, goal, priority_score, status, urgency, expected_value, risk_level, strategy_type,
			 actuation_decision_id, execution_task_id, outcome_type, last_error, attempt_count, completed_at,
			 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`
	_, err := s.pool.Exec(ctx, q,
		t.ID, t.Source, t.Goal, t.PriorityScore, string(t.Status),
		t.Urgency, t.ExpectedValue, t.RiskLevel, t.StrategyType,
		nilIfEmpty(t.ActuationDecisionID), nilIfEmpty(t.ExecutionTaskID),
		t.OutcomeType, t.LastError, t.AttemptCount, t.CompletedAt,
		t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert orchestrated task: %w", err)
	}
	return nil
}

// Get retrieves an orchestrated task by ID.
func (s *TaskStore) Get(ctx context.Context, id string) (OrchestratedTask, error) {
	const q = `
		SELECT id, source, goal, priority_score, status, urgency, expected_value,
		       risk_level, strategy_type,
		       COALESCE(actuation_decision_id, ''), COALESCE(execution_task_id, ''),
		       COALESCE(outcome_type, ''), COALESCE(last_error, ''), COALESCE(attempt_count, 0),
		       completed_at, created_at, updated_at
		FROM agent_orchestrated_tasks WHERE id = $1`
	var t OrchestratedTask
	var statusStr string
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&t.ID, &t.Source, &t.Goal, &t.PriorityScore, &statusStr,
		&t.Urgency, &t.ExpectedValue, &t.RiskLevel, &t.StrategyType,
		&t.ActuationDecisionID, &t.ExecutionTaskID,
		&t.OutcomeType, &t.LastError, &t.AttemptCount,
		&t.CompletedAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return OrchestratedTask{}, ErrTaskNotFound
	}
	if err != nil {
		return OrchestratedTask{}, fmt.Errorf("get orchestrated task: %w", err)
	}
	t.Status = TaskStatus(statusStr)
	return t, nil
}

// Update persists the full task state.
func (s *TaskStore) Update(ctx context.Context, t OrchestratedTask) error {
	const q = `
		UPDATE agent_orchestrated_tasks
		SET priority_score = $1, status = $2, urgency = $3,
		    expected_value = $4, risk_level = $5, strategy_type = $6,
		    actuation_decision_id = $7, execution_task_id = $8,
		    outcome_type = $9, last_error = $10, attempt_count = $11,
		    completed_at = $12, updated_at = $13
		WHERE id = $14`
	_, err := s.pool.Exec(ctx, q,
		t.PriorityScore, string(t.Status), t.Urgency,
		t.ExpectedValue, t.RiskLevel, t.StrategyType,
		nilIfEmpty(t.ActuationDecisionID), nilIfEmpty(t.ExecutionTaskID),
		t.OutcomeType, t.LastError, t.AttemptCount,
		t.CompletedAt, t.UpdatedAt,
		t.ID,
	)
	if err != nil {
		return fmt.Errorf("update orchestrated task: %w", err)
	}
	return nil
}

// List returns orchestrated tasks ordered by creation time descending.
func (s *TaskStore) List(ctx context.Context, limit int) ([]OrchestratedTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT id, source, goal, priority_score, status, urgency, expected_value,
		       risk_level, strategy_type,
		       COALESCE(actuation_decision_id, ''), COALESCE(execution_task_id, ''),
		       COALESCE(outcome_type, ''), COALESCE(last_error, ''), COALESCE(attempt_count, 0),
		       completed_at, created_at, updated_at
		FROM agent_orchestrated_tasks ORDER BY created_at DESC LIMIT $1`
	return s.scanTasks(ctx, q, limit)
}

// ListByStatus returns tasks with the given status.
func (s *TaskStore) ListByStatus(ctx context.Context, status TaskStatus, limit int) ([]OrchestratedTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT id, source, goal, priority_score, status, urgency, expected_value,
		       risk_level, strategy_type,
		       COALESCE(actuation_decision_id, ''), COALESCE(execution_task_id, ''),
		       COALESCE(outcome_type, ''), COALESCE(last_error, ''), COALESCE(attempt_count, 0),
		       completed_at, created_at, updated_at
		FROM agent_orchestrated_tasks WHERE status = $1 ORDER BY created_at DESC LIMIT $2`
	return s.scanTasksWithStatus(ctx, q, string(status), limit)
}

// CountByStatus returns the count of tasks with a given status.
func (s *TaskStore) CountByStatus(ctx context.Context, status TaskStatus) (int, error) {
	const q = `SELECT COUNT(*) FROM agent_orchestrated_tasks WHERE status = $1`
	var count int
	err := s.pool.QueryRow(ctx, q, string(status)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count orchestrated tasks: %w", err)
	}
	return count, nil
}

// FindByActuationDecision returns the task ID linked to the given actuation decision.
// Returns "" if no task is linked.
func (s *TaskStore) FindByActuationDecision(ctx context.Context, decisionID string) (string, error) {
	const q = `SELECT id FROM agent_orchestrated_tasks WHERE actuation_decision_id = $1 LIMIT 1`
	var id string
	err := s.pool.QueryRow(ctx, q, decisionID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find by actuation decision: %w", err)
	}
	return id, nil
}

// SetActuationDecisionID links an actuation decision to a task.
func (s *TaskStore) SetActuationDecisionID(ctx context.Context, taskID, decisionID string) error {
	const q = `UPDATE agent_orchestrated_tasks SET actuation_decision_id = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.pool.Exec(ctx, q, decisionID, taskID)
	if err != nil {
		return fmt.Errorf("set actuation decision id: %w", err)
	}
	return nil
}

// SetExecutionTaskID links an execution task to an orchestrated task.
func (s *TaskStore) SetExecutionTaskID(ctx context.Context, taskID, execTaskID string) error {
	const q = `UPDATE agent_orchestrated_tasks SET execution_task_id = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.pool.Exec(ctx, q, execTaskID, taskID)
	if err != nil {
		return fmt.Errorf("set execution task id: %w", err)
	}
	return nil
}

// SetOutcome records the execution outcome on a task.
func (s *TaskStore) SetOutcome(ctx context.Context, taskID, outcomeType, lastError string, attemptCount int) error {
	const q = `UPDATE agent_orchestrated_tasks SET outcome_type = $1, last_error = $2, attempt_count = $3, completed_at = NOW(), updated_at = NOW() WHERE id = $4`
	_, err := s.pool.Exec(ctx, q, outcomeType, lastError, attemptCount, taskID)
	if err != nil {
		return fmt.Errorf("set outcome: %w", err)
	}
	return nil
}

// nilIfEmpty returns nil if the string is empty, otherwise returns the string pointer.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *TaskStore) scanTasks(ctx context.Context, q string, limit int) ([]OrchestratedTask, error) {
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list orchestrated tasks: %w", err)
	}
	defer rows.Close()
	return s.collectRows(rows)
}

func (s *TaskStore) scanTasksWithStatus(ctx context.Context, q, status string, limit int) ([]OrchestratedTask, error) {
	rows, err := s.pool.Query(ctx, q, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list orchestrated tasks by status: %w", err)
	}
	defer rows.Close()
	return s.collectRows(rows)
}

func (s *TaskStore) collectRows(rows pgx.Rows) ([]OrchestratedTask, error) {
	var tasks []OrchestratedTask
	for rows.Next() {
		var t OrchestratedTask
		var statusStr string
		if err := rows.Scan(
			&t.ID, &t.Source, &t.Goal, &t.PriorityScore, &statusStr,
			&t.Urgency, &t.ExpectedValue, &t.RiskLevel, &t.StrategyType,
			&t.ActuationDecisionID, &t.ExecutionTaskID,
			&t.OutcomeType, &t.LastError, &t.AttemptCount,
			&t.CompletedAt, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan orchestrated task: %w", err)
		}
		t.Status = TaskStatus(statusStr)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// --- PostgreSQL QueueStore ---

// QueueStore handles persistence of the priority queue.
type QueueStore struct {
	pool *pgxpool.Pool
}

// NewQueueStore creates a new QueueStore.
func NewQueueStore(pool *pgxpool.Pool) *QueueStore {
	return &QueueStore{pool: pool}
}

// Upsert inserts or updates a queue entry.
func (s *QueueStore) Upsert(ctx context.Context, e TaskQueueEntry) error {
	const q = `
		INSERT INTO agent_task_queue (task_id, priority_score, inserted_at, last_updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (task_id)
		DO UPDATE SET priority_score = EXCLUDED.priority_score, last_updated_at = EXCLUDED.last_updated_at`
	_, err := s.pool.Exec(ctx, q, e.TaskID, e.PriorityScore, e.InsertedAt, e.LastUpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert queue entry: %w", err)
	}
	return nil
}

// Remove deletes a queue entry by task ID.
func (s *QueueStore) Remove(ctx context.Context, taskID string) error {
	const q = `DELETE FROM agent_task_queue WHERE task_id = $1`
	_, err := s.pool.Exec(ctx, q, taskID)
	if err != nil {
		return fmt.Errorf("remove queue entry: %w", err)
	}
	return nil
}

// List returns queue entries ordered by priority descending, then inserted_at ascending (oldest first for tie-break).
func (s *QueueStore) List(ctx context.Context, limit int) ([]TaskQueueEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = MaxTasksInQueue
	}
	const q = `
		SELECT task_id, priority_score, inserted_at, last_updated_at
		FROM agent_task_queue
		ORDER BY priority_score DESC, inserted_at ASC
		LIMIT $1`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list queue entries: %w", err)
	}
	defer rows.Close()

	var entries []TaskQueueEntry
	for rows.Next() {
		var e TaskQueueEntry
		if err := rows.Scan(&e.TaskID, &e.PriorityScore, &e.InsertedAt, &e.LastUpdatedAt); err != nil {
			return nil, fmt.Errorf("scan queue entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Count returns the number of entries in the queue.
func (s *QueueStore) Count(ctx context.Context) (int, error) {
	const q = `SELECT COUNT(*) FROM agent_task_queue`
	var count int
	err := s.pool.QueryRow(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count queue entries: %w", err)
	}
	return count, nil
}

// --- In-memory stores for testing ---

// InMemoryTaskStore is an in-memory implementation of TaskStoreInterface.
type InMemoryTaskStore struct {
	tasks map[string]OrchestratedTask
	order []string
}

// NewInMemoryTaskStore creates a new in-memory task store.
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{
		tasks: make(map[string]OrchestratedTask),
	}
}

func (s *InMemoryTaskStore) Insert(_ context.Context, t OrchestratedTask) error {
	s.tasks[t.ID] = t
	s.order = append(s.order, t.ID)
	return nil
}

func (s *InMemoryTaskStore) Get(_ context.Context, id string) (OrchestratedTask, error) {
	t, ok := s.tasks[id]
	if !ok {
		return OrchestratedTask{}, ErrTaskNotFound
	}
	return t, nil
}

func (s *InMemoryTaskStore) Update(_ context.Context, t OrchestratedTask) error {
	if _, ok := s.tasks[t.ID]; !ok {
		return ErrTaskNotFound
	}
	s.tasks[t.ID] = t
	return nil
}

func (s *InMemoryTaskStore) List(_ context.Context, limit int) ([]OrchestratedTask, error) {
	if limit <= 0 || limit > len(s.order) {
		limit = len(s.order)
	}
	var result []OrchestratedTask
	// Return in reverse insertion order (newest first).
	for i := len(s.order) - 1; i >= 0 && len(result) < limit; i-- {
		if t, ok := s.tasks[s.order[i]]; ok {
			result = append(result, t)
		}
	}
	return result, nil
}

func (s *InMemoryTaskStore) ListByStatus(_ context.Context, status TaskStatus, limit int) ([]OrchestratedTask, error) {
	var result []OrchestratedTask
	for i := len(s.order) - 1; i >= 0 && len(result) < limit; i-- {
		if t, ok := s.tasks[s.order[i]]; ok && t.Status == status {
			result = append(result, t)
		}
	}
	return result, nil
}

func (s *InMemoryTaskStore) CountByStatus(_ context.Context, status TaskStatus) (int, error) {
	count := 0
	for _, t := range s.tasks {
		if t.Status == status {
			count++
		}
	}
	return count, nil
}

func (s *InMemoryTaskStore) FindByActuationDecision(_ context.Context, decisionID string) (string, error) {
	for _, t := range s.tasks {
		if t.ActuationDecisionID == decisionID {
			return t.ID, nil
		}
	}
	return "", nil
}

func (s *InMemoryTaskStore) SetActuationDecisionID(_ context.Context, taskID, decisionID string) error {
	if t, ok := s.tasks[taskID]; ok {
		t.ActuationDecisionID = decisionID
		s.tasks[taskID] = t
		return nil
	}
	return ErrTaskNotFound
}

func (s *InMemoryTaskStore) SetExecutionTaskID(_ context.Context, taskID, execTaskID string) error {
	if t, ok := s.tasks[taskID]; ok {
		t.ExecutionTaskID = execTaskID
		s.tasks[taskID] = t
		return nil
	}
	return ErrTaskNotFound
}

func (s *InMemoryTaskStore) SetOutcome(_ context.Context, taskID, outcomeType, lastError string, attemptCount int) error {
	if t, ok := s.tasks[taskID]; ok {
		t.OutcomeType = outcomeType
		t.LastError = lastError
		t.AttemptCount = attemptCount
		now := nowUTC()
		t.CompletedAt = &now
		s.tasks[taskID] = t
		return nil
	}
	return ErrTaskNotFound
}

// InMemoryQueueStore is an in-memory implementation of QueueStoreInterface.
type InMemoryQueueStore struct {
	entries map[string]TaskQueueEntry
}

// NewInMemoryQueueStore creates a new in-memory queue store.
func NewInMemoryQueueStore() *InMemoryQueueStore {
	return &InMemoryQueueStore{
		entries: make(map[string]TaskQueueEntry),
	}
}

func (s *InMemoryQueueStore) Upsert(_ context.Context, e TaskQueueEntry) error {
	s.entries[e.TaskID] = e
	return nil
}

func (s *InMemoryQueueStore) Remove(_ context.Context, taskID string) error {
	delete(s.entries, taskID)
	return nil
}

func (s *InMemoryQueueStore) List(_ context.Context, limit int) ([]TaskQueueEntry, error) {
	var entries []TaskQueueEntry
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	// Sort by priority desc, then inserted_at asc.
	sortQueueEntries(entries)
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *InMemoryQueueStore) Count(_ context.Context) (int, error) {
	return len(s.entries), nil
}

// sortQueueEntries sorts entries by priority descending, then inserted_at ascending.
func sortQueueEntries(entries []TaskQueueEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			if entries[j].PriorityScore > entries[j-1].PriorityScore ||
				(entries[j].PriorityScore == entries[j-1].PriorityScore &&
					entries[j].InsertedAt.Before(entries[j-1].InsertedAt)) {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			}
		}
	}
}

// GetTaskDirect returns a task directly from the in-memory store (test helper).
func (s *InMemoryTaskStore) GetTaskDirect(id string) (OrchestratedTask, bool) {
	t, ok := s.tasks[id]
	return t, ok
}

// GetEntryDirect returns a queue entry directly (test helper).
func (s *InMemoryQueueStore) GetEntryDirect(taskID string) (TaskQueueEntry, bool) {
	e, ok := s.entries[taskID]
	return e, ok
}

// AllEntries returns all queue entries (test helper).
func (s *InMemoryQueueStore) AllEntries() map[string]TaskQueueEntry {
	return s.entries
}

// CountDirect returns the number of entries (test helper).
func (s *InMemoryQueueStore) CountDirect() int {
	return len(s.entries)
}

// SetTimestamps is a test helper for overriding time on stored tasks.
func (s *InMemoryTaskStore) SetTimestamps(id string, created, updated time.Time) {
	if t, ok := s.tasks[id]; ok {
		t.CreatedAt = created
		t.UpdatedAt = updated
		s.tasks[id] = t
	}
}
