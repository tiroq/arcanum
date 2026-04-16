package objective

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// --- Provider interfaces (local, avoids import cycles) ---

// FinancialTruthProvider reads verified financial data.
type FinancialTruthProvider interface {
	GetVerifiedIncome(ctx context.Context) float64
	GetTargetIncome(ctx context.Context) float64
}

// FinancialPressureProvider reads current financial pressure.
type FinancialPressureProvider interface {
	GetPressure(ctx context.Context) (pressureScore float64, urgencyLevel string)
}

// CapacityProvider reads current owner capacity.
type CapacityProvider interface {
	GetOwnerLoadScore(ctx context.Context) float64
	GetAvailableHoursToday(ctx context.Context) float64
	GetAvailableHoursWeek(ctx context.Context) float64
	GetMaxDailyWorkHours(ctx context.Context) float64
	GetBlockedHoursToday(ctx context.Context) float64
	GetMinFamilyTimeHours(ctx context.Context) float64
}

// PortfolioProvider reads portfolio-level metrics.
type PortfolioProvider interface {
	GetDiversificationIndex(ctx context.Context) float64
	GetDominantAllocation(ctx context.Context) float64
	GetActiveStrategyCount(ctx context.Context) int
	GetPortfolioROI(ctx context.Context) float64
}

// IncomeProvider reads income pipeline state.
type IncomeProvider interface {
	GetBestOpenScore(ctx context.Context) float64
	GetOpenOpportunityCount(ctx context.Context) int
}

// PricingProvider reads pricing confidence data.
type PricingProvider interface {
	GetPricingConfidence(ctx context.Context) float64
	GetWinRate(ctx context.Context) float64
}

// ExternalActionsProvider reads external action execution stats.
type ExternalActionsProvider interface {
	GetActionCounts(ctx context.Context) (failed, pending, total int)
}

// ExecutionMetricsProvider reads closed-loop execution feedback (Iteration 55A).
type ExecutionMetricsProvider interface {
	GetExecMetrics(ctx context.Context) (successRate float64, repeatedFailures, abortedCount, blockedCount, totalExecutions int)
}

// Engine orchestrates the global objective function: gathers inputs, computes
// utility + risk + net utility, persists results, and emits audit events.
type Engine struct {
	objStore     *ObjectiveStateStore
	riskStore    *RiskStateStore
	summaryStore *SummaryStore
	auditor      audit.AuditRecorder
	logger       *zap.Logger

	truth           FinancialTruthProvider
	pressure        FinancialPressureProvider
	capacity        CapacityProvider
	portfolio       PortfolioProvider
	income          IncomeProvider
	pricing         PricingProvider
	externalActions ExternalActionsProvider
	execMetrics     ExecutionMetricsProvider
}

// NewEngine creates a new objective function engine.
func NewEngine(
	objStore *ObjectiveStateStore,
	riskStore *RiskStateStore,
	summaryStore *SummaryStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		objStore:     objStore,
		riskStore:    riskStore,
		summaryStore: summaryStore,
		auditor:      auditor,
		logger:       logger,
	}
}

// WithTruth sets the financial truth provider.
func (e *Engine) WithTruth(p FinancialTruthProvider) *Engine {
	e.truth = p
	return e
}

// WithPressure sets the financial pressure provider.
func (e *Engine) WithPressure(p FinancialPressureProvider) *Engine {
	e.pressure = p
	return e
}

// WithCapacity sets the capacity provider.
func (e *Engine) WithCapacity(p CapacityProvider) *Engine {
	e.capacity = p
	return e
}

// WithPortfolio sets the portfolio provider.
func (e *Engine) WithPortfolio(p PortfolioProvider) *Engine {
	e.portfolio = p
	return e
}

// WithIncome sets the income provider.
func (e *Engine) WithIncome(p IncomeProvider) *Engine {
	e.income = p
	return e
}

// WithPricing sets the pricing provider.
func (e *Engine) WithPricing(p PricingProvider) *Engine {
	e.pricing = p
	return e
}

// WithExternalActions sets the external actions provider.
func (e *Engine) WithExternalActions(p ExternalActionsProvider) *Engine {
	e.externalActions = p
	return e
}

// WithExecutionMetrics sets the closed-loop execution metrics provider (Iteration 55A).
func (e *Engine) WithExecutionMetrics(p ExecutionMetricsProvider) *Engine {
	e.execMetrics = p
	return e
}

