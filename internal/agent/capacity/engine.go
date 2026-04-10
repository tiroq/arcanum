package capacity

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// DerivedStateProvider retrieves derived signal state (owner_load_score, etc.).
// Defined here to avoid import cycles — implemented by signals.Engine.
type DerivedStateProvider interface {
	GetDerivedState(ctx context.Context) map[string]float64
}

// Engine orchestrates capacity computation, evaluation, and audit.
type Engine struct {
	store   *Store
	family  FamilyConfig
	derived DerivedStateProvider
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewEngine creates a new capacity engine.
func NewEngine(store *Store, family FamilyConfig, auditor audit.AuditRecorder, logger *zap.Logger) *Engine {
	return &Engine{
		store:   store,
		family:  family,
		auditor: auditor,
		logger:  logger,
	}
}

// WithDerivedState sets the derived state provider (signals engine).
func (e *Engine) WithDerivedState(d DerivedStateProvider) *Engine {
	e.derived = d
	return e
}

// RecomputeState recomputes the current capacity state from family config and signals.
func (e *Engine) RecomputeState(ctx context.Context) (CapacityState, error) {
	// Get owner load from signals (fail-open: 0 if unavailable).
	ownerLoad := 0.0
	if e.derived != nil {
		derived := e.derived.GetDerivedState(ctx)
		if v, ok := derived["owner_load_score"]; ok {
			ownerLoad = v
		}
	}

	blockedHours := ComputeBlockedHours(e.family.BlockedRanges)

	available := ComputeAvailableCapacity(
		e.family.MaxDailyWorkHours,
		blockedHours,
		ownerLoad,
	)

	// Weekly estimate: simple 5-day projection.
	availableWeek := available * 5.0

	state := CapacityState{
		AvailableHoursToday: available,
		AvailableHoursWeek:  availableWeek,
		BlockedHoursToday:   blockedHours,
		OwnerLoadScore:      ownerLoad,
		MaxDailyWorkHours:   e.family.MaxDailyWorkHours,
		MinFamilyTimeHours:  e.family.MinFamilyTimeHours,
	}

	// Persist.
	saved, err := e.store.UpsertState(ctx, state)
	if err != nil {
		e.logger.Warn("capacity_state_persist_failed", zap.Error(err))
		// Fail-open: return computed state even if persist fails.
		state.UpdatedAt = time.Now().UTC()
		return state, nil
	}

	// Audit.
	e.emitAudit(ctx, "capacity.state_computed", map[string]any{
		"available_hours_today": saved.AvailableHoursToday,
		"available_hours_week":  saved.AvailableHoursWeek,
		"blocked_hours_today":   saved.BlockedHoursToday,
		"owner_load_score":      saved.OwnerLoadScore,
		"max_daily_work_hours":  saved.MaxDailyWorkHours,
		"min_family_time_hours": saved.MinFamilyTimeHours,
	})

	return saved, nil
}

// GetState returns the current capacity state (from DB or zero).
func (e *Engine) GetState(ctx context.Context) (CapacityState, error) {
	return e.store.GetState(ctx)
}

// EvaluateItems evaluates a list of items against current capacity.
// Returns decisions sorted by capacity_fit_score descending.
func (e *Engine) EvaluateItems(ctx context.Context, items []CapacityItem) ([]CapacityDecision, CapacitySummary, error) {
	state, err := e.store.GetState(ctx)
	if err != nil {
		// Fail-open: use default capacity.
		state = CapacityState{
			AvailableHoursToday: e.family.MaxDailyWorkHours,
			MaxDailyWorkHours:   e.family.MaxDailyWorkHours,
		}
	}
	// If state is zero (never computed), recompute.
	if state.AvailableHoursToday == 0 && state.MaxDailyWorkHours == 0 {
		state, _ = e.RecomputeState(ctx)
	}

	decisions := make([]CapacityDecision, 0, len(items))
	now := time.Now().UTC()
	totalHours := 0.0
	recommended := 0
	deferred := 0

	for _, item := range items {
		d := EvaluateItem(item, state)
		d.ID = uuid.New().String()
		d.CreatedAt = now

		if d.Recommended {
			recommended++
		} else {
			deferred++
		}
		totalHours += item.EstimatedEffort

		decisions = append(decisions, d)

		// Audit individual evaluation.
		e.emitAudit(ctx, "capacity.item_evaluated", map[string]any{
			"item_type":        d.ItemType,
			"item_id":          d.ItemID,
			"value_per_hour":   d.ValuePerHour,
			"capacity_fit":     d.CapacityFitScore,
			"recommended":      d.Recommended,
			"defer_reason":     d.DeferReason,
			"estimated_effort": d.EstimatedEffort,
		})
	}

	// Sort by fit score descending.
	sort.Slice(decisions, func(i, j int) bool {
		return decisions[i].CapacityFitScore > decisions[j].CapacityFitScore
	})

	summary := CapacitySummary{
		TotalItemsEvaluated: len(items),
		RecommendedCount:    recommended,
		DeferredCount:       deferred,
		TotalEstimatedHours: totalHours,
		UpdatedAt:           now,
	}

	// Persist decisions.
	if err := e.store.SaveDecisions(ctx, decisions); err != nil {
		e.logger.Warn("capacity_decisions_persist_failed", zap.Error(err))
	}

	// Audit summary.
	e.emitAudit(ctx, "capacity.recommendation_generated", map[string]any{
		"total_evaluated": summary.TotalItemsEvaluated,
		"recommended":     summary.RecommendedCount,
		"deferred":        summary.DeferredCount,
		"total_hours":     summary.TotalEstimatedHours,
	})

	return decisions, summary, nil
}

// GetRecommendations returns the most recent capacity decisions.
func (e *Engine) GetRecommendations(ctx context.Context, limit int) ([]CapacityDecision, error) {
	return e.store.ListRecentDecisions(ctx, limit)
}

func (e *Engine) emitAudit(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "capacity", uuid.Nil, eventType, "system", "capacity_engine", payload)
}
