package executionloop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskStore handles persistence of execution tasks.
type TaskStore struct {
	pool *pgxpool.Pool
}

// NewTaskStore creates a new TaskStore.
func NewTaskStore(pool *pgxpool.Pool) *TaskStore {
	return &TaskStore{pool: pool}
}

// Insert persists a new execution task.
func (s *TaskStore) Insert(ctx context.Context, t ExecutionTask) error {
	planJSON, err := json.Marshal(t.Plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	const q = `
		INSERT INTO agent_execution_tasks
			(id, opportunity_id, goal, status, plan, current_step, iteration_count, max_iterations, abort_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err = s.pool.Exec(ctx, q,
		t.ID, t.OpportunityID, t.Goal, string(t.Status),
		planJSON, t.CurrentStep, t.IterationCount, t.MaxIterations,
		t.AbortReason, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert execution task: %w", err)
	}
	return nil
}

// Get retrieves an execution task by ID.
func (s *TaskStore) Get(ctx context.Context, id string) (ExecutionTask, error) {
	const q = `
		SELECT id, opportunity_id, goal, status, plan, current_step,
		       iteration_count, max_iterations, abort_reason, created_at, updated_at
		FROM agent_execution_tasks WHERE id = $1`
	var t ExecutionTask
	var statusStr string
	var planJSON []byte
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&t.ID, &t.OpportunityID, &t.Goal, &statusStr,
		&planJSON, &t.CurrentStep, &t.IterationCount, &t.MaxIterations,
		&t.AbortReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return ExecutionTask{}, ErrTaskNotFound
	}
	if err != nil {
		return ExecutionTask{}, fmt.Errorf("get execution task: %w", err)
	}
	t.Status = TaskStatus(statusStr)
	if len(planJSON) > 0 {
		if err := json.Unmarshal(planJSON, &t.Plan); err != nil {
			return ExecutionTask{}, fmt.Errorf("unmarshal plan: %w", err)
		}
	}
	return t, nil
}

// Update persists the full task state (plan, status, step, iteration count).
func (s *TaskStore) Update(ctx context.Context, t ExecutionTask) error {
	planJSON, err := json.Marshal(t.Plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	const q = `
		UPDATE agent_execution_tasks
		SET status = $1, plan = $2, current_step = $3,
		    iteration_count = $4, abort_reason = $5, updated_at = $6
		WHERE id = $7`
	_, err = s.pool.Exec(ctx, q,
		string(t.Status), planJSON, t.CurrentStep,
		t.IterationCount, t.AbortReason, t.UpdatedAt,
		t.ID,
	)
	if err != nil {
		return fmt.Errorf("update execution task: %w", err)
	}
	return nil
}

// List returns execution tasks ordered by creation time descending.
func (s *TaskStore) List(ctx context.Context, limit int) ([]ExecutionTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT id, opportunity_id, goal, status, plan, current_step,
		       iteration_count, max_iterations, abort_reason, created_at, updated_at
		FROM agent_execution_tasks ORDER BY created_at DESC LIMIT $1`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list execution tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ExecutionTask
	for rows.Next() {
		var t ExecutionTask
		var statusStr string
		var planJSON []byte
		if err := rows.Scan(
			&t.ID, &t.OpportunityID, &t.Goal, &statusStr,
			&planJSON, &t.CurrentStep, &t.IterationCount, &t.MaxIterations,
			&t.AbortReason, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan execution task: %w", err)
		}
		t.Status = TaskStatus(statusStr)
		if len(planJSON) > 0 {
			_ = json.Unmarshal(planJSON, &t.Plan)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// ObservationStore handles persistence of execution observations.
type ObservationStore struct {
	pool *pgxpool.Pool
}

// NewObservationStore creates a new ObservationStore.
func NewObservationStore(pool *pgxpool.Pool) *ObservationStore {
	return &ObservationStore{pool: pool}
}

// Insert persists an observation.
func (s *ObservationStore) Insert(ctx context.Context, o ExecutionObservation) error {
	outputJSON := o.Output
	if outputJSON == nil {
		outputJSON = json.RawMessage(`null`)
	}
	const q = `
		INSERT INTO agent_execution_observations
			(step_id, task_id, success, output, error, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := s.pool.Exec(ctx, q,
		o.StepID, o.TaskID, o.Success, outputJSON, o.Error, o.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert execution observation: %w", err)
	}
	return nil
}

// ListByTask returns observations for a task ordered by timestamp.
func (s *ObservationStore) ListByTask(ctx context.Context, taskID string) ([]ExecutionObservation, error) {
	const q = `
		SELECT step_id, task_id, success, output, error, timestamp
		FROM agent_execution_observations
		WHERE task_id = $1 ORDER BY timestamp ASC`
	rows, err := s.pool.Query(ctx, q, taskID)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	defer rows.Close()

	var obs []ExecutionObservation
	for rows.Next() {
		var o ExecutionObservation
		var output []byte
		if err := rows.Scan(&o.StepID, &o.TaskID, &o.Success, &output, &o.Error, &o.Timestamp); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		if len(output) > 0 {
			o.Output = json.RawMessage(output)
		}
		obs = append(obs, o)
	}
	return obs, nil
}

// CountConsecutiveFailures returns the number of consecutive failures at the tail of observations.
func (s *ObservationStore) CountConsecutiveFailures(ctx context.Context, taskID string) (int, error) {
	obs, err := s.ListByTask(ctx, taskID)
	if err != nil {
		return 0, err
	}
	count := 0
	for i := len(obs) - 1; i >= 0; i-- {
		if !obs[i].Success {
			count++
		} else {
			break
		}
	}
	return count, nil
}

// InMemoryTaskStore is an in-memory implementation for testing.
type InMemoryTaskStore struct {
	tasks map[string]ExecutionTask
}

// NewInMemoryTaskStore creates a new in-memory task store.
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{tasks: make(map[string]ExecutionTask)}
}

// Insert stores a task.
func (s *InMemoryTaskStore) Insert(_ context.Context, t ExecutionTask) error {
	s.tasks[t.ID] = t
	return nil
}

// Get retrieves a task.
func (s *InMemoryTaskStore) Get(_ context.Context, id string) (ExecutionTask, error) {
	t, ok := s.tasks[id]
	if !ok {
		return ExecutionTask{}, ErrTaskNotFound
	}
	return t, nil
}

// Update replaces a task.
func (s *InMemoryTaskStore) Update(_ context.Context, t ExecutionTask) error {
	if _, ok := s.tasks[t.ID]; !ok {
		return ErrTaskNotFound
	}
	s.tasks[t.ID] = t
	return nil
}

// List returns all tasks.
func (s *InMemoryTaskStore) List(_ context.Context, limit int) ([]ExecutionTask, error) {
	var out []ExecutionTask
	for _, t := range s.tasks {
		out = append(out, t)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// InMemoryObservationStore is an in-memory implementation for testing.
type InMemoryObservationStore struct {
	obs []ExecutionObservation
}

// NewInMemoryObservationStore creates a new in-memory observation store.
func NewInMemoryObservationStore() *InMemoryObservationStore {
	return &InMemoryObservationStore{}
}

// Insert appends an observation.
func (s *InMemoryObservationStore) Insert(_ context.Context, o ExecutionObservation) error {
	s.obs = append(s.obs, o)
	return nil
}

// ListByTask returns observations for a task.
func (s *InMemoryObservationStore) ListByTask(_ context.Context, taskID string) ([]ExecutionObservation, error) {
	var out []ExecutionObservation
	for _, o := range s.obs {
		if o.TaskID == taskID {
			out = append(out, o)
		}
	}
	return out, nil
}

// CountConsecutiveFailures returns consecutive failures at the tail.
func (s *InMemoryObservationStore) CountConsecutiveFailures(_ context.Context, taskID string) (int, error) {
	obs, _ := s.ListByTask(nil, taskID)
	count := 0
	for i := len(obs) - 1; i >= 0; i-- {
		if !obs[i].Success {
			count++
		} else {
			break
		}
	}
	return count, nil
}

// TaskStoreInterface abstracts task persistence for testing.
type TaskStoreInterface interface {
	Insert(ctx context.Context, t ExecutionTask) error
	Get(ctx context.Context, id string) (ExecutionTask, error)
	Update(ctx context.Context, t ExecutionTask) error
	List(ctx context.Context, limit int) ([]ExecutionTask, error)
}

// ObservationStoreInterface abstracts observation persistence for testing.
type ObservationStoreInterface interface {
	Insert(ctx context.Context, o ExecutionObservation) error
	ListByTask(ctx context.Context, taskID string) ([]ExecutionObservation, error)
	CountConsecutiveFailures(ctx context.Context, taskID string) (int, error)
}

// Ensure concrete stores satisfy interfaces.
var _ TaskStoreInterface = (*TaskStore)(nil)
var _ TaskStoreInterface = (*InMemoryTaskStore)(nil)
var _ ObservationStoreInterface = (*ObservationStore)(nil)
var _ ObservationStoreInterface = (*InMemoryObservationStore)(nil)

// nowUTC is a replaceable clock for deterministic testing.
var nowUTC = func() time.Time { return time.Now().UTC() }
