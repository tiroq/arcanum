package portfolio

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// FinancialPressureProvider reads current financial pressure.
// Defined here to avoid import cycles.
type FinancialPressureProvider interface {
	GetPressure(ctx context.Context) (pressureScore float64, urgencyLevel string)
}

// CapacityProvider reads current owner capacity.
// Defined here to avoid import cycles.
type CapacityProvider interface {
	GetAvailableHoursWeek(ctx context.Context) float64
}

// Engine orchestrates the portfolio lifecycle: create strategies,
// allocate capacity, track performance, rebalance, and emit signals.
type Engine struct {
	strategies   *StrategyStore
	allocations  *AllocationStore
	performances *PerformanceStore
	auditor      audit.AuditRecorder
	logger       *zap.Logger

	pressure FinancialPressureProvider
	capacity CapacityProvider
}

// NewEngine creates a new portfolio engine.
func NewEngine(
	strategies *StrategyStore,
	allocations *AllocationStore,
	performances *PerformanceStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		strategies:   strategies,
		allocations:  allocations,
		performances: performances,
		auditor:      auditor,
		logger:       logger,
	}
}

// WithPressure sets the financial pressure provider.
func (e *Engine) WithPressure(p FinancialPressureProvider) *Engine {
	e.pressure = p
	return e
}

// WithCapacity sets the capacity provider.
func (e *Engine) WithCapacity(c CapacityProvider) *Engine {
	e.capacity = c
	return e
}

// CreateStrategy validates and persists a new strategy.
func (e *Engine) CreateStrategy(ctx context.Context, st Strategy) (Strategy, error) {
	if st.Name == "" {
		return Strategy{}, fmt.Errorf("strategy name is required")
	}
	if !ValidStrategyTypes[st.Type] {
		return Strategy{}, fmt.Errorf("invalid strategy type: %s", st.Type)
	}
	if st.Volatility < 0 || st.Volatility > 1 {
		return Strategy{}, fmt.Errorf("volatility must be in [0, 1]")
	}
	if st.ExpectedReturnPerHr < 0 {
		return Strategy{}, fmt.Errorf("expected_return_per_hour must be >= 0")
	}

	saved, err := e.strategies.Create(ctx, st)
	if err != nil {
		return Strategy{}, err
	}

	e.auditEvent(ctx, "strategy", saved.ID, "strategy.created", map[string]any{
		"strategy_id":  saved.ID,
		"name":         saved.Name,
		"type":         saved.Type,
		"expected_rph": saved.ExpectedReturnPerHr,
		"volatility":   saved.Volatility,
	})

	return saved, nil
}

// UpdateStrategyStatus changes a strategy's status.
func (e *Engine) UpdateStrategyStatus(ctx context.Context, id, status string) error {
	if !ValidStatuses[status] {
		return fmt.Errorf("invalid status: %s", status)
	}
	if err := e.strategies.UpdateStatus(ctx, id, status); err != nil {
		return err
	}

	e.auditEvent(ctx, "strategy", id, "strategy.updated", map[string]any{
		"strategy_id": id,
		"new_status":  status,
	})

	return nil
}

// ListStrategies returns all strategies.
func (e *Engine) ListStrategies(ctx context.Context) ([]Strategy, error) {
	return e.strategies.ListAll(ctx)
}

// GetPortfolio assembles the full portfolio view.
func (e *Engine) GetPortfolio(ctx context.Context) (Portfolio, error) {
	strategies, err := e.strategies.ListActive(ctx)
	if err != nil {
		return Portfolio{}, err
	}

	allocs, err := e.allocations.ListAll(ctx)
	if err != nil {
		return Portfolio{}, err
	}
	allocMap := make(map[string]StrategyAllocation, len(allocs))
	for _, a := range allocs {
		allocMap[a.StrategyID] = a
	}

	perfs, err := e.performances.ListAll(ctx)
	if err != nil {
		return Portfolio{}, err
	}
	perfMap := make(map[string]StrategyPerformance, len(perfs))
	for _, p := range perfs {
		perfMap[p.StrategyID] = p
	}

	var entries []PortfolioEntry
	totalAllocated := 0.0
	totalActual := 0.0
	totalRevenue := 0.0
	allocHrsMap := make(map[string]float64)

	for _, st := range strategies {
		entry := PortfolioEntry{Strategy: st}
		if a, ok := allocMap[st.ID]; ok {
			entry.Allocation = &a
			totalAllocated += a.AllocatedHours
			totalActual += a.ActualHours
			allocHrsMap[st.ID] = a.AllocatedHours
		}
		if p, ok := perfMap[st.ID]; ok {
			entry.Performance = &p
			totalRevenue += p.TotalRevenue
		}
		entries = append(entries, entry)
	}

	portfolioROI := 0.0
	if totalActual > 0 {
		portfolioROI = totalRevenue / totalActual
	}

	return Portfolio{
		Entries:            entries,
		TotalAllocatedHrs:  totalAllocated,
		TotalActualHrs:     totalActual,
		TotalRevenue:       totalRevenue,
		PortfolioROI:       portfolioROI,
		DiversificationIdx: ComputeDiversificationIndex(allocHrsMap),
	}, nil
}

// GetPerformance returns all performance records as a summary.
func (e *Engine) GetPerformance(ctx context.Context) ([]StrategyPerformance, error) {
	return e.performances.ListAll(ctx)
}

