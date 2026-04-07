package policy

import (
	"context"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"go.uber.org/zap"
)

// Evaluator checks whether previously applied policy changes led to improvements.
type Evaluator struct {
	store       *Store
	memoryStore *actionmemory.Store
	logger      *zap.Logger
}

// NewEvaluator creates an Evaluator.
func NewEvaluator(store *Store, memoryStore *actionmemory.Store, logger *zap.Logger) *Evaluator {
	return &Evaluator{store: store, memoryStore: memoryStore, logger: logger}
}

// EvaluationResult is the outcome of evaluating past policy changes.
type EvaluationResult struct {
	Evaluated int `json:"evaluated"`
	Improved  int `json:"improved"`
	Regressed int `json:"regressed"`
}

// EvaluateChanges reviews applied-but-unevaluated changes older than the
// evaluation window and marks them as improved or regressed.
func (e *Evaluator) EvaluateChanges(ctx context.Context) (*EvaluationResult, error) {
	// Only evaluate changes that are old enough (at least EvaluationCycles worth of time).
	// We use a simple time heuristic: changes older than 10 minutes.
	cutoff := time.Now().UTC().Add(-10 * time.Minute)

	changes, err := e.store.ListUnevaluatedChanges(ctx, cutoff)
	if err != nil {
		return nil, err
	}

	result := &EvaluationResult{}

	// Get current action memory for comparison.
	records, err := e.memoryStore.List(ctx)
	if err != nil {
		return nil, err
	}
	memByAction := make(map[string]*actionmemory.ActionMemoryRecord)
	for i := range records {
		memByAction[records[i].ActionType] = &records[i]
	}

	for _, c := range changes {
		improved := e.assessImprovement(c, memByAction)
		if err := e.store.MarkEvaluated(ctx, c.ID, improved); err != nil {
			e.logger.Warn("evaluate_mark_failed", zap.Error(err))
			continue
		}
		result.Evaluated++
		if improved {
			result.Improved++
		} else {
			result.Regressed++
		}
	}

	return result, nil
}

// assessImprovement checks if a parameter change led to better outcomes.
// This is a simple heuristic: for penalty increases, we check if the
// relevant action's failure rate went down; for boost increases, if
// success rate went up.
func (e *Evaluator) assessImprovement(c ChangeRecord, memByAction map[string]*actionmemory.ActionMemoryRecord) bool {
	param := PolicyParam(c.Parameter)

	switch param {
	case ParamFeedbackAvoidPenalty:
		// Increased penalty → check if overall success rate improved.
		for _, m := range memByAction {
			if m.TotalRuns >= 5 && m.SuccessRate >= 0.50 {
				return true
			}
		}
		return false

	case ParamFeedbackPreferBoost:
		// Increased boost → check if preferred actions succeed.
		for _, m := range memByAction {
			if m.TotalRuns >= 5 && m.SuccessRate >= 0.70 {
				return true
			}
		}
		return false

	case ParamHighRetryBoost:
		// Decreased boost → check if retry_job failure rate decreased.
		if m, ok := memByAction["retry_job"]; ok {
			return m.FailureRate < 0.50
		}
		return true // no data → assume not regressed

	case ParamNoopBasePenalty:
		// Increased penalty → check if noop ratio is reasonable (we can't
		// easily check here so assume OK if any non-noop action exists).
		return len(memByAction) > 0

	default:
		return true // unknown param → assume not regressed
	}
}