// GatherInputs collects all objective inputs from subsystem providers.
// Fail-open: returns zero for any unavailable provider.
func (e *Engine) GatherInputs(ctx context.Context) ObjectiveInputs {
	var inputs ObjectiveInputs

	if e.truth != nil {
		inputs.VerifiedMonthlyIncome = e.truth.GetVerifiedIncome(ctx)
		inputs.TargetMonthlyIncome = e.truth.GetTargetIncome(ctx)
	}

	if e.pressure != nil {
		inputs.PressureScore, inputs.UrgencyLevel = e.pressure.GetPressure(ctx)
	}

	if e.capacity != nil {
		inputs.OwnerLoadScore = e.capacity.GetOwnerLoadScore(ctx)
		inputs.AvailableHoursToday = e.capacity.GetAvailableHoursToday(ctx)
		inputs.AvailableHoursWeek = e.capacity.GetAvailableHoursWeek(ctx)
		inputs.MaxDailyWorkHours = e.capacity.GetMaxDailyWorkHours(ctx)
		inputs.BlockedHoursToday = e.capacity.GetBlockedHoursToday(ctx)
		inputs.MinFamilyTimeHours = e.capacity.GetMinFamilyTimeHours(ctx)
	}

	if e.portfolio != nil {
		inputs.DiversificationIndex = e.portfolio.GetDiversificationIndex(ctx)
		inputs.DominantAllocation = e.portfolio.GetDominantAllocation(ctx)
		inputs.ActiveStrategies = e.portfolio.GetActiveStrategyCount(ctx)
		inputs.PortfolioROI = e.portfolio.GetPortfolioROI(ctx)
	}

	if e.income != nil {
		inputs.BestOpenOppScore = e.income.GetBestOpenScore(ctx)
		inputs.OpenOpportunityCount = e.income.GetOpenOpportunityCount(ctx)
	}

	if e.pricing != nil {
		inputs.PricingConfidence = e.pricing.GetPricingConfidence(ctx)
		inputs.WinRate = e.pricing.GetWinRate(ctx)
	}

	if e.externalActions != nil {
		inputs.FailedActionCount, inputs.PendingActionCount, inputs.TotalActionCount = e.externalActions.GetActionCounts(ctx)
	}

	if e.execMetrics != nil {
		inputs.ExecFeedbackSuccessRate,
			inputs.ExecFeedbackRepeatedFailures,
			inputs.ExecFeedbackAbortedCount,
			inputs.ExecFeedbackBlockedCount,
			inputs.ExecFeedbackTotalExecutions = e.execMetrics.GetExecMetrics(ctx)
	}

	return inputs
}

// Recompute gathers inputs, runs the full objective pipeline,
// persists results, and emits audit events.
func (e *Engine) Recompute(ctx context.Context) (ObjectiveSummary, error) {
	inputs := e.GatherInputs(ctx)
	now := time.Now().UTC()

	objState, riskState, summary := ComputeFromInputs(inputs)
	objState.ComputedAt = now
	riskState.ComputedAt = now
	summary.UpdatedAt = now

	// Persist all three states.
	if err := e.objStore.Upsert(ctx, objState); err != nil {
		return ObjectiveSummary{}, err
	}
	if err := e.riskStore.Upsert(ctx, riskState); err != nil {
		return ObjectiveSummary{}, err
	}
	if err := e.summaryStore.Upsert(ctx, summary); err != nil {
		return ObjectiveSummary{}, err
	}

	// Emit audit events.
	e.auditEvent(ctx, "objective.recomputed", map[string]any{
		"utility_score":            summary.UtilityScore,
		"risk_score":               summary.RiskScore,
		"net_utility":              summary.NetUtility,
		"dominant_positive_factor": summary.DominantPositiveFactor,
		"dominant_risk_factor":     summary.DominantRiskFactor,
		"verified_income_score":    objState.VerifiedIncomeScore,
		"family_stability_score":   objState.FamilyStabilityScore,
		"owner_relief_score":       objState.OwnerReliefScore,
		"execution_readiness":      objState.ExecutionReadinessScore,
		"strategy_quality":         objState.StrategyQualityScore,
	})
	e.auditEvent(ctx, "risk.recomputed", map[string]any{
		"financial_risk":     riskState.FinancialInstabilityRisk,
		"overload_risk":      riskState.OverloadRisk,
		"execution_risk":     riskState.ExecutionRisk,
		"concentration_risk": riskState.StrategyConcentrationRisk,
		"pricing_risk":       riskState.PricingConfidenceRisk,
		"risk_score":         summary.RiskScore,
	})

	return summary, nil
}

// GetObjectiveState returns the last computed objective state.
func (e *Engine) GetObjectiveState(ctx context.Context) (ObjectiveState, error) {
	return e.objStore.Get(ctx)
}

// GetRiskState returns the last computed risk state.
func (e *Engine) GetRiskState(ctx context.Context) (RiskState, error) {
	return e.riskStore.Get(ctx)
}

// GetSummary returns the last computed objective summary.
func (e *Engine) GetSummary(ctx context.Context) (ObjectiveSummary, error) {
	return e.summaryStore.Get(ctx)
}

// GetObjectiveSignal returns the current planner-facing signal.
// Fail-open: returns zero signal if no summary is available.
func (e *Engine) GetObjectiveSignal(ctx context.Context) (ObjectiveSignal, error) {
	summary, err := e.summaryStore.Get(ctx)
	if err != nil {
		return ObjectiveSignal{}, err
	}
	if summary.UpdatedAt.IsZero() {
		return ObjectiveSignal{}, nil
	}
	return ComputeObjectiveSignal(summary.NetUtility, summary.DominantPositiveFactor, summary.DominantRiskFactor), nil
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	if err := e.auditor.RecordEvent(ctx, "objective", uuid.Nil, eventType, "system", "objective_engine", payload); err != nil {
		e.logger.Warn("audit event failed",
			zap.String("event", eventType),
			zap.Error(err),
		)
	}
}
