package goals

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// lookbackWindow is how far back the engine looks for recent job/proposal
// activity when building a SystemSnapshot.
const lookbackWindow = 24 * time.Hour

// GoalEngine observes system state and produces advisory goals.
// It is strictly read-only: no writes, no mutations, no side effects.
type GoalEngine struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

// NewGoalEngine creates a GoalEngine.
func NewGoalEngine(db *pgxpool.Pool, logger *zap.Logger) *GoalEngine {
	return &GoalEngine{db: db, logger: logger}
}

// Evaluate collects a point-in-time snapshot of system state and derives
// advisory goals. The returned goals are sorted by priority (highest first).
func (e *GoalEngine) Evaluate(ctx context.Context) ([]Goal, error) {
	snap, err := e.collectSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("goal engine: collect snapshot: %w", err)
	}

	goals := EvaluateSystem(snap)

	// Sort by priority descending (most urgent first).
	sort.Slice(goals, func(i, j int) bool {
		return goals[i].Priority > goals[j].Priority
	})

	topGoal := ""
	topConf := 0.0
	if len(goals) > 0 {
		topGoal = goals[0].Type
		topConf = goals[0].Confidence
	}
	e.logger.Info("goal_engine_evaluation",
		zap.Int("goals_count", len(goals)),
		zap.String("top_goal", topGoal),
		zap.Float64("confidence", topConf),
	)

	return goals, nil
}

// collectSnapshot gathers all data the evaluator needs in a single pass.
// Every query is a read-only SELECT — no writes, no locks.
func (e *GoalEngine) collectSnapshot(ctx context.Context) (SystemSnapshot, error) {
	snap := SystemSnapshot{
		QueueStats: map[string]int64{
			"queued":          0,
			"leased":          0,
			"retry_scheduled": 0,
			"failed":          0,
			"dead_letter":     0,
		},
	}

	// 1. Queue stats (current counts).
	if err := e.loadQueueStats(ctx, &snap); err != nil {
		return snap, err
	}

	// 2. Recent job activity (lookback window).
	if err := e.loadRecentJobStats(ctx, &snap); err != nil {
		return snap, err
	}

	// 3. Proposal acceptance stats (lookback window).
	if err := e.loadProposalStats(ctx, &snap); err != nil {
		return snap, err
	}

	// 4. Average latency (lookback window).
	if err := e.loadLatencyStats(ctx, &snap); err != nil {
		return snap, err
	}

	return snap, nil
}

func (e *GoalEngine) loadQueueStats(ctx context.Context, snap *SystemSnapshot) error {
	const q = `
		SELECT status, COUNT(*) AS cnt
		FROM processing_jobs
		WHERE status IN ('queued', 'leased', 'retry_scheduled', 'failed', 'dead_letter')
		GROUP BY status`

	rows, err := e.db.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("queue stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var cnt int64
		if err := rows.Scan(&status, &cnt); err != nil {
			return fmt.Errorf("queue stats scan: %w", err)
		}
		snap.QueueStats[status] = cnt
	}
	return rows.Err()
}

func (e *GoalEngine) loadRecentJobStats(ctx context.Context, snap *SystemSnapshot) error {
	since := time.Now().UTC().Add(-lookbackWindow)

	const q = `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('succeeded', 'failed', 'dead_letter')) AS total,
			COUNT(*) FILTER (WHERE status = 'failed')                                AS failed,
			COUNT(*) FILTER (WHERE status = 'dead_letter')                           AS dead_letter,
			COUNT(*) FILTER (WHERE status = 'succeeded')                             AS succeeded
		FROM processing_jobs
		WHERE updated_at >= $1`

	return e.db.QueryRow(ctx, q, since).Scan(
		&snap.TotalJobsRecent,
		&snap.FailedJobsRecent,
		&snap.DeadLetterRecent,
		&snap.SucceededJobsRecent,
	)
}

func (e *GoalEngine) loadProposalStats(ctx context.Context, snap *SystemSnapshot) error {
	since := time.Now().UTC().Add(-lookbackWindow)

	const q = `
		SELECT
			COUNT(*) FILTER (WHERE approval_status = 'approved') AS accepted,
			COUNT(*) FILTER (WHERE approval_status = 'rejected') AS rejected,
			COUNT(*) FILTER (WHERE approval_status IN ('approved', 'rejected')) AS total
		FROM suggestion_proposals
		WHERE updated_at >= $1`

	return e.db.QueryRow(ctx, q, since).Scan(
		&snap.AcceptedProposals,
		&snap.RejectedProposals,
		&snap.TotalProposals,
	)
}

func (e *GoalEngine) loadLatencyStats(ctx context.Context, snap *SystemSnapshot) error {
	since := time.Now().UTC().Add(-lookbackWindow)

	const q = `
		SELECT COALESCE(AVG(duration_ms), 0)
		FROM processing_runs
		WHERE started_at >= $1 AND duration_ms > 0`

	return e.db.QueryRow(ctx, q, since).Scan(&snap.AvgLatencyMS)
}
