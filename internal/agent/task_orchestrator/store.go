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
			(id, source, goal, priority_score, status, urgency, expected_value, risk_level, strategy_type, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := s.pool.Exec(ctx, q,
		t.ID, t.Source, t.Goal, t.PriorityScore, string(t.Status),
		t.Urgency, t.ExpectedValue, t.RiskLevel, t.StrategyType,
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
		       risk_level, strategy_type, created_at, updated_at
		FROM agent_orchestrated_tasks WHERE id = $1`
	var t OrchestratedTask
	var statusStr string
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&t.ID, &t.Source, &t.Goal, &t.PriorityScore, &statusStr,
		&t.Urgency, &t.ExpectedValue, &t.RiskLevel, &t.StrategyType,
		&t.CreatedAt, &t.UpdatedAt,
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
		    expected_value = $4, risk_level = $5, strategy_type = $6, updated_at = $7
		WHERE id = $8`
	_, err := s.pool.Exec(ctx, q,
		t.PriorityScore, string(t.Status), t.Urgency,
		t.ExpectedValue, t.RiskLevel, t.StrategyType, t.UpdatedAt,
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
		       risk_level, strategy_type, created_at, updated_at
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
		       risk_level, strategy_type, created_at, updated_at
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
			&t.CreatedAt, &t.UpdatedAt,
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
