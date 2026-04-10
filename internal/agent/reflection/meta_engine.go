package reflection

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// MetaEngine orchestrates meta-reflection: trigger → aggregate → analyze → store → signal.
type MetaEngine struct {
	aggregator  *Aggregator
	trigger     *Trigger
	reportStore *ReportStore
	auditor     audit.AuditRecorder
	logger      *zap.Logger

	// Latest signals for decision graph consumption.
	latestSignals []ReflectionSignal
}

// NewMetaEngine creates a MetaEngine.
func NewMetaEngine(
	aggregator *Aggregator,
	trigger *Trigger,
	reportStore *ReportStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *MetaEngine {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MetaEngine{
		aggregator:  aggregator,
		trigger:     trigger,
		reportStore: reportStore,
		auditor:     auditor,
		logger:      logger,
	}
}

// RunReflection executes a full meta-reflection cycle:
// 1. Check triggers (skipped if force=true)
// 2. Aggregate data
// 3. Analyze
// 4. Build report
// 5. Store report
// 6. Emit reflection signals
// 7. Emit audit events
//
// Returns nil report if triggers don't fire and force=false.
// Fail-open: aggregation/analysis errors produce empty reports.
func (me *MetaEngine) RunReflection(ctx context.Context, force bool) (*MetaReflectionReport, error) {
	if me == nil {
		return nil, nil
	}

	now := time.Now().UTC()

	// Step 1: check triggers
	if !force && me.trigger != nil && !me.trigger.ShouldTrigger(now) {
		return nil, nil
	}

	// Audit run start
	me.emitAudit("reflection.run_started", uuid.Nil, map[string]any{
		"forced":    force,
		"timestamp": now,
	})

	// Step 2: aggregate
	periodEnd := now
	periodStart := periodEnd.Add(-24 * time.Hour) // default 24h window
	if me.trigger != nil && !me.trigger.GetState().LastRunAt.IsZero() {
		periodStart = me.trigger.GetState().LastRunAt
	}

	var data AggregatedData
	if me.aggregator != nil {
		data = me.aggregator.Aggregate(ctx, periodStart, periodEnd)
	} else {
		data = AggregatedData{
			PeriodStart:        periodStart,
			PeriodEnd:          periodEnd,
			SignalsSummary:     make(map[string]float64),
			ManualActionCounts: make(map[string]int),
		}
	}

	// Step 3: analyze
	insights := MetaAnalyze(data)

	// Step 4: build report
	reportID := uuid.New().String()
	report := MetaReflectionReport{
		ID:                 reportID,
		PeriodStart:        data.PeriodStart,
		PeriodEnd:          data.PeriodEnd,
		ActionsCount:       data.ActionsCount,
		OpportunitiesCount: data.OpportunitiesCount,
		IncomeEstimated:    data.IncomeEstimated,
		IncomeVerified:     data.IncomeVerified,
		SuccessRate:        data.SuccessRate,
		AvgAccuracy:        data.AvgAccuracy,
		AvgValuePerHour:    data.ValuePerHour,
		FailureCount:       data.FailureCount,
		SignalsSummary:     data.SignalsSummary,
		Inefficiencies:     insights.Inefficiencies,
		Improvements:       insights.Improvements,
		RiskFlags:          insights.RiskFlags,
		CreatedAt:          now,
	}

	// Ensure nil slices become empty arrays in JSON
	if report.Inefficiencies == nil {
		report.Inefficiencies = []Inefficiency{}
	}
	if report.Improvements == nil {
		report.Improvements = []Improvement{}
	}
	if report.RiskFlags == nil {
		report.RiskFlags = []RiskFlag{}
	}
	if report.SignalsSummary == nil {
		report.SignalsSummary = make(map[string]float64)
	}

	// Step 5: store report (best-effort)
	if me.reportStore != nil {
		if err := me.reportStore.SaveReport(ctx, report); err != nil {
			me.logger.Warn("meta_reflection_persist_failed", zap.Error(err))
			// Continue — report is still returned
		}
	}

	// Step 6: emit reflection signals
	me.latestSignals = insights.Signals
	if me.latestSignals == nil {
		me.latestSignals = []ReflectionSignal{}
	}

	for _, sig := range me.latestSignals {
		me.emitAudit("reflection.signal_emitted", uuid.Nil, map[string]any{
			"report_id":   reportID,
			"signal_type": string(sig.SignalType),
			"strength":    sig.Strength,
		})
	}

	// Step 7: audit report creation
	me.emitAudit("reflection.report_created", uuid.Nil, map[string]any{
		"report_id":      reportID,
		"period_start":   report.PeriodStart,
		"period_end":     report.PeriodEnd,
		"actions_count":  report.ActionsCount,
		"inefficiencies": len(report.Inefficiencies),
		"improvements":   len(report.Improvements),
		"risk_flags":     len(report.RiskFlags),
		"signals":        len(me.latestSignals),
	})

	// Reset trigger state
	if me.trigger != nil {
		me.trigger.Reset(now)
	}

	me.logger.Info("meta_reflection_completed",
		zap.String("report_id", reportID),
		zap.Int("actions_count", report.ActionsCount),
		zap.Int("inefficiencies", len(report.Inefficiencies)),
		zap.Int("signals", len(me.latestSignals)),
	)

	return &report, nil
}

// GetLatestSignals returns the most recent reflection signals for decision graph use.
// Fail-open: returns empty slice if none available.
func (me *MetaEngine) GetLatestSignals() []ReflectionSignal {
	if me == nil {
		return nil
	}
	return me.latestSignals
}

func (me *MetaEngine) emitAudit(eventType string, entityID uuid.UUID, payload map[string]any) {
	if me.auditor == nil {
		return
	}
	if err := me.auditor.RecordEvent(
		context.Background(),
		"reflection",
		entityID,
		eventType,
		"system",
		"meta_reflection_engine",
		payload,
	); err != nil {
		me.logger.Warn("meta_reflection_audit_failed",
			zap.String("event_type", eventType),
			zap.Error(err),
		)
	}
}
