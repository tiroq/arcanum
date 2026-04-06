package actionmemory

import (
	"context"
	"fmt"
)

// FeedbackAdapter implements actions.FeedbackProvider using the memory store.
type FeedbackAdapter struct {
	store *Store
}

// NewFeedbackAdapter creates a FeedbackAdapter.
func NewFeedbackAdapter(store *Store) *FeedbackAdapter {
	return &FeedbackAdapter{store: store}
}

// ShouldAvoid returns true if historical data indicates this action type
// should be rejected, along with a human-readable reason.
func (a *FeedbackAdapter) ShouldAvoid(ctx context.Context, actionType string) (bool, string) {
	record, err := a.store.GetByActionType(ctx, actionType)
	if err != nil || record == nil {
		return false, ""
	}

	fb := GenerateFeedback(record)
	if fb.Recommendation == RecommendAvoidAction {
		return true, fmt.Sprintf(
			"low historical success rate for %s (success=%.2f, failure=%.2f, n=%d)",
			actionType, fb.SuccessRate, fb.FailureRate, fb.SampleSize,
		)
	}

	return false, ""
}
