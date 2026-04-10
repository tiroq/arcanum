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
	if st.StabilityScore < 0 || st.StabilityScore > 1 {
		return Strategy{}, fmt.Errorf("stability_score must be in [0, 1]")
	}
	if st.ExpectedReturnPerHr < 0 {
		return Strategy{}, fmt.Errorf("expected_return_per_hour must be >= 0")
	}
	if st.Confidence < 0 || st.Confidence > 1 {
		return Strategy{}, fmt.Errorf("confidence must be in [0, 1]")
	}

	saved, err := e.strategies.Create(ctx, st)
	if err != nil {
		return Strategy{}, err
	}

	e.auditEvent(ctx, "strategy", saved.ID, "strategy.created", map[string]any{
		"strategy_id":         saved.ID,
		"name":                saved.Name,
		"type":                saved.Type,
		"expected_rph":        saved.ExpectedReturnPerHr,
		"stability_score":     saved.StabilityScore,
		"confidence":          saved.Confidence,
		"time_to_first_value": saved.TimeToFirstValue,
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
	dominantID := ""
	dominantHrs := 0.0
	var latestRebalance time.Time

	for _, st := range strategies {
		entry := PortfolioEntry{Strategy: st}
		if a, ok := allocMap[st.ID]; ok {
			entry.Allocation = &a
			totalAllocated += a.AllocatedHours
			totalActual += a.ActualHours
			allocHrsMap[st.ID] = a.AllocatedHours
			if a.AllocatedHours > dominantHrs {
				dominantHrs = a.AllocatedHours
				dominantID = st.ID
			}
			if a.CreatedAt.After(latestRebalance) {
				latestRebalance = a.CreatedAt
			}
		}
		if p, ok := perfMap[st.ID]; ok {
			entry.Performance = &p
			totalRevenue += p.TotalVerifiedRevenue
		}
		entries = append(entries, entry)
	}

	portfolioROI := 0.0
	if totalActual > 0 {
		portfolioROI = totalRevenue / totalActual
	}

	divIdx := ComputeDiversificationIndex(allocHrsMap)

	return Portfolio{
		Entries: entries,
		Summary: PortfolioSummary{
			TotalActiveStrategies: len(strategies),
			TotalAllocatedHours:   totalAllocated,
			DominantStrategyID:    dominantID,
			DiversificationScore:  divIdx,
			RebalancedAt:          latestRebalance,
		},
		TotalAllocatedHrs:  totalAllocated,
		TotalActualHrs:     totalActual,
		TotalRevenue:       totalRevenue,
		PortfolioROI:       portfolioROI,
		DiversificationIdx: divIdx,
	}, nil
}

// GetAllocations returns all current allocations.
func (e *Engine) GetAllocations(ctx context.Context) ([]StrategyAllocation, error) {
	return e.allocations.ListAll(ctx)
}

// GetPerformance returns all performance records as a summary.
func (e *Engine) GetPerformance(ctx context.Context) ([]StrategyPerformance, error) {
	return e.performances.ListAll(ctx)
}

// RecordPerformance updates the performance data for a strategy.
func (e *Engine) RecordPerformance(ctx context.Context, perf StrategyPerformance) error {
	if perf.TotalEstimatedHours > 0 {
		perf.ROIPerHour = ComputeROI(perf.TotalVerifiedRevenue, perf.TotalEstimatedHours)
	}
	if perf.OpportunityCount > 0 {
		perf.ConversionRate = ComputeConversionRate(perf.WonCount, perf.OpportunityCount)
	}

	if err := e.performances.Upsert(ctx, perf); err != nil {
		return err
	}

	e.auditEvent(ctx, "strategy_performance", perf.StrategyID, "strategy.performance_updated", map[string]any{
		"strategy_id":            perf.StrategyID,
		"total_verified_revenue": perf.TotalVerifiedRevenue,
		"total_estimated_hours":  perf.TotalEstimatedHours,
		"roi_per_hour":           perf.ROIPerHour,
		"conversion_rate":        perf.ConversionRate,
		"opportunity_count":      perf.OpportunityCount,
		"won_count":              perf.WonCount,
		"lost_count":             perf.LostCount,
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
	hourAllocs, weightAllocs := NormaliseAllocations(rawScores, availableHours)

	// Persist new allocations.
	var newAllocs []StrategyAllocation
	now := time.Now().UTC()
	for _, st := range strategies {
		hrs := hourAllocs[st.ID]
		wt := weightAllocs[st.ID]
		alloc := StrategyAllocation{
			ID:               uuid.New().String(),
			StrategyID:       st.ID,
			AllocatedHours:   hrs,
			ActualHours:      0,
			AllocationWeight: wt,
			CreatedAt:        now,
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

		e.auditEvent(ctx, "strategy_allocation", st.ID, "portfolio.allocation_updated", map[string]any{
			"strategy_id":       st.ID,
			"allocated_hours":   hrs,
			"allocation_weight": wt,
			"roi_per_hour":      rawScores[st.ID],
			"reason":            "rebalance",
		})
	}

	// Detect signals.
	allocMap := make(map[string]float64, len(hourAllocs))
	for k, v := range hourAllocs {
		allocMap[k] = v
	}
	signals := DetectSignals(strategies, perfMap, allocMap, availableHours)

	// Emit audit events for each signal.
	for _, sig := range signals {
		e.auditEvent(ctx, "strategy", sig.StrategyID, "strategy.signal_applied", map[string]any{
			"strategy_id":   sig.StrategyID,
			"strategy_type": sig.StrategyType,
			"signal_type":   sig.SignalType,
			"score":         sig.Score,
			"reason":        sig.Reason,
		})
	}

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
