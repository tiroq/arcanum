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
			id, goal_id, plan_id, title, description, status, progress_score,
			target_metric, target_value, current_value, preferred_action,
			horizon, priority, depends_on, block_reason, strategy,
			failure_count, success_count,
			last_task_emitted, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		sg.ID, sg.GoalID, sg.PlanID, sg.Title, sg.Description, string(sg.Status), sg.ProgressScore,
		sg.TargetMetric, sg.TargetValue, sg.CurrentValue, sg.PreferredAction,
		string(sg.Horizon), sg.Priority, sg.DependsOn, sg.BlockReason, string(sg.Strategy),
		sg.FailureCount, sg.SuccessCount,
		sg.LastTaskEmitted, sg.CreatedAt, sg.UpdatedAt,
	)
	return err
}

// Get retrieves a subgoal by ID.
func (s *SubgoalStore) Get(ctx context.Context, id string) (Subgoal, error) {
	var sg Subgoal
	var status, horizon, strategy string
	err := s.pool.QueryRow(ctx, `
		SELECT id, goal_id, plan_id, title, description, status, progress_score,
		       target_metric, target_value, current_value, preferred_action,
		       horizon, priority, depends_on, block_reason, strategy,
		       failure_count, success_count,
		       last_task_emitted, created_at, updated_at
		FROM agent_subgoals WHERE id = $1`, id,
	).Scan(
		&sg.ID, &sg.GoalID, &sg.PlanID, &sg.Title, &sg.Description, &status, &sg.ProgressScore,
		&sg.TargetMetric, &sg.TargetValue, &sg.CurrentValue, &sg.PreferredAction,
		&horizon, &sg.Priority, &sg.DependsOn, &sg.BlockReason, &strategy,
		&sg.FailureCount, &sg.SuccessCount,
		&sg.LastTaskEmitted, &sg.CreatedAt, &sg.UpdatedAt,
	)
	sg.Status = SubgoalStatus(status)
	sg.Horizon = Horizon(horizon)
	sg.Strategy = Strategy(strategy)
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

// ListByPlan returns all subgoals for a given plan ID.
func (s *SubgoalStore) ListByPlan(ctx context.Context, planID string) ([]Subgoal, error) {
	return s.listWhere(ctx, "plan_id = $1", planID)
}

// ListActive returns all subgoals in active status.
func (s *SubgoalStore) ListActive(ctx context.Context) ([]Subgoal, error) {
	return s.listWhere(ctx, "status = $1", string(SubgoalActive))
}

// ListAll returns all subgoals.
func (s *SubgoalStore) ListAll(ctx context.Context) ([]Subgoal, error) {
	return s.listWhere(ctx, "1=1")
}

// UpdateStrategy updates a subgoal's strategy and execution counters.
func (s *SubgoalStore) UpdateStrategy(ctx context.Context, id string, strategy Strategy, failureCount, successCount int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_subgoals
		SET strategy = $2, failure_count = $3, success_count = $4, updated_at = $5
		WHERE id = $1`,
		id, string(strategy), failureCount, successCount, time.Now().UTC(),
	)
	return err
}

func (s *SubgoalStore) listWhere(ctx context.Context, where string, args ...any) ([]Subgoal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, goal_id, plan_id, title, description, status, progress_score,
		       target_metric, target_value, current_value, preferred_action,
		       horizon, priority, depends_on, block_reason, strategy,
		       failure_count, success_count,
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
		var status, horizon, strategy string
		if err := rows.Scan(
			&sg.ID, &sg.GoalID, &sg.PlanID, &sg.Title, &sg.Description, &status, &sg.ProgressScore,
			&sg.TargetMetric, &sg.TargetValue, &sg.CurrentValue, &sg.PreferredAction,
			&horizon, &sg.Priority, &sg.DependsOn, &sg.BlockReason, &strategy,
			&sg.FailureCount, &sg.SuccessCount,
			&sg.LastTaskEmitted, &sg.CreatedAt, &sg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		sg.Status = SubgoalStatus(status)
		sg.Horizon = Horizon(horizon)
		sg.Strategy = Strategy(strategy)
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

func (s *InMemorySubgoalStore) ListByPlan(_ context.Context, planID string) ([]Subgoal, error) {
	var result []Subgoal
	for _, sg := range s.subgoals {
		if sg.PlanID == planID {
			result = append(result, sg)
		}
	}
	return result, nil
}

func (s *InMemorySubgoalStore) UpdateStrategy(_ context.Context, id string, strategy Strategy, failureCount, successCount int) error {
	sg, ok := s.subgoals[id]
	if !ok {
		return context.Canceled
	}
	sg.Strategy = strategy
	sg.FailureCount = failureCount
	sg.SuccessCount = successCount
	sg.UpdatedAt = time.Now().UTC()
	s.subgoals[id] = sg
	return nil
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
	UpdateStrategy(ctx context.Context, id string, strategy Strategy, failureCount, successCount int) error
	ListByGoal(ctx context.Context, goalID string) ([]Subgoal, error)
	ListByPlan(ctx context.Context, planID string) ([]Subgoal, error)
	ListActive(ctx context.Context) ([]Subgoal, error)
	ListAll(ctx context.Context) ([]Subgoal, error)
}

// ProgressStoreInterface abstracts the progress store for testing.
type ProgressStoreInterface interface {
	Insert(ctx context.Context, p GoalProgress) error
	LatestForSubgoal(ctx context.Context, subgoalID string) (GoalProgress, error)
	ListByGoal(ctx context.Context, goalID string) ([]GoalProgress, error)
}

// PlanStoreInterface abstracts the plan store for testing.
type PlanStoreInterface interface {
	Insert(ctx context.Context, p GoalPlan) error
	Get(ctx context.Context, id string) (GoalPlan, error)
	GetByGoal(ctx context.Context, goalID string) (GoalPlan, error)
	UpdateStatus(ctx context.Context, id string, status PlanStatus) error
	IncrementVersion(ctx context.Context, id string) error
	IncrementReplanCount(ctx context.Context, id string) error
	ListAll(ctx context.Context) ([]GoalPlan, error)
}

// DependencyStoreInterface abstracts the dependency store for testing.
type DependencyStoreInterface interface {
	Insert(ctx context.Context, d GoalDependency) error
	ListByPlan(ctx context.Context, planID string) ([]GoalDependency, error)
	DeleteByPlan(ctx context.Context, planID string) error
}

// --- PlanStore (PostgreSQL) ---

// PlanStore handles database operations for goal plans.
type PlanStore struct {
	pool *pgxpool.Pool
}

// NewPlanStore creates a new plan store.
func NewPlanStore(pool *pgxpool.Pool) *PlanStore {
	return &PlanStore{pool: pool}
}

// Insert creates a new goal plan.
func (s *PlanStore) Insert(ctx context.Context, p GoalPlan) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_goal_plans (
			id, goal_id, version, horizon, strategy, status,
			expected_utility, risk_estimate, replan_count,
			last_replan_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.ID, p.GoalID, p.Version, string(p.Horizon), string(p.Strategy), string(p.Status),
		p.ExpectedUtility, p.RiskEstimate, p.ReplanCount,
		p.LastReplanAt, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

// Get retrieves a plan by ID.
func (s *PlanStore) Get(ctx context.Context, id string) (GoalPlan, error) {
	var p GoalPlan
	var horizon, strategy, status string
	err := s.pool.QueryRow(ctx, `
		SELECT id, goal_id, version, horizon, strategy, status,
		       expected_utility, risk_estimate, replan_count,
		       last_replan_at, created_at, updated_at
		FROM agent_goal_plans WHERE id = $1`, id,
	).Scan(
		&p.ID, &p.GoalID, &p.Version, &horizon, &strategy, &status,
		&p.ExpectedUtility, &p.RiskEstimate, &p.ReplanCount,
		&p.LastReplanAt, &p.CreatedAt, &p.UpdatedAt,
	)
	p.Horizon = Horizon(horizon)
	p.Strategy = Strategy(strategy)
	p.Status = PlanStatus(status)
	return p, err
}

// GetByGoal returns the latest plan for a goal.
func (s *PlanStore) GetByGoal(ctx context.Context, goalID string) (GoalPlan, error) {
	var p GoalPlan
	var horizon, strategy, status string
	err := s.pool.QueryRow(ctx, `
		SELECT id, goal_id, version, horizon, strategy, status,
		       expected_utility, risk_estimate, replan_count,
		       last_replan_at, created_at, updated_at
		FROM agent_goal_plans WHERE goal_id = $1
		ORDER BY version DESC LIMIT 1`, goalID,
	).Scan(
		&p.ID, &p.GoalID, &p.Version, &horizon, &strategy, &status,
		&p.ExpectedUtility, &p.RiskEstimate, &p.ReplanCount,
		&p.LastReplanAt, &p.CreatedAt, &p.UpdatedAt,
	)
	p.Horizon = Horizon(horizon)
	p.Strategy = Strategy(strategy)
	p.Status = PlanStatus(status)
	return p, err
}

// UpdateStatus transitions a plan's status.
func (s *PlanStore) UpdateStatus(ctx context.Context, id string, status PlanStatus) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_goal_plans SET status = $2, updated_at = $3 WHERE id = $1`,
		id, string(status), time.Now().UTC(),
	)
	return err
}

