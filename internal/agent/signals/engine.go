package signals

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// DefaultActiveWindow is the default lookback window for active signals.
const DefaultActiveWindow = 1 * time.Hour

// Engine orchestrates the signal ingestion pipeline:
// raw events → normalised signals → derived state.
// Fail-open: all errors are logged but do not stop the pipeline.
type Engine struct {
	rawStore     *RawEventStore
	signalStore  *SignalStore
	derivedStore *DerivedStateStore
	auditor      audit.AuditRecorder
	logger       *zap.Logger
}

// NewEngine creates a signal Engine.
func NewEngine(raw *RawEventStore, sig *SignalStore, derived *DerivedStateStore, auditor audit.AuditRecorder, logger *zap.Logger) *Engine {
	return &Engine{
		rawStore:     raw,
		signalStore:  sig,
		derivedStore: derived,
		auditor:      auditor,
		logger:       logger,
	}
}

// Ingest processes a raw event through the full pipeline:
// 1. Persist raw event
// 2. Normalise to signal
// 3. Persist signal
// 4. Recompute derived state
// Returns the normalised signal (if any) and error.
func (e *Engine) Ingest(ctx context.Context, event RawEvent) (*Signal, error) {
	now := time.Now().UTC()
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.ObservedAt.IsZero() {
		event.ObservedAt = now
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}

	// 1. Persist raw event.
	if err := e.rawStore.Save(ctx, event); err != nil {
		e.logger.Warn("raw_event_save_failed", zap.Error(err), zap.String("event_id", event.ID))
		return nil, err
	}
	e.auditEvent(ctx, "signals.raw_ingested", map[string]any{
		"event_id":   event.ID,
		"source":     event.Source,
		"event_type": event.EventType,
	})

	// 2. Normalise.
	sig, ok := Normalize(event)
	if !ok {
		e.logger.Debug("event_type_not_normalised",
			zap.String("event_type", event.EventType),
			zap.String("event_id", event.ID),
		)
		return nil, nil
	}

	// 3. Persist signal.
	if err := e.signalStore.Save(ctx, sig); err != nil {
		e.logger.Warn("signal_save_failed", zap.Error(err), zap.String("signal_id", sig.ID))
		return &sig, err
	}
	e.auditEvent(ctx, "signals.normalized", map[string]any{
		"signal_id":   sig.ID,
		"signal_type": sig.SignalType,
		"severity":    sig.Severity,
		"confidence":  sig.Confidence,
		"value":       sig.Value,
		"raw_event_id": event.ID,
	})

	// 4. Recompute derived state.
	if err := e.RecomputeDerived(ctx); err != nil {
		e.logger.Warn("derived_recompute_failed", zap.Error(err))
	}

	return &sig, nil
}

// RecomputeDerived recalculates all derived state from active signals.
func (e *Engine) RecomputeDerived(ctx context.Context) error {
	signals, err := e.signalStore.ListActive(ctx, DefaultActiveWindow, 1000)
	if err != nil {
		return err
	}

	derived := ComputeDerivedState(signals)

	for k, v := range derived {
		if err := e.derivedStore.Upsert(ctx, k, v); err != nil {
			e.logger.Warn("derived_upsert_failed", zap.String("key", k), zap.Error(err))
			continue
		}
	}

	e.auditEvent(ctx, "signals.derived_updated", map[string]any{
		"signal_count": len(signals),
		"derived":      derived,
	})

	return nil
}

// GetActiveSignals returns the current active signals and derived state.
// Fail-open: returns empty ActiveSignals if any query fails.
func (e *Engine) GetActiveSignals(ctx context.Context) ActiveSignals {
	signals, err := e.signalStore.ListActive(ctx, DefaultActiveWindow, 500)
	if err != nil {
		e.logger.Warn("get_active_signals_failed", zap.Error(err))
		signals = nil
	}
	if signals == nil {
		signals = []Signal{}
	}

	derived, err := e.derivedStore.GetAll(ctx)
	if err != nil {
		e.logger.Warn("get_derived_state_failed", zap.Error(err))
		derived = map[string]float64{}
	}

	return ActiveSignals{
		Signals: signals,
		Derived: derived,
	}
}

// ListSignals returns paginated signals.
func (e *Engine) ListSignals(ctx context.Context, limit, offset int) ([]Signal, error) {
	return e.signalStore.ListAll(ctx, limit, offset)
}

// ListDerived returns all derived state entries.
func (e *Engine) ListDerived(ctx context.Context) ([]DerivedState, error) {
	return e.derivedStore.GetAllEntries(ctx)
}

// ListRawEvents returns paginated raw events.
func (e *Engine) ListRawEvents(ctx context.Context, limit, offset int) ([]RawEvent, error) {
	return e.rawStore.List(ctx, limit, offset)
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "signals", uuid.New(), eventType,
		"system", "signal_engine", payload)
}
