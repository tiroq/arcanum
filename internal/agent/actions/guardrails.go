package actions

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Guardrail thresholds.
const (
	// dedupeWindow prevents the same action target from being acted on
	// more than once within this period.
	dedupeWindow = 2 * time.Minute

	// backlogThreshold rejects non-critical actions when the queue is
	// overloaded.
	backlogThreshold int64 = 100

	// maxRecentRetries rejects retry actions if the target job has already
	// been retried this many times in the current window.
	maxRecentRetries = 3
)

// FeedbackProvider supplies learning signals for action types.
// Implemented by the actionmemory package to avoid import cycles.
type FeedbackProvider interface {
	ShouldAvoid(ctx context.Context, actionType string) (bool, string)
}

// StabilityChecker tells guardrails whether an action type is currently
// blocked by the stability layer. Implemented by stability.Engine.
type StabilityChecker interface {
	IsActionBlocked(ctx context.Context, actionType string) (bool, string)
}

// Guardrails evaluates whether planned actions are safe to execute.
// It enforces rate limits, deduplication, and system load checks.
type Guardrails struct {
	db        *pgxpool.Pool
	logger    *zap.Logger
	feedback  FeedbackProvider
	stability StabilityChecker

	mu          sync.Mutex
	recentExecs map[string]time.Time // action key → last execution time
}

// NewGuardrails creates a Guardrails evaluator.
func NewGuardrails(db *pgxpool.Pool, logger *zap.Logger) *Guardrails {
	return &Guardrails{
		db:          db,
		logger:      logger,
		recentExecs: make(map[string]time.Time),
	}
}

// WithFeedback attaches a FeedbackProvider for learning-based rejection.
func (g *Guardrails) WithFeedback(fp FeedbackProvider) *Guardrails {
	g.feedback = fp
	return g
}

// WithStability attaches a StabilityChecker for stability-layer blocking.
func (g *Guardrails) WithStability(sc StabilityChecker) *Guardrails {
	g.stability = sc
	return g
}

// EvaluateSafety checks whether an action is safe to execute.
// Returns (safe bool, reason string).
func (g *Guardrails) EvaluateSafety(ctx context.Context, action Action) (bool, string) {
	// 0a. Check stability layer — reject actions blocked by stability controls.
	if g.stability != nil && action.Type != string(ActionLogRecommendation) {
		if blocked, reason := g.stability.IsActionBlocked(ctx, action.Type); blocked {
			return false, reason
		}
	}

	// 0b. Check feedback — reject actions with consistently poor outcomes.
	if g.feedback != nil && action.Type != string(ActionLogRecommendation) {
		if avoid, reason := g.feedback.ShouldAvoid(ctx, action.Type); avoid {
			return false, reason
		}
	}

	// 1. Check dedupe window — same target should not be acted on too frequently.
	key := actionDedupeKey(action)
	if g.isRecentDuplicate(key) {
		return false, fmt.Sprintf("action %s on target already executed within %s", action.Type, dedupeWindow)
	}

	// 2. Check system load for non-recommendation actions.
	if action.Type != string(ActionLogRecommendation) {
		overloaded, err := g.isSystemOverloaded(ctx)
		if err != nil {
			g.logger.Warn("guardrail_load_check_failed", zap.Error(err))
			return false, "failed to check system load"
		}
		if overloaded {
			return false, fmt.Sprintf("system backlog exceeds threshold (%d)", backlogThreshold)
		}
	}

	// 3. For retry actions, check the job hasn't been retried too many times recently.
	if action.Type == string(ActionRetryJob) {
		jobID, _ := action.Params["job_id"].(string)
		if jobID != "" {
			tooMany, err := g.tooManyRecentRetries(ctx, jobID)
			if err != nil {
				g.logger.Warn("guardrail_retry_check_failed", zap.Error(err))
				return false, "failed to check retry count"
			}
			if tooMany {
				return false, fmt.Sprintf("job %s already retried %d+ times recently", jobID, maxRecentRetries)
			}
		}
	}

	return true, ""
}

// RecordExecution marks that an action was executed so future dedup checks work.
func (g *Guardrails) RecordExecution(action Action) {
	key := actionDedupeKey(action)
	g.mu.Lock()
	g.recentExecs[key] = time.Now().UTC()
	g.mu.Unlock()
}

// isRecentDuplicate checks if the same action target was executed within the
// dedupe window.
func (g *Guardrails) isRecentDuplicate(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Prune expired entries.
	now := time.Now().UTC()
	for k, t := range g.recentExecs {
		if now.Sub(t) > dedupeWindow {
			delete(g.recentExecs, k)
		}
	}

	lastExec, exists := g.recentExecs[key]
	if !exists {
		return false
	}
	return now.Sub(lastExec) < dedupeWindow
}

// isSystemOverloaded checks whether the queue backlog exceeds the threshold.
func (g *Guardrails) isSystemOverloaded(ctx context.Context) (bool, error) {
	const query = `SELECT COUNT(*) FROM processing_jobs WHERE status = 'queued'`
	var count int64
	if err := g.db.QueryRow(ctx, query).Scan(&count); err != nil {
		return false, fmt.Errorf("check queue backlog: %w", err)
	}
	return count > backlogThreshold, nil
}

// tooManyRecentRetries checks if a job has been retried too many times
// in the recent window by looking at audit events.
func (g *Guardrails) tooManyRecentRetries(ctx context.Context, jobID string) (bool, error) {
	since := time.Now().UTC().Add(-dedupeWindow)
	const query = `
		SELECT COUNT(*) FROM audit_events
		WHERE entity_id = $1
		  AND event_type = 'job.retried'
		  AND occurred_at > $2`
	var count int64
	if err := g.db.QueryRow(ctx, query, jobID, since).Scan(&count); err != nil {
		return false, fmt.Errorf("check recent retries: %w", err)
	}
	return count >= int64(maxRecentRetries), nil
}

// actionDedupeKey produces a stable key for deduplication based on action type
// and its primary target parameter.
func actionDedupeKey(a Action) string {
	switch a.Type {
	case string(ActionRetryJob):
		return "retry:" + fmt.Sprint(a.Params["job_id"])
	case string(ActionTriggerResync):
		return "resync:" + fmt.Sprint(a.Params["source_task_id"])
	default:
		return a.Type + ":" + a.ID
	}
}