// IncrementVersion bumps the plan version.
func (s *PlanStore) IncrementVersion(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_goal_plans SET version = version + 1, updated_at = $2 WHERE id = $1`,
		id, time.Now().UTC(),
	)
	return err
}

// IncrementReplanCount increments the replan counter and records the timestamp.
func (s *PlanStore) IncrementReplanCount(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_goal_plans SET replan_count = replan_count + 1, last_replan_at = $2, updated_at = $3 WHERE id = $1`,
		id, now, now,
	)
	return err
}

// ListAll returns all plans.
func (s *PlanStore) ListAll(ctx context.Context) ([]GoalPlan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, goal_id, version, horizon, strategy, status,
		       expected_utility, risk_estimate, replan_count,
		       last_replan_at, created_at, updated_at
		FROM agent_goal_plans ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []GoalPlan
	for rows.Next() {
		var p GoalPlan
		var horizon, strategy, status string
		if err := rows.Scan(
			&p.ID, &p.GoalID, &p.Version, &horizon, &strategy, &status,
			&p.ExpectedUtility, &p.RiskEstimate, &p.ReplanCount,
			&p.LastReplanAt, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.Horizon = Horizon(horizon)
		p.Strategy = Strategy(strategy)
		p.Status = PlanStatus(status)
		result = append(result, p)
	}
	return result, rows.Err()
}

// --- DependencyStore (PostgreSQL) ---

// DependencyStore handles database operations for goal dependencies.
type DependencyStore struct {
	pool *pgxpool.Pool
}

// NewDependencyStore creates a new dependency store.
func NewDependencyStore(pool *pgxpool.Pool) *DependencyStore {
	return &DependencyStore{pool: pool}
}

// Insert creates a new dependency.
func (s *DependencyStore) Insert(ctx context.Context, d GoalDependency) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_goal_dependencies (id, plan_id, from_subgoal_id, to_subgoal_id, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		d.ID, d.PlanID, d.FromSubgoalID, d.ToSubgoalID, d.CreatedAt,
	)
	return err
}

