package financialtruth

import "context"

// PressureTruthAdapter implements financialpressure.FinancialTruthProvider
// to supply verified monthly income to the pressure computation.
// Nil-safe and fail-open: returns (0, false) when engine is nil.
type PressureTruthAdapter struct {
	engine *Engine
}

// NewPressureTruthAdapter creates a PressureTruthAdapter backed by the engine.
func NewPressureTruthAdapter(engine *Engine) *PressureTruthAdapter {
	return &PressureTruthAdapter{engine: engine}
}

// GetVerifiedMonthlyIncome returns the verified monthly income and whether data exists.
func (a *PressureTruthAdapter) GetVerifiedMonthlyIncome(ctx context.Context) (float64, bool) {
	if a == nil || a.engine == nil {
		return 0, false
	}
	sig := a.engine.GetTruthSignal(ctx)
	return sig.VerifiedMonthlyIncome, sig.HasVerifiedData
}
