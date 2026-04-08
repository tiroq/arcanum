package resource_optimization

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProfileStore persists and retrieves ResourceProfiles via PostgreSQL.
type ProfileStore struct {
	pool *pgxpool.Pool
}

// NewProfileStore creates a ProfileStore.
func NewProfileStore(pool *pgxpool.Pool) *ProfileStore {
	return &ProfileStore{pool: pool}
}

// RecordOutcome updates the resource profile for the given mode+goal_type
// using incremental rolling averages. Creates the row if it does not exist.
// Deterministic: same inputs always produce the same state transition.
func (s *ProfileStore) RecordOutcome(ctx context.Context, input ResourceOutcomeInput) error {
	query := `
		INSERT INTO agent_resource_profiles (mode, goal_type, avg_latency_ms, avg_reasoning_depth, avg_path_length, avg_token_cost, avg_execution_cost, sample_count, last_updated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1, NOW())
		ON CONFLICT (mode, goal_type) DO UPDATE SET
			avg_latency_ms       = (agent_resource_profiles.avg_latency_ms * agent_resource_profiles.sample_count + $3) / (agent_resource_profiles.sample_count + 1),
			avg_reasoning_depth  = (agent_resource_profiles.avg_reasoning_depth * agent_resource_profiles.sample_count + $4) / (agent_resource_profiles.sample_count + 1),
			avg_path_length      = (agent_resource_profiles.avg_path_length * agent_resource_profiles.sample_count + $5) / (agent_resource_profiles.sample_count + 1),
			avg_token_cost       = (agent_resource_profiles.avg_token_cost * agent_resource_profiles.sample_count + $6) / (agent_resource_profiles.sample_count + 1),
			avg_execution_cost   = (agent_resource_profiles.avg_execution_cost * agent_resource_profiles.sample_count + $7) / (agent_resource_profiles.sample_count + 1),
			sample_count         = agent_resource_profiles.sample_count + 1,
			last_updated         = NOW()
	`
	_, err := s.pool.Exec(ctx, query,
		input.Mode,
		input.GoalType,
		input.LatencyMs,
		input.ReasoningDepth,
		input.PathLength,
		input.TokenCost,
		input.ExecutionCost,
	)
	return err
}

