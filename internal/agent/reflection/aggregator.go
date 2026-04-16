package reflection

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// --- Interfaces for data sources (avoids import cycles) ---

// IncomeDataProvider reads income performance data.
type IncomeDataProvider interface {
	GetPerformanceStats(ctx context.Context) (totalOutcomes int, successRate, avgAccuracy, estimatedIncome float64)
	GetOpportunityCount(ctx context.Context) int
}

// FinancialTruthProvider reads verified financial data.
type FinancialTruthProvider interface {
	GetVerifiedIncome(ctx context.Context) float64
}

// SignalDataProvider reads signal-derived state.
type SignalDataProvider interface {
	GetDerivedState(ctx context.Context) map[string]float64
}

// CapacityDataProvider reads capacity state.
type CapacityDataProvider interface {
	GetOwnerLoadScore(ctx context.Context) float64
	GetAvailableHoursToday(ctx context.Context) float64
}

// ExternalActionsProvider reads external action data.
type ExternalActionsProvider interface {
	GetRecentActionCounts(ctx context.Context, since time.Time) map[string]int
}

// ExecutionFeedbackProvider reads structured execution feedback for reflection.
type ExecutionFeedbackProvider interface {
	GetReflectionFeedback(ctx context.Context) []ExecutionFeedbackEntry
}

// ExecutionFeedbackEntry is a single execution feedback record (local type to avoid import cycles).
type ExecutionFeedbackEntry struct {
	Signal    string
	Outcome   string
	Success   bool
	TaskID    string
	CreatedAt time.Time
}

// Aggregator collects and computes data from all sources for a time window.
type Aggregator struct {
	income            IncomeDataProvider
	financialTruth    FinancialTruthProvider
	signals           SignalDataProvider
	capacity          CapacityDataProvider
	externalActions   ExternalActionsProvider
	executionFeedback ExecutionFeedbackProvider
	logger            *zap.Logger
}

// NewAggregator creates an Aggregator with all data source providers.
func NewAggregator(logger *zap.Logger) *Aggregator {
	return &Aggregator{logger: logger}
}

// WithIncome attaches the income data provider.
func (a *Aggregator) WithIncome(p IncomeDataProvider) *Aggregator {
	a.income = p
	return a
}

// WithFinancialTruth attaches the financial truth provider.
func (a *Aggregator) WithFinancialTruth(p FinancialTruthProvider) *Aggregator {
	a.financialTruth = p
	return a
}

// WithSignals attaches the signal data provider.
func (a *Aggregator) WithSignals(p SignalDataProvider) *Aggregator {
	a.signals = p
	return a
}

// WithCapacity attaches the capacity data provider.
func (a *Aggregator) WithCapacity(p CapacityDataProvider) *Aggregator {
	a.capacity = p
	return a
}

// WithExternalActions attaches the external actions provider.
func (a *Aggregator) WithExternalActions(p ExternalActionsProvider) *Aggregator {
	a.externalActions = p
	return a
}

// WithExecutionFeedback attaches the execution feedback provider.
func (a *Aggregator) WithExecutionFeedback(p ExecutionFeedbackProvider) *Aggregator {
	a.executionFeedback = p
	return a
}

// Aggregate collects data from all sources and computes metrics for the given period.
// Fail-open: any provider failure results in zero values for that source.
func (a *Aggregator) Aggregate(ctx context.Context, periodStart, periodEnd time.Time) AggregatedData {
	data := AggregatedData{
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		SignalsSummary:     make(map[string]float64),
		ManualActionCounts: make(map[string]int),
	}

	// Income data
	if a.income != nil {
		totalOutcomes, successRate, avgAccuracy, estimatedIncome := a.income.GetPerformanceStats(ctx)
		data.ActionsCount = totalOutcomes
		data.SuccessRate = successRate
		data.AvgAccuracy = avgAccuracy
		data.IncomeEstimated = estimatedIncome
		data.OpportunitiesCount = a.income.GetOpportunityCount(ctx)
		if totalOutcomes > 0 {
			data.FailureCount = totalOutcomes - int(float64(totalOutcomes)*successRate)
		}
	}

	// Financial truth — prefer verified income over estimated
	if a.financialTruth != nil {
		verified := a.financialTruth.GetVerifiedIncome(ctx)
		data.IncomeVerified = verified
	}

	// Signals — derived state
	if a.signals != nil {
		derived := a.signals.GetDerivedState(ctx)
		for k, v := range derived {
			data.SignalsSummary[k] = v
		}
	}

	// Capacity
	if a.capacity != nil {
		data.OwnerLoadScore = a.capacity.GetOwnerLoadScore(ctx)
		avail := a.capacity.GetAvailableHoursToday(ctx)
		data.TotalEffortHours = avail
	}

	// External actions — count manual/repeated actions
	if a.externalActions != nil {
		counts := a.externalActions.GetRecentActionCounts(ctx, periodStart)
		data.ManualActionCounts = counts
	}

	// Compute value per hour
	income := data.IncomeVerified
	if income == 0 {
		income = data.IncomeEstimated
	}
	if data.TotalEffortHours > 0 {
		data.ValuePerHour = income / data.TotalEffortHours
	}

	// Execution feedback (Iteration 55A)
	if a.executionFeedback != nil {
		entries := a.executionFeedback.GetReflectionFeedback(ctx)
		summary := ReflectionExecutionSummary{}
		for _, e := range entries {
			if e.CreatedAt.Before(periodStart) {
				continue // outside window
			}
			summary.TotalCount++
			switch e.Signal {
			case "safe_action_succeeded":
				summary.SafeSuccessCount++
				summary.SuccessCount++
			case "repeated_failure":
				summary.RepeatedFailureCount++
				summary.FailureCount++
			case "execution_failure":
				summary.FailureCount++
			case "blocked_by_review":
				summary.BlockedByReviewCount++
			case "blocked_by_governance":
				summary.BlockedByGovernanceCount++
			case "objective_penalty_abort":
				summary.ObjectiveAbortCount++
			case "execution_aborted":
				summary.FailureCount++
			default:
				if e.Success {
					summary.SuccessCount++
				} else {
					summary.FailureCount++
				}
			}
		}
		data.ExecutionFeedback = summary
	}

	return data
}