// ListByPlan returns all dependencies for a plan.
func (s *DependencyStore) ListByPlan(ctx context.Context, planID string) ([]GoalDependency, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, plan_id, from_subgoal_id, to_subgoal_id, created_at
		FROM agent_goal_dependencies WHERE plan_id = $1`, planID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []GoalDependency
	for rows.Next() {
		var d GoalDependency
		if err := rows.Scan(&d.ID, &d.PlanID, &d.FromSubgoalID, &d.ToSubgoalID, &d.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// DeleteByPlan removes all dependencies for a plan.
func (s *DependencyStore) DeleteByPlan(ctx context.Context, planID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM agent_goal_dependencies WHERE plan_id = $1`, planID)
	return err
}

// --- In-memory PlanStore ---

// InMemoryPlanStore is a test-only plan store.
type InMemoryPlanStore struct {
	plans map[string]GoalPlan
}

// NewInMemoryPlanStore creates a new in-memory plan store.
func NewInMemoryPlanStore() *InMemoryPlanStore {
	return &InMemoryPlanStore{plans: make(map[string]GoalPlan)}
}

func (s *InMemoryPlanStore) Insert(_ context.Context, p GoalPlan) error {
	s.plans[p.ID] = p
	return nil
}

func (s *InMemoryPlanStore) Get(_ context.Context, id string) (GoalPlan, error) {
	p, ok := s.plans[id]
	if !ok {
		return GoalPlan{}, context.Canceled
	}
	return p, nil
}

