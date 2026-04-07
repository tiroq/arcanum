package exploration

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/audit"
)

// StabilityMode mirrors stability.Mode to avoid import cycles.
type StabilityMode string

const (
	StabilityNormal    StabilityMode = "normal"
	StabilityThrottled StabilityMode = "throttled"
	StabilitySafeMode  StabilityMode = "safe_mode"
)

// StabilityProvider reads current stability state.
// Implemented by the stability adapter to avoid import cycles.
type StabilityProvider interface {
	GetMode(ctx context.Context) StabilityMode
}

// Engine orchestrates bounded exploration decisions.
// It is strictly deterministic: same inputs always produce the same output.
type Engine struct {
	budgetStore *BudgetStore
	stability   StabilityProvider
	auditor     audit.AuditRecorder
	logger      *zap.Logger

	// lastDecision holds the most recent ExplorationDecision for API visibility.
	lastDecision *ExplorationDecision
}

// NewEngine creates an exploration Engine.
func NewEngine(
	budgetStore *BudgetStore,
	stability StabilityProvider,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		budgetStore: budgetStore,
		stability:   stability,
		auditor:     auditor,
		logger:      logger,
	}
}

// LastDecision returns the most recent ExplorationDecision.
func (e *Engine) LastDecision() *ExplorationDecision {
	return e.lastDecision
}

// Evaluate determines whether an exploratory override is warranted for
// the given planning decision. This is the main entry point called by
// the adaptive planner after normal scoring.
//
// It returns:
//   - an ExplorationDecision (always, for observability)
//   - a replacement action type if exploration was chosen, or empty string
//
// Fail-open: if any error occurs, returns the exploitation result unchanged.
func (e *Engine) Evaluate(
	ctx context.Context,
	decision planning.PlanningDecision,
	globalFeedback map[string]actionmemory.ActionFeedback,
	now time.Time,
) ExplorationDecision {
	d := ExplorationDecision{
		CreatedAt: now,
	}

	// --- Check stability ---
	mode := StabilityNormal
	if e.stability != nil {
		mode = e.stability.GetMode(ctx)
	}
	if mode == StabilitySafeMode || mode == StabilityThrottled {
		d.Enabled = false
		d.DecisionReason = "stability_mode_" + string(mode)
		e.auditEvent(ctx, "exploration.skipped", d)
		e.lastDecision = &d
		return d
	}
	d.Enabled = true

	// --- Check triggers ---
	triggered, triggerReason := ShouldExplore(decision)
	if !triggered {
		d.DecisionReason = "no_trigger: " + triggerReason
		e.auditEvent(ctx, "exploration.considered", d)
		e.lastDecision = &d
		return d
	}

	// --- Check budget ---
	budget, err := e.loadBudget(ctx, now)
	if err != nil {
		d.DecisionReason = "budget_load_error"
		e.logger.Warn("exploration_budget_load_failed", zap.Error(err))
		e.lastDecision = &d
		return d
	}
	d.BudgetReason = budget.BudgetReason(now)

	if !budget.HasCycleBudget() || !budget.HasWindowBudget(now) {
		d.DecisionReason = "budget_exhausted"
		e.auditEvent(ctx, "exploration.budget_exhausted", d)
		e.lastDecision = &d
		return d
	}

	// --- Score exploration candidates ---
	candidates := ScoreExplorationCandidates(decision, globalFeedback)
	d.Candidates = candidates

	if len(candidates) == 0 {
		d.DecisionReason = "no_viable_candidates"
		e.auditEvent(ctx, "exploration.considered", d)
		e.lastDecision = &d
		return d
	}

	// --- Select best candidate ---
	best := candidates[0]
	if best.ExplorationScore < ExplorationScoreThreshold {
		d.DecisionReason = "score_below_threshold"
		e.auditEvent(ctx, "exploration.considered", d)
		e.lastDecision = &d
		return d
	}

	// --- Choose exploration ---
	d.Chosen = true
	d.ChosenActionType = best.ActionType
	d.DecisionReason = "exploration_chosen: " + triggerReason

	// Consume budget.
	budget.Consume(now)
	if saveErr := e.saveBudget(ctx, budget); saveErr != nil {
		e.logger.Warn("exploration_budget_save_failed", zap.Error(saveErr))
		// Do not fail — budget is best-effort durability.
	}

	e.auditEvent(ctx, "exploration.chosen", d)
	e.lastDecision = &d
	return d
}

// loadBudget loads budget from store, or returns defaults on error.
func (e *Engine) loadBudget(ctx context.Context, now time.Time) (*ExplorationBudget, error) {
	if e.budgetStore == nil {
		b := DefaultBudget()
		return &b, nil
	}
	b, err := e.budgetStore.Load(ctx)
	if err != nil {
		return nil, err
	}
	// Always reset cycle counter at start of evaluation.
	b.ResetCycle()
	return b, nil
}

// saveBudget persists the updated budget state.
func (e *Engine) saveBudget(ctx context.Context, b *ExplorationBudget) error {
	if e.budgetStore == nil {
		return nil
	}
	return e.budgetStore.Save(ctx, b)
}

// auditEvent records an exploration audit event.
func (e *Engine) auditEvent(ctx context.Context, eventType string, d ExplorationDecision) {
	if e.auditor == nil {
		return
	}

	payload := map[string]any{
		"enabled":         d.Enabled,
		"chosen":          d.Chosen,
		"budget_reason":   d.BudgetReason,
		"decision_reason": d.DecisionReason,
	}
	if d.ChosenActionType != "" {
		payload["chosen_action_type"] = d.ChosenActionType
	}
	if len(d.Candidates) > 0 {
		best := d.Candidates[0]
		payload["top_exploration_score"] = best.ExplorationScore
		payload["top_novelty_score"] = best.NoveltyScore
		payload["top_safety_score"] = best.SafetyScore
	}

	_ = e.auditor.RecordEvent(ctx, "exploration", uuid.New(), eventType,
		"system", "exploration_engine", payload)
}
