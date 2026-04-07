package planning

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// ContextCollector gathers all inputs the planner needs in a single pass.
// Every query is read-only.
type ContextCollector struct {
	db          *pgxpool.Pool
	memoryStore *actionmemory.Store
	logger      *zap.Logger
}

// NewContextCollector creates a ContextCollector.
func NewContextCollector(db *pgxpool.Pool, memoryStore *actionmemory.Store, logger *zap.Logger) *ContextCollector {
	return &ContextCollector{
		db:          db,
		memoryStore: memoryStore,
		logger:      logger,
	}
}

// Collect builds a PlanningContext from current DB state and action memory.
func (cc *ContextCollector) Collect(ctx context.Context) (PlanningContext, error) {
	pctx := PlanningContext{
		RecentActionFeedback: make(map[string]actionmemory.ActionFeedback),
		Timestamp:            time.Now().UTC(),
	}

	if err := cc.loadQueueState(ctx, &pctx); err != nil {
		return pctx, fmt.Errorf("collect queue state: %w", err)
	}

	if err := cc.loadRates(ctx, &pctx); err != nil {
		return pctx, fmt.Errorf("collect rates: %w", err)
	}

	if err := cc.loadActionFeedback(ctx, &pctx); err != nil {
		return pctx, fmt.Errorf("collect action feedback: %w", err)
	}

	return pctx, nil
}

func (cc *ContextCollector) loadQueueState(ctx context.Context, pctx *PlanningContext) error {
	const q = `
		SELECT
			COALESCE(SUM(CASE WHEN status = 'queued' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'retry_scheduled' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'leased' THEN 1 ELSE 0 END), 0)
		FROM processing_jobs
		WHERE status IN ('queued', 'retry_scheduled', 'leased')`

	var queued, retry, leased int
	if err := cc.db.QueryRow(ctx, q).Scan(&queued, &retry, &leased); err != nil {
		return fmt.Errorf("queue state: %w", err)
	}
	pctx.QueueBacklog = queued
	pctx.RetryScheduledCount = retry
	pctx.LeasedCount = leased
	return nil
}

func (cc *ContextCollector) loadRates(ctx context.Context, pctx *PlanningContext) error {
	since := time.Now().UTC().Add(-24 * time.Hour)

	// Failure rate from recent jobs.
	const jobQ = `
		SELECT
			COALESCE(COUNT(*) FILTER (WHERE status IN ('succeeded', 'failed', 'dead_letter')), 0),
			COALESCE(COUNT(*) FILTER (WHERE status IN ('failed', 'dead_letter')), 0)
		FROM processing_jobs
		WHERE updated_at >= $1`

	var total, failed int64
	if err := cc.db.QueryRow(ctx, jobQ, since).Scan(&total, &failed); err != nil {
		return fmt.Errorf("failure rate: %w", err)
	}
	if total > 0 {
		pctx.FailureRate = float64(failed) / float64(total)
	}

	// Acceptance rate from recent proposals.
	const propQ = `
		SELECT
			COALESCE(COUNT(*) FILTER (WHERE approval_status IN ('approved', 'rejected')), 0),
			COALESCE(COUNT(*) FILTER (WHERE approval_status = 'approved'), 0)
		FROM suggestion_proposals
		WHERE updated_at >= $1`

	var totalProp, accepted int64
	if err := cc.db.QueryRow(ctx, propQ, since).Scan(&totalProp, &accepted); err != nil {
		return fmt.Errorf("acceptance rate: %w", err)
	}
	if totalProp > 0 {
		pctx.AcceptanceRate = float64(accepted) / float64(totalProp)
	}

	return nil
}

func (cc *ContextCollector) loadActionFeedback(ctx context.Context, pctx *PlanningContext) error {
	records, err := cc.memoryStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list action memory: %w", err)
	}

	for i := range records {
		fb := actionmemory.GenerateFeedback(&records[i])
		pctx.RecentActionFeedback[records[i].ActionType] = fb
	}

	return nil
}