func (s *InMemoryPlanStore) GetByGoal(_ context.Context, goalID string) (GoalPlan, error) {
	var latest GoalPlan
	found := false
	for _, p := range s.plans {
		if p.GoalID == goalID && (!found || p.Version > latest.Version) {
			latest = p
			found = true
		}
	}
	if !found {
		return GoalPlan{}, context.Canceled
	}
	return latest, nil
}

func (s *InMemoryPlanStore) UpdateStatus(_ context.Context, id string, status PlanStatus) error {
	p, ok := s.plans[id]
	if !ok {
		return context.Canceled
	}
	p.Status = status
	p.UpdatedAt = time.Now().UTC()
	s.plans[id] = p
	return nil
}

func (s *InMemoryPlanStore) IncrementVersion(_ context.Context, id string) error {
	p, ok := s.plans[id]
	if !ok {
		return context.Canceled
	}
	p.Version++
	p.UpdatedAt = time.Now().UTC()
	s.plans[id] = p
	return nil
}

func (s *InMemoryPlanStore) IncrementReplanCount(_ context.Context, id string) error {
	p, ok := s.plans[id]
	if !ok {
		return context.Canceled
	}
	p.ReplanCount++
	p.LastReplanAt = time.Now().UTC()
	p.UpdatedAt = time.Now().UTC()
	s.plans[id] = p
	return nil
}

func (s *InMemoryPlanStore) ListAll(_ context.Context) ([]GoalPlan, error) {
	var result []GoalPlan
	for _, p := range s.plans {
		result = append(result, p)
	}
	return result, nil
}

// --- In-memory DependencyStore ---

// InMemoryDependencyStore is a test-only dependency store.
type InMemoryDependencyStore struct {
	deps []GoalDependency
}

// NewInMemoryDependencyStore creates a new in-memory dependency store.
func NewInMemoryDependencyStore() *InMemoryDependencyStore {
	return &InMemoryDependencyStore{}
}

func (s *InMemoryDependencyStore) Insert(_ context.Context, d GoalDependency) error {
	s.deps = append(s.deps, d)
	return nil
}

func (s *InMemoryDependencyStore) ListByPlan(_ context.Context, planID string) ([]GoalDependency, error) {
	var result []GoalDependency
	for _, d := range s.deps {
		if d.PlanID == planID {
			result = append(result, d)
		}
	}
	return result, nil
}

func (s *InMemoryDependencyStore) DeleteByPlan(_ context.Context, planID string) error {
	var kept []GoalDependency
	for _, d := range s.deps {
		if d.PlanID != planID {
			kept = append(kept, d)
		}
	}
	s.deps = kept
	return nil
}
