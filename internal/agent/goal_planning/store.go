package goal_planning

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SubgoalStore handles database operations for subgoals.
type SubgoalStore struct {
	pool *pgxpool.Pool
}

// NewSubgoalStore creates a new subgoal store.
func NewSubgoalStore(pool *pgxpool.Pool) *SubgoalStore {
	return &SubgoalStore{pool: pool}
}

// Insert creates a new subgoal.
func (s *SubgoalStore) Insert(ctx context.Context, sg Subgoal) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_subgoals (
			id, goal_id, title, description, status, progress_score,
			target_metric, target_value, current_value, preferred_action,
			horizon, priority, depends_on, block_reason,
			last_task_emitted, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		sg.ID, sg.GoalID, sg.Title, sg.Description, string(sg.Status), sg.ProgressScore,
		sg.TargetMetric, sg.TargetValue, sg.CurrentValue, sg.PreferredAction,
		string(sg.Horizon), sg.Priority, sg.DependsOn, sg.BlockReason,
		sg.LastTaskEmitted, sg.CreatedAt, sg.UpdatedAt,
	)
	return err
}

// Get retrieves a subgoal by ID.
func (s *SubgoalStore) Get(ctx context.Context, id string) (Subgoal, error) {
	var sg Subgoal
	var status, horizon string
	err := s.pool.QueryRow(ctx, `
		SELECT id, goal_id, title, description, status, progress_score,
		       target_metric, target_value, current_value, preferred_action,
		       horizon, priority, depends_on, block_reason,
		       last_task_emitted, created_at, updated_at
		FROM agent_subgoals WHERE id = $1`, id,
	).Scan(
		&sg.ID, &sg.GoalID, &sg.Title, &sg.Description, &status, &sg.ProgressScore,
		&sg.TargetMetric, &sg.TargetValue, &sg.CurrentValue, &sg.PreferredAction,
		&horizon, &sg.Priority, &sg.DependsOn, &sg.BlockReason,
		&sg.LastTaskEmitted, &sg.CreatedAt, &sg.UpdatedAt,
	)
	sg.Status = SubgoalStatus(status)
	sg.Horizon = Horizon(horizon)
	return sg, err
}

// UpdateStatus transitions a subgoal's status.
func (s *SubgoalStore) UpdateStatus(ctx context.Context, id string, status SubgoalStatus, blockReason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_subgoals
		SET status = $2, block_reason = $3, updated_at = $4
		WHERE id = $1`,
		id, string(status), blockReason, time.Now().UTC(),
	)
	return err
}

// UpdateProgress updates a subgoal's progress and current value.
func (s *SubgoalStore) UpdateProgress(ctx context.Context, id string, progressScore, currentValue float64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_subgoals
		SET progress_score = $2, current_value = $3, updated_at = $4
		WHERE id = $1`,
		id, progressScore, currentValue, time.Now().UTC(),
	)
	return err
}

