package stability

import (
	"context"
)

// SchedulerAdapter implements scheduler.StabilityProvider so the stability
// layer can be plugged into the scheduler without import cycles.
type SchedulerAdapter struct {
	engine *Engine
}

// NewSchedulerAdapter creates a SchedulerAdapter.
func NewSchedulerAdapter(engine *Engine) *SchedulerAdapter {
	return &SchedulerAdapter{engine: engine}
}

// GetThrottleMultiplier returns the current throttle multiplier from the
// persisted stability state.
func (a *SchedulerAdapter) GetThrottleMultiplier(ctx context.Context) float64 {
	st, err := a.engine.GetState(ctx)
	if err != nil {
		return 1.0 // fail-safe: no throttle on error
	}
	return st.ThrottleMultiplier
}

// RecordCycleResult forwards cycle success/failure to the stability engine.
func (a *SchedulerAdapter) RecordCycleResult(err error) {
	a.engine.RecordCycleResult(err)
}

// RunEvaluation triggers one stability evaluation pass (best-effort).
func (a *SchedulerAdapter) RunEvaluation(ctx context.Context) {
	if _, _, err := a.engine.Evaluate(ctx); err != nil {
		// Best-effort — logged inside engine.
		_ = err
	}
}
