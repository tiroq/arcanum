package reflection

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/outcome"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates a reflection cycle: collects inputs, runs analysis,
// persists findings, and emits audit events. It is strictly read-only
// with respect to the planner and scoring — findings are advisory.
type Engine struct {
	db              *pgxpool.Pool
	decisionJournal *planning.DecisionJournal
	outcomeStore    *outcome.Store
	memoryStore     *actionmemory.Store
	findingStore    *Store
	auditor         audit.AuditRecorder
	logger          *zap.Logger
}

// NewEngine creates a ReflectionEngine.
func NewEngine(
	db *pgxpool.Pool,
	journal *planning.DecisionJournal,
	outcomeStore *outcome.Store,
	memoryStore *actionmemory.Store,
	findingStore *Store,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		db:              db,
		decisionJournal: journal,
		outcomeStore:    outcomeStore,
		memoryStore:     memoryStore,
		findingStore:    findingStore,
		auditor:         auditor,
		logger:          logger,
	}
}

// Reflect executes a full reflection cycle:
// 1. Collect recent decisions, outcomes, and memory.
// 2. Run all deterministic analysis rules.
// 3. Persist findings.
// 4. Audit the cycle.
func (e *Engine) Reflect(ctx context.Context) (*Report, error) {
	cycleID := uuid.New().String()
	now := time.Now().UTC()

	// Step 1: gather inputs.
	decisions, err := e.decisionJournal.ListRecent(ctx, 50)
	if err != nil {
		return nil, fmt.Errorf("load recent decisions: %w", err)
	}

	outcomes, err := e.outcomeStore.List(ctx, outcome.ListFilter{Limit: 100})
	if err != nil {
		return nil, fmt.Errorf("load recent outcomes: %w", err)
	}

	memoryRecords, err := e.memoryStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load action memory: %w", err)
	}

	input := AnalysisInput{
		RecentDecisions: decisions,
		RecentOutcomes:  outcomes,
		ActionMemory:    memoryRecords,
		CycleID:         cycleID,
		Timestamp:       now,
	}

	// Step 2: run deterministic analysis.
	findings := Analyze(input)

	// Step 3: persist findings (best-effort).
	if len(findings) > 0 {
		if err := e.findingStore.SaveFindings(ctx, findings); err != nil {
			e.logger.Warn("reflection_persist_failed", zap.Error(err))
			// Continue — findings are still returned.
		}
	}

	// Step 4: audit.
	e.auditReflection(cycleID, len(decisions), len(outcomes), len(findings))

	report := &Report{
		CycleID:   cycleID,
		Findings:  findings,
		CreatedAt: now,
	}

	e.logger.Info("reflection_completed",
		zap.String("cycle_id", cycleID),
		zap.Int("decisions_analyzed", len(decisions)),
		zap.Int("outcomes_analyzed", len(outcomes)),
		zap.Int("findings", len(findings)),
	)

	return report, nil
}

func (e *Engine) auditReflection(cycleID string, decisions, outcomes, findings int) {
	if e.auditor == nil {
		return
	}
	if err := e.auditor.RecordEvent(
		context.Background(),
		"reflection",
		uuid.New(),
		"reflection.completed",
		"system",
		"reflection_engine",
		map[string]any{
			"cycle_id":           cycleID,
			"decisions_analyzed": decisions,
			"outcomes_analyzed":  outcomes,
			"findings_count":     findings,
		},
	); err != nil {
		e.logger.Warn("reflection_audit_failed", zap.Error(err))
	}
}
