package stability

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/agent/reflection"
	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates a stability evaluation cycle:
// 1. Collect inputs (decisions, outcomes, memory, reflections, cycle stats)
// 2. Run detection rules
// 3. Apply policy to produce new state
// 4. Persist state
// 5. Audit changes
type Engine struct {
	store           *Store
	decisionJournal *planning.DecisionJournal
	outcomeStore    *outcome.Store
	memoryStore     *actionmemory.Store
	reflectionStore *reflection.Store
	auditor         audit.AuditRecorder
	logger          *zap.Logger

	// cycleErrorCounter tracks recent cycle errors for Rule C.
	// Set externally via RecordCycleResult().
	recentCycleErrors int
	recentCycleTotal  int
}

// NewEngine creates a StabilityEngine.
func NewEngine(
	store *Store,
	journal *planning.DecisionJournal,
	outcomeStore *outcome.Store,
	memoryStore *actionmemory.Store,
	reflectionStore *reflection.Store,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		store:           store,
		decisionJournal: journal,
		outcomeStore:    outcomeStore,
		memoryStore:     memoryStore,
		reflectionStore: reflectionStore,
		auditor:         auditor,
		logger:          logger,
	}
}

// RecordCycleResult lets the scheduler report cycle success/failure so Rule C
// can detect instability. Thread-safe is not needed — the scheduler calls
// this sequentially.
func (e *Engine) RecordCycleResult(err error) {
	e.recentCycleTotal++
	if err != nil {
		e.recentCycleErrors++
	}
	// Keep a sliding window of the last 10 cycles.
	if e.recentCycleTotal > 10 {
		// Rough decay: halve counters.
		e.recentCycleErrors = e.recentCycleErrors / 2
		e.recentCycleTotal = e.recentCycleTotal / 2
	}
}

// Evaluate runs one full stability evaluation pass.
func (e *Engine) Evaluate(ctx context.Context) (*State, *DetectionResult, error) {
	now := time.Now().UTC()

	// 1. Collect current state.
	current, err := e.store.Get(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get stability state: %w", err)
	}

	// 2. Collect inputs.
	decisions, err := e.decisionJournal.ListRecent(ctx, 50)
	if err != nil {
		return nil, nil, fmt.Errorf("load decisions: %w", err)
	}
	outcomes, err := e.outcomeStore.List(ctx, outcome.ListFilter{Limit: 100})
	if err != nil {
		return nil, nil, fmt.Errorf("load outcomes: %w", err)
	}
	memRecords, err := e.memoryStore.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load memory: %w", err)
	}
	reflections, err := e.reflectionStore.ListRecent(ctx, 50)
	if err != nil {
		return nil, nil, fmt.Errorf("load reflections: %w", err)
	}

	input := DetectionInput{
		RecentDecisions:   decisions,
		RecentOutcomes:    outcomes,
		ActionMemory:      memRecords,
		RecentReflections: reflections,
		RecentCycleErrors: e.recentCycleErrors,
		RecentCycleTotal:  e.recentCycleTotal,
		CurrentState:      current,
		Timestamp:         now,
	}

	// 3. Run detection.
	result := Detect(input)

	// 4. Apply policy.
	previousMode := current.Mode
	newState, reason := ApplyPolicy(current, result)

	// 5. Persist if state changed.
	stateChanged := newState.Mode != current.Mode ||
		newState.ThrottleMultiplier != current.ThrottleMultiplier ||
		blockedTypesChanged(current.BlockedActionTypes, newState.BlockedActionTypes)

	if stateChanged {
		if err := e.store.Update(ctx, newState); err != nil {
			e.logger.Error("stability_persist_failed", zap.Error(err))
			return current, &result, fmt.Errorf("persist state: %w", err)
		}
	}

	// 6. Audit.
	e.auditEvaluated(len(result.Findings), string(previousMode), string(newState.Mode), stateChanged)
	if stateChanged && newState.Mode != previousMode {
		e.auditModeChanged(string(previousMode), string(newState.Mode), reason, newState.BlockedActionTypes, newState.ThrottleMultiplier)
	}
	if newState.Mode == ModeNormal && previousMode != ModeNormal {
		e.auditRecovered(string(previousMode), reason)
	}

	e.logger.Info("stability_evaluated",
		zap.Int("findings", len(result.Findings)),
		zap.String("previous_mode", string(previousMode)),
		zap.String("new_mode", string(newState.Mode)),
		zap.Bool("state_changed", stateChanged),
	)

	// Return the updated state.
	final, err := e.store.Get(ctx)
	if err != nil {
		return newState, &result, nil
	}
	return final, &result, nil
}

// GetState returns the current stability state (read-only convenience).
func (e *Engine) GetState(ctx context.Context) (*State, error) {
	return e.store.Get(ctx)
}

// Reset manually restores normal mode (operator override).
func (e *Engine) Reset(ctx context.Context) (*State, error) {
	current, err := e.store.Get(ctx)
	if err != nil {
		return nil, err
	}
	previousMode := current.Mode

	st, err := e.store.Reset(ctx, "manual_operator_reset")
	if err != nil {
		return nil, err
	}

	e.auditModeChanged(string(previousMode), string(ModeNormal), "manual_operator_reset", []string{}, 1.0)
	e.logger.Info("stability_reset",
		zap.String("previous_mode", string(previousMode)),
	)
	return st, nil
}

func blockedTypesChanged(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}
	set := make(map[string]struct{}, len(a))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := set[v]; !ok {
			return true
		}
	}
	return false
}

func (e *Engine) auditEvaluated(findings int, prevMode, newMode string, changed bool) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(
		context.Background(),
		"stability", uuid.New(),
		"stability.evaluated",
		"system", "stability_engine",
		map[string]any{
			"findings_count": findings,
			"previous_mode":  prevMode,
			"new_mode":       newMode,
			"state_changed":  changed,
		},
	)
}

func (e *Engine) auditModeChanged(prevMode, newMode, reason string, blocked []string, multiplier float64) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(
		context.Background(),
		"stability", uuid.New(),
		"stability.mode_changed",
		"system", "stability_engine",
		map[string]any{
			"previous_mode":        prevMode,
			"new_mode":             newMode,
			"reason":               reason,
			"blocked_action_types": blocked,
			"throttle_multiplier":  multiplier,
		},
	)
}

func (e *Engine) auditRecovered(prevMode, reason string) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(
		context.Background(),
		"stability", uuid.New(),
		"stability.recovered",
		"system", "stability_engine",
		map[string]any{
			"previous_mode": prevMode,
			"reason":        reason,
		},
	)
}
