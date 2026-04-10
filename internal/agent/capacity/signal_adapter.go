package capacity

import (
	"context"
)

// SignalDerivedAdapter wraps a signals.Engine to implement DerivedStateProvider.
// Avoids importing the signals package directly.
type SignalDerivedAdapter struct {
	getter func(ctx context.Context) map[string]float64
}

// NewSignalDerivedAdapter creates an adapter from a function that returns derived state.
func NewSignalDerivedAdapter(getter func(ctx context.Context) map[string]float64) *SignalDerivedAdapter {
	return &SignalDerivedAdapter{getter: getter}
}

// GetDerivedState returns the current derived state map.
func (a *SignalDerivedAdapter) GetDerivedState(ctx context.Context) map[string]float64 {
	if a == nil || a.getter == nil {
		return nil
	}
	return a.getter(ctx)
}
