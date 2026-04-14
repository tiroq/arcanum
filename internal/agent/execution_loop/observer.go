package executionloop

import (
	"context"

	"go.uber.org/zap"
)

// Observer records execution observations and detects patterns
// such as repeated failures and no-progress conditions.
type Observer struct {
	store  ObservationStoreInterface
	logger *zap.Logger
}

// NewObserver creates a new Observer.
func NewObserver(store ObservationStoreInterface, logger *zap.Logger) *Observer {
	return &Observer{store: store, logger: logger}
}

// Record persists an observation.
func (o *Observer) Record(ctx context.Context, obs ExecutionObservation) error {
	return o.store.Insert(ctx, obs)
}

// ShouldAbort returns true if the task should be aborted due to consecutive failures.
func (o *Observer) ShouldAbort(ctx context.Context, taskID string) (bool, string) {
	count, err := o.store.CountConsecutiveFailures(ctx, taskID)
	if err != nil {
		o.logger.Warn("failed to count consecutive failures", zap.String("task_id", taskID), zap.Error(err))
		return false, ""
	}
	if count >= MaxConsecFailures {
		return true, "consecutive failures exceeded threshold"
	}
	return false, ""
}

// IsStepBlocked returns true if a step has failed with the same error twice in a row.
func (o *Observer) IsStepBlocked(ctx context.Context, taskID, stepID string) bool {
	obs, err := o.store.ListByTask(ctx, taskID)
	if err != nil {
		return false
	}

	var lastErr string
	sameCount := 0
	for _, ob := range obs {
		if ob.StepID != stepID {
			continue
		}
		if !ob.Success {
			if ob.Error == lastErr && lastErr != "" {
				sameCount++
			} else {
				sameCount = 1
			}
			lastErr = ob.Error
		}
	}
	return sameCount >= 2
}

// DetectNoProgress returns true if the last N observations show no success.
func (o *Observer) DetectNoProgress(ctx context.Context, taskID string, window int) bool {
	obs, err := o.store.ListByTask(ctx, taskID)
	if err != nil || len(obs) < window {
		return false
	}

	tail := obs[len(obs)-window:]
	for _, ob := range tail {
		if ob.Success {
			return false
		}
	}
	return true
}