// RecordPerformance updates the performance data for a strategy.
func (e *Engine) RecordPerformance(ctx context.Context, perf StrategyPerformance) error {
	if perf.TotalTimeSpent > 0 {
		perf.ROI = ComputeROI(perf.TotalRevenue, perf.TotalTimeSpent)
	}

	if err := e.performances.Upsert(ctx, perf); err != nil {
		return err
	}

	e.auditEvent(ctx, "strategy_performance", perf.StrategyID, "strategy.performance_updated", map[string]any{
		"strategy_id":      perf.StrategyID,
		"total_revenue":    perf.TotalRevenue,
		"total_time_spent": perf.TotalTimeSpent,
		"roi":              perf.ROI,
		"conversion_rate":  perf.ConversionRate,
	})

	return nil
}

// Rebalance re-allocates capacity across active strategies.
func (e *Engine) Rebalance(ctx context.Context) (RebalanceResult, error) {
	strategies, err := e.strategies.ListActive(ctx)
	if err != nil {
		return RebalanceResult{}, err
	}
	if len(strategies) == 0 {
		return RebalanceResult{Reason: "no active strategies"}, nil
	}

	// Get current allocations for the "previous" snapshot.
	prevAllocs, err := e.allocations.ListAll(ctx)
	if err != nil {
		return RebalanceResult{}, err
	}

	// Gather performance.
	perfs, err := e.performances.ListAll(ctx)
	if err != nil {
		return RebalanceResult{}, err
	}
	perfMap := make(map[string]StrategyPerformance, len(perfs))
	for _, p := range perfs {
		perfMap[p.StrategyID] = p
	}

	// Determine inputs.
	pressure := 0.0
	if e.pressure != nil {
		pressure, _ = e.pressure.GetPressure(ctx)
	}

	availableHours := 40.0 // weekly default
	if e.capacity != nil {
		h := e.capacity.GetAvailableHoursWeek(ctx)
		if h > 0 {
			availableHours = h
		}
	}

	familyPriorityHigh := pressure > 0.5 // heuristic: high pressure = safety focus

	// Compute scores and allocations.
	rawScores := ComputeAllocationScores(strategies, perfMap, pressure, familyPriorityHigh)
	hourAllocs := NormaliseAllocations(rawScores, availableHours)

	// Persist new allocations.
	var newAllocs []StrategyAllocation
	now := time.Now().UTC()
	for _, st := range strategies {
		hrs := hourAllocs[st.ID]
		alloc := StrategyAllocation{
			ID:             uuid.New().String(),
			StrategyID:     st.ID,
			AllocatedHours: hrs,
			ActualHours:    0,
			CreatedAt:      now,
		}

		// Preserve actual_hours from previous allocation if exists.
		for _, pa := range prevAllocs {
			if pa.StrategyID == st.ID {
				alloc.ActualHours = pa.ActualHours
				break
			}
		}

		saved, err := e.allocations.Upsert(ctx, alloc)
		if err != nil {
			return RebalanceResult{}, fmt.Errorf("upsert allocation for %s: %w", st.ID, err)
		}
		newAllocs = append(newAllocs, saved)
	}

	// Detect signals.
	allocMap := make(map[string]float64, len(hourAllocs))
	for k, v := range hourAllocs {
		allocMap[k] = v
	}
	signals := DetectSignals(strategies, perfMap, allocMap, availableHours)

	result := RebalanceResult{
		PreviousAllocations: prevAllocs,
		NewAllocations:      newAllocs,
		Signals:             signals,
		Reason:              "rebalanced based on current performance and constraints",
	}

	e.auditEvent(ctx, "portfolio", "portfolio", "portfolio.rebalanced", map[string]any{
		"strategy_count":  len(strategies),
		"available_hours": availableHours,
		"pressure":        pressure,
		"signal_count":    len(signals),
		"diversification": ComputeDiversificationIndex(allocMap),
	})

	return result, nil
}

// FindStrategyForOpportunity maps an opportunity type to its active strategy.
func (e *Engine) FindStrategyForOpportunity(ctx context.Context, opportunityType string) (Strategy, error) {
	strategyType := MapOpportunityToStrategy(opportunityType)
	return e.strategies.FindByType(ctx, strategyType)
}

// GetStrategiesAndPerformance returns loaded strategies and their performance,
// for use by the graph adapter.
func (e *Engine) GetStrategiesAndPerformance(ctx context.Context) ([]Strategy, map[string]StrategyPerformance, error) {
	strategies, err := e.strategies.ListActive(ctx)
	if err != nil {
		return nil, nil, err
	}

	perfs, err := e.performances.ListAll(ctx)
	if err != nil {
		return nil, nil, err
	}
	perfMap := make(map[string]StrategyPerformance, len(perfs))
	for _, p := range perfs {
		perfMap[p.StrategyID] = p
	}

	return strategies, perfMap, nil
}

func (e *Engine) auditEvent(ctx context.Context, entityType, entityID, eventType string, payload map[string]any) {
	id, _ := uuid.Parse(entityID)
	if id == uuid.Nil {
		id = uuid.New()
	}
	if err := e.auditor.RecordEvent(ctx, entityType, id, eventType, "portfolio_engine", "engine", payload); err != nil {
		e.logger.Warn("audit event failed",
			zap.String("event", eventType),
			zap.Error(err),
		)
	}
}
