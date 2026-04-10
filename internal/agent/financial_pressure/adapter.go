package financialpressure

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// GraphAdapter implements decision_graph.FinancialPressureProvider using the store.
// Fail-open: returns zero pressure when store is nil or DB unavailable.
type GraphAdapter struct {
	store   *Store
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter backed by the given store.
func NewGraphAdapter(store *Store, auditor audit.AuditRecorder, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{store: store, auditor: auditor, logger: logger}
}

// GetPressure returns the current financial pressure score and urgency level.
// Fail-open: returns (0, "low") if store is nil or state unavailable.
func (a *GraphAdapter) GetPressure(ctx context.Context) (pressureScore float64, urgencyLevel string) {
	if a == nil || a.store == nil {
		return 0, UrgencyLow
	}
	state, err := a.store.Get(ctx)
	if err != nil || state.ID == "" {
		return 0, UrgencyLow
	}
	p := ComputePressure(state)
	return p.PressureScore, p.UrgencyLevel
}

// IsIncomeRelated delegates to the income action check.
// This is used by the planner adapter to decide whether a path should receive
// the pressure boost. We check the same set of income-related actions.
func (a *GraphAdapter) IsIncomeRelated(actionType string) bool {
	return isIncomeAction(actionType)
}

// UpdateState persists a new financial state and emits an audit event.
func (a *GraphAdapter) UpdateState(ctx context.Context, state FinancialState) (FinancialState, FinancialPressure, error) {
	saved, err := a.store.Upsert(ctx, state)
	if err != nil {
		return FinancialState{}, FinancialPressure{}, err
	}

	pressure := ComputePressure(saved)

	if a.auditor != nil {
		a.auditor.RecordEvent(ctx, "financial_state", uuid.Nil, //nolint:errcheck
			"financial.state_updated", "financial_pressure", "system", map[string]any{
				"current_income_month": saved.CurrentIncomeMonth,
				"target_income_month":  saved.TargetIncomeMonth,
				"monthly_expenses":     saved.MonthlyExpenses,
				"cash_buffer":          saved.CashBuffer,
			})

		a.auditor.RecordEvent(ctx, "financial_pressure", uuid.Nil, //nolint:errcheck
			"financial.pressure_computed", "financial_pressure", "system", map[string]any{
				"pressure_score": pressure.PressureScore,
				"urgency_level":  pressure.UrgencyLevel,
				"income_gap":     pressure.IncomeGap,
				"buffer_ratio":   pressure.BufferRatio,
			})
	}

	a.logger.Info("financial state updated",
		zap.Float64("pressure_score", pressure.PressureScore),
		zap.String("urgency_level", pressure.UrgencyLevel),
		zap.Float64("income_gap", pressure.IncomeGap),
	)

	return saved, pressure, nil
}

// GetState retrieves the current financial state and computed pressure.
func (a *GraphAdapter) GetState(ctx context.Context) (FinancialState, FinancialPressure, error) {
	if a == nil || a.store == nil {
		return FinancialState{}, FinancialPressure{}, nil
	}
	state, err := a.store.Get(ctx)
	if err != nil {
		return FinancialState{}, FinancialPressure{}, err
	}
	if state.ID == "" {
		return FinancialState{}, FinancialPressure{}, nil
	}
	return state, ComputePressure(state), nil
}

// incomeActionTypes mirrors the set from income package to avoid import cycles.
var incomeActionTypes = map[string]bool{
	"propose_income_action": true,
	"analyze_opportunity":   true,
	"schedule_work":         true,
}

func isIncomeAction(actionType string) bool {
	return incomeActionTypes[actionType]
}
