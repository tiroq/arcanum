package scheduling

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter provides scheduling functionality to the API layer.
// Nil-safe and fail-open: returns zero values if engine is not available.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a new scheduling graph adapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// GetEngine returns the underlying engine for direct access (e.g., from handlers).
func (a *GraphAdapter) GetEngine() *Engine {
	if a == nil {
		return nil
	}
	return a.engine
}

// RecomputeSlots triggers slot regeneration.
func (a *GraphAdapter) RecomputeSlots(ctx context.Context) ([]ScheduleSlot, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.RecomputeSlots(ctx)
}

// AddCandidate creates a scheduling candidate.
func (a *GraphAdapter) AddCandidate(ctx context.Context, c SchedulingCandidate) (SchedulingCandidate, error) {
	if a == nil || a.engine == nil {
		return SchedulingCandidate{}, nil
	}
	return a.engine.AddCandidate(ctx, c)
}

// Recommend produces a scheduling recommendation for a candidate.
func (a *GraphAdapter) Recommend(ctx context.Context, candidateID string) (ScheduleRecommendation, error) {
	if a == nil || a.engine == nil {
		return ScheduleRecommendation{NoValidSlots: true}, nil
	}
	return a.engine.Recommend(ctx, candidateID)
}

// ApproveDecision transitions a decision from proposed → approved.
func (a *GraphAdapter) ApproveDecision(ctx context.Context, decisionID string) (ScheduleDecision, error) {
	if a == nil || a.engine == nil {
		return ScheduleDecision{}, nil
	}
	return a.engine.ApproveDecision(ctx, decisionID)
}

// WriteCalendar creates a calendar event for an approved decision.
func (a *GraphAdapter) WriteCalendar(ctx context.Context, decisionID string, dryRun bool) (CalendarRecord, error) {
	if a == nil || a.engine == nil {
		return CalendarRecord{}, nil
	}
	return a.engine.WriteCalendar(ctx, decisionID, dryRun)
}

// ListSlots returns slots for today + configured range.
func (a *GraphAdapter) ListSlots(ctx context.Context) ([]ScheduleSlot, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.RecomputeSlots(ctx)
}

// ListCandidates returns recent scheduling candidates.
func (a *GraphAdapter) ListCandidates(ctx context.Context, limit int) ([]SchedulingCandidate, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListCandidates(ctx, limit)
}

// ListDecisions returns recent schedule decisions.
func (a *GraphAdapter) ListDecisions(ctx context.Context, limit int) ([]ScheduleDecision, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListDecisions(ctx, limit)
}
