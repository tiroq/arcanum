package stability

import (
	"context"
	"fmt"
)

// GuardrailAdapter implements actions.StabilityChecker so the stability
// layer can be plugged into guardrails without import cycles.
type GuardrailAdapter struct {
	store *Store
}

// NewGuardrailAdapter creates a GuardrailAdapter from a stability store.
func NewGuardrailAdapter(store *Store) *GuardrailAdapter {
	return &GuardrailAdapter{store: store}
}

// IsActionBlocked checks the current stability state and returns whether
// the given action type is blocked.
func (a *GuardrailAdapter) IsActionBlocked(ctx context.Context, actionType string) (bool, string) {
	st, err := a.store.Get(ctx)
	if err != nil {
		// On error, do not block — fail open.
		return false, ""
	}

	// In safe_mode, only log_recommendation and noop are allowed.
	if st.Mode == ModeSafeMode {
		if actionType != "noop" && actionType != "log_recommendation" {
			return true, fmt.Sprintf("stability: safe_mode active — action %q blocked (%s)", actionType, st.Reason)
		}
	}

	// Check explicit blocklist.
	if st.IsActionBlocked(actionType) {
		return true, fmt.Sprintf("stability: action %q blocked by stability layer (%s)", actionType, st.Reason)
	}

	return false, ""
}