// GetByModeAndGoal retrieves a single resource profile.
// Returns nil, nil if not found.
func (s *ProfileStore) GetByModeAndGoal(ctx context.Context, mode, goalType string) (*ResourceProfile, error) {
	query := `
		SELECT id, mode, goal_type, avg_latency_ms, avg_reasoning_depth, avg_path_length,
		       avg_token_cost, avg_execution_cost, sample_count, last_updated
		FROM agent_resource_profiles
		WHERE mode = $1 AND goal_type = $2
	`
	row := s.pool.QueryRow(ctx, query, mode, goalType)

	var p ResourceProfile
	err := row.Scan(
		&p.ID, &p.Mode, &p.GoalType, &p.AvgLatencyMs, &p.AvgReasoningDepth,
		&p.AvgPathLength, &p.AvgTokenCost, &p.AvgExecutionCost, &p.SampleCount, &p.LastUpdated,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetByMode retrieves all profiles for a given mode.
func (s *ProfileStore) GetByMode(ctx context.Context, mode string) ([]ResourceProfile, error) {
	query := `
		SELECT id, mode, goal_type, avg_latency_ms, avg_reasoning_depth, avg_path_length,
		       avg_token_cost, avg_execution_cost, sample_count, last_updated
		FROM agent_resource_profiles
		WHERE mode = $1
		ORDER BY goal_type
	`
	rows, err := s.pool.Query(ctx, query, mode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []ResourceProfile
	for rows.Next() {
		var p ResourceProfile
		if err := rows.Scan(
			&p.ID, &p.Mode, &p.GoalType, &p.AvgLatencyMs, &p.AvgReasoningDepth,
			&p.AvgPathLength, &p.AvgTokenCost, &p.AvgExecutionCost, &p.SampleCount, &p.LastUpdated,
		); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// GetAll retrieves all resource profiles.
func (s *ProfileStore) GetAll(ctx context.Context) ([]ResourceProfile, error) {
	query := `
		SELECT id, mode, goal_type, avg_latency_ms, avg_reasoning_depth, avg_path_length,
		       avg_token_cost, avg_execution_cost, sample_count, last_updated
		FROM agent_resource_profiles
		ORDER BY mode, goal_type
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []ResourceProfile
	for rows.Next() {
		var p ResourceProfile
		if err := rows.Scan(
			&p.ID, &p.Mode, &p.GoalType, &p.AvgLatencyMs, &p.AvgReasoningDepth,
			&p.AvgPathLength, &p.AvgTokenCost, &p.AvgExecutionCost, &p.SampleCount, &p.LastUpdated,
		); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// Tracker wraps ProfileStore and provides higher-level tracking operations.
type Tracker struct {
	store *ProfileStore
}

// NewTracker creates a Tracker.
func NewTracker(pool *pgxpool.Pool) *Tracker {
	return &Tracker{store: NewProfileStore(pool)}
}

// RecordDecisionOutcome records resource metrics after a decision outcome.
// If tokenCost is 0, computes a proxy from pathLength and mode complexity.
// If executionCost is 0, computes a normalized proxy.
func (t *Tracker) RecordDecisionOutcome(ctx context.Context, mode, goalType string, latencyMs, reasoningDepth, pathLength, tokenCost, executionCost float64) error {
	// Compute proxy costs if unavailable.
	if tokenCost <= 0 {
		tokenCost = pathLength * ModeComplexityWeight(mode)
	}
	if executionCost <= 0 {
		executionCost = tokenCost * 0.1 // normalized proxy
	}

	input := ResourceOutcomeInput{
		Mode:           mode,
		GoalType:       goalType,
		LatencyMs:      latencyMs,
		ReasoningDepth: reasoningDepth,
		PathLength:     pathLength,
		TokenCost:      tokenCost,
		ExecutionCost:  executionCost,
	}
	return t.store.RecordOutcome(ctx, input)
}

// GetProfile retrieves a resource profile for a mode+goal_type.
// Returns nil if not found (fail-open).
func (t *Tracker) GetProfile(ctx context.Context, mode, goalType string) *ResourceProfile {
	p, err := t.store.GetByModeAndGoal(ctx, mode, goalType)
	if err != nil || p == nil {
		return nil
	}
	return p
}

// GetAllProfiles retrieves all resource profiles.
func (t *Tracker) GetAllProfiles(ctx context.Context) []ResourceProfile {
	profiles, err := t.store.GetAll(ctx)
	if err != nil {
		return nil
	}
	return profiles
}

// BuildSummary computes an aggregate resource summary from all profiles.
func (t *Tracker) BuildSummary(ctx context.Context) ResourceSummary {
	profiles := t.GetAllProfiles(ctx)

	summary := ResourceSummary{
		TotalProfiles:  len(profiles),
		PressureState:  "none",
		ProfilesByMode: make(map[string][]ResourceProfile),
	}

	if len(profiles) == 0 {
		return summary
	}

	totalEfficiency := 0.0
	maxLatency := 0.0
	maxCost := 0.0

	for _, p := range profiles {
		summary.ProfilesByMode[p.Mode] = append(summary.ProfilesByMode[p.Mode], p)
		signals := ComputeSignals(p)
		totalEfficiency += signals.EfficiencyScore

		if p.AvgLatencyMs > maxLatency {
			maxLatency = p.AvgLatencyMs
		}
		if p.AvgExecutionCost > maxCost {
			maxCost = p.AvgExecutionCost
		}
	}

	summary.AvgEfficiency = totalEfficiency / float64(len(profiles))

	// Determine pressure state.
	if maxLatency >= HighLatencyPressureThreshold && maxCost >= HighCostPressureThreshold {
		summary.PressureState = "high_latency_and_cost"
	} else if maxLatency >= HighLatencyPressureThreshold {
		summary.PressureState = "high_latency"
	} else if maxCost >= HighCostPressureThreshold {
		summary.PressureState = "high_cost"
	}

	return summary
}

// recentDecisions stores a bounded FIFO of recent decisions for API visibility.
var recentDecisions []ResourceDecisionRecord

// RecordDecision stores a resource decision record for API visibility.
func RecordDecision(record ResourceDecisionRecord) {
	record.Timestamp = time.Now().UTC()
	recentDecisions = append(recentDecisions, record)
	if len(recentDecisions) > MaxRecentDecisions {
		recentDecisions = recentDecisions[len(recentDecisions)-MaxRecentDecisions:]
	}
}

// GetRecentDecisions returns the most recent resource decision records.
func GetRecentDecisions() []ResourceDecisionRecord {
	if recentDecisions == nil {
		return []ResourceDecisionRecord{}
	}
	result := make([]ResourceDecisionRecord, len(recentDecisions))
	copy(result, recentDecisions)
	return result
}