// UpdateLastTaskEmitted records when a task was last emitted for a subgoal.
func (s *SubgoalStore) UpdateLastTaskEmitted(ctx context.Context, id string, t time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_subgoals SET last_task_emitted = $2, updated_at = $3 WHERE id = $1`,
		id, t, time.Now().UTC(),
	)
	return err
}

// ListByGoal returns all subgoals for a given goal ID.
func (s *SubgoalStore) ListByGoal(ctx context.Context, goalID string) ([]Subgoal, error) {
	return s.listWhere(ctx, "goal_id = $1", goalID)
}

// ListActive returns all subgoals in active status.
func (s *SubgoalStore) ListActive(ctx context.Context) ([]Subgoal, error) {
	return s.listWhere(ctx, "status = $1", string(SubgoalActive))
}

// ListAll returns all subgoals.
func (s *SubgoalStore) ListAll(ctx context.Context) ([]Subgoal, error) {
	return s.listWhere(ctx, "1=1")
}

func (s *SubgoalStore) listWhere(ctx context.Context, where string, args ...any) ([]Subgoal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, goal_id, title, description, status, progress_score,
		       target_metric, target_value, current_value, preferred_action,
		       horizon, priority, depends_on, block_reason,
		       last_task_emitted, created_at, updated_at
		FROM agent_subgoals WHERE `+where+` ORDER BY priority DESC, created_at ASC`, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Subgoal
	for rows.Next() {
		var sg Subgoal
		var status, horizon string
		if err := rows.Scan(
			&sg.ID, &sg.GoalID, &sg.Title, &sg.Description, &status, &sg.ProgressScore,
			&sg.TargetMetric, &sg.TargetValue, &sg.CurrentValue, &sg.PreferredAction,
			&horizon, &sg.Priority, &sg.DependsOn, &sg.BlockReason,
			&sg.LastTaskEmitted, &sg.CreatedAt, &sg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		sg.Status = SubgoalStatus(status)
		sg.Horizon = Horizon(horizon)
		result = append(result, sg)
	}
	return result, rows.Err()
}

// ProgressStore handles database operations for goal progress measurements.
type ProgressStore struct {
	pool *pgxpool.Pool
}

// NewProgressStore creates a new progress store.
func NewProgressStore(pool *pgxpool.Pool) *ProgressStore {
	return &ProgressStore{pool: pool}
}

// Insert records a progress measurement.
func (s *ProgressStore) Insert(ctx context.Context, p GoalProgress) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_goal_progress (id, subgoal_id, goal_id, metric_name, metric_value, progress_pct, measured_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.ID, p.SubgoalID, p.GoalID, p.MetricName, p.MetricValue, p.ProgressPct, p.MeasuredAt,
	)
	return err
}

// LatestForSubgoal returns the most recent progress measurement for a subgoal.
func (s *ProgressStore) LatestForSubgoal(ctx context.Context, subgoalID string) (GoalProgress, error) {
	var p GoalProgress
	err := s.pool.QueryRow(ctx, `
		SELECT id, subgoal_id, goal_id, metric_name, metric_value, progress_pct, measured_at
		FROM agent_goal_progress WHERE subgoal_id = $1
		ORDER BY measured_at DESC LIMIT 1`, subgoalID,
	).Scan(&p.ID, &p.SubgoalID, &p.GoalID, &p.MetricName, &p.MetricValue, &p.ProgressPct, &p.MeasuredAt)
	return p, err
}

// ListByGoal returns all progress records for a goal.
func (s *ProgressStore) ListByGoal(ctx context.Context, goalID string) ([]GoalProgress, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, subgoal_id, goal_id, metric_name, metric_value, progress_pct, measured_at
		FROM agent_goal_progress WHERE goal_id = $1
		ORDER BY measured_at DESC`, goalID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []GoalProgress
	for rows.Next() {
		var p GoalProgress
		if err := rows.Scan(&p.ID, &p.SubgoalID, &p.GoalID, &p.MetricName, &p.MetricValue, &p.ProgressPct, &p.MeasuredAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// --- In-memory stores for testing ---

// InMemorySubgoalStore is a test-only subgoal store.
type InMemorySubgoalStore struct {
	subgoals map[string]Subgoal
}

// NewInMemorySubgoalStore creates a new in-memory subgoal store.
func NewInMemorySubgoalStore() *InMemorySubgoalStore {
	return &InMemorySubgoalStore{subgoals: make(map[string]Subgoal)}
}

func (s *InMemorySubgoalStore) Insert(_ context.Context, sg Subgoal) error {
	s.subgoals[sg.ID] = sg
	return nil
}

func (s *InMemorySubgoalStore) Get(_ context.Context, id string) (Subgoal, error) {
	sg, ok := s.subgoals[id]
	if !ok {
		return Subgoal{}, context.Canceled // simulate not-found
	}
	return sg, nil
}

func (s *InMemorySubgoalStore) UpdateStatus(_ context.Context, id string, status SubgoalStatus, blockReason string) error {
	sg, ok := s.subgoals[id]
	if !ok {
		return context.Canceled
	}
	sg.Status = status
	sg.BlockReason = blockReason
	sg.UpdatedAt = time.Now().UTC()
	s.subgoals[id] = sg
	return nil
}

func (s *InMemorySubgoalStore) UpdateProgress(_ context.Context, id string, progressScore, currentValue float64) error {
	sg, ok := s.subgoals[id]
	if !ok {
		return context.Canceled
	}
	sg.ProgressScore = progressScore
	sg.CurrentValue = currentValue
	sg.UpdatedAt = time.Now().UTC()
	s.subgoals[id] = sg
	return nil
}

func (s *InMemorySubgoalStore) UpdateLastTaskEmitted(_ context.Context, id string, t time.Time) error {
	sg, ok := s.subgoals[id]
	if !ok {
		return context.Canceled
	}
	sg.LastTaskEmitted = t
	sg.UpdatedAt = time.Now().UTC()
	s.subgoals[id] = sg
	return nil
}

func (s *InMemorySubgoalStore) ListByGoal(_ context.Context, goalID string) ([]Subgoal, error) {
	var result []Subgoal
	for _, sg := range s.subgoals {
		if sg.GoalID == goalID {
			result = append(result, sg)
		}
	}
	return result, nil
}

func (s *InMemorySubgoalStore) ListActive(_ context.Context) ([]Subgoal, error) {
	var result []Subgoal
	for _, sg := range s.subgoals {
		if sg.Status == SubgoalActive {
			result = append(result, sg)
		}
	}
	return result, nil
}

func (s *InMemorySubgoalStore) ListAll(_ context.Context) ([]Subgoal, error) {
	var result []Subgoal
	for _, sg := range s.subgoals {
		result = append(result, sg)
	}
	return result, nil
}

// InMemoryProgressStore is a test-only progress store.
type InMemoryProgressStore struct {
	entries []GoalProgress
}

// NewInMemoryProgressStore creates a new in-memory progress store.
func NewInMemoryProgressStore() *InMemoryProgressStore {
	return &InMemoryProgressStore{}
}

func (s *InMemoryProgressStore) Insert(_ context.Context, p GoalProgress) error {
	s.entries = append(s.entries, p)
	return nil
}

func (s *InMemoryProgressStore) LatestForSubgoal(_ context.Context, subgoalID string) (GoalProgress, error) {
	var latest GoalProgress
	found := false
	for _, p := range s.entries {
		if p.SubgoalID == subgoalID && (!found || p.MeasuredAt.After(latest.MeasuredAt)) {
			latest = p
			found = true
		}
	}
	if !found {
		return GoalProgress{}, context.Canceled
	}
	return latest, nil
}

func (s *InMemoryProgressStore) ListByGoal(_ context.Context, goalID string) ([]GoalProgress, error) {
	var result []GoalProgress
	for _, p := range s.entries {
		if p.GoalID == goalID {
			result = append(result, p)
		}
	}
	return result, nil
}

// SubgoalStoreInterface abstracts the subgoal store for testing.
type SubgoalStoreInterface interface {
	Insert(ctx context.Context, sg Subgoal) error
	Get(ctx context.Context, id string) (Subgoal, error)
	UpdateStatus(ctx context.Context, id string, status SubgoalStatus, blockReason string) error
	UpdateProgress(ctx context.Context, id string, progressScore, currentValue float64) error
	UpdateLastTaskEmitted(ctx context.Context, id string, t time.Time) error
	ListByGoal(ctx context.Context, goalID string) ([]Subgoal, error)
	ListActive(ctx context.Context) ([]Subgoal, error)
	ListAll(ctx context.Context) ([]Subgoal, error)
}

// ProgressStoreInterface abstracts the progress store for testing.
type ProgressStoreInterface interface {
	Insert(ctx context.Context, p GoalProgress) error
	LatestForSubgoal(ctx context.Context, subgoalID string) (GoalProgress, error)
	ListByGoal(ctx context.Context, goalID string) ([]GoalProgress, error)
}
