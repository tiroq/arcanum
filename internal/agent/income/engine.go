package income

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// EngineGovernanceProvider reads current governance mode without importing governance.
type EngineGovernanceProvider interface {
	GetMode(ctx context.Context) string
}

// EngineFinancialTruthProvider supplies verified financial values for learning.
// Defined here to avoid import cycles — implemented in financial_truth package.
// Fail-open: returns 0 when nil or unavailable.
type EngineFinancialTruthProvider interface {
	GetVerifiedValueForOpportunity(ctx context.Context, oppID string) float64
}

// Engine orchestrates the full income pipeline:
//
//	create opportunity → score → generate proposals → persist → audit
type Engine struct {
	oppStore      *OpportunityStore
	propStore     *ProposalStore
	outcomeStr    *OutcomeStore
	learningStore *LearningStore
	governance    EngineGovernanceProvider
	truthProvider EngineFinancialTruthProvider
	auditor       audit.AuditRecorder
	logger        *zap.Logger
}

// NewEngine creates an Engine with required stores.
func NewEngine(
	oppStore *OpportunityStore,
	propStore *ProposalStore,
	outcomeStr *OutcomeStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		oppStore:   oppStore,
		propStore:  propStore,
		outcomeStr: outcomeStr,
		auditor:    auditor,
		logger:     logger,
	}
}

// WithGovernance attaches a governance provider to enable governance-aware proposal gating.
func (e *Engine) WithGovernance(gp EngineGovernanceProvider) *Engine {
	e.governance = gp
	return e
}

// WithLearning attaches the learning store for outcome attribution (Iteration 39).
func (e *Engine) WithLearning(ls *LearningStore) *Engine {
	e.learningStore = ls
	return e
}

// WithTruthProvider attaches a verified financial truth provider (Iteration 42).
// When present, learning and attribution prefer verified values.
func (e *Engine) WithTruthProvider(tp EngineFinancialTruthProvider) *Engine {
	e.truthProvider = tp
	return e
}

// CreateOpportunity validates and persists a new income opportunity.
func (e *Engine) CreateOpportunity(ctx context.Context, o IncomeOpportunity) (IncomeOpportunity, error) {
	if err := validateOpportunity(o); err != nil {
		return IncomeOpportunity{}, fmt.Errorf("validate opportunity: %w", err)
	}
	if o.ID == "" {
		o.ID = uuid.New().String()
	}

	saved, err := e.oppStore.Create(ctx, o)
	if err != nil {
		return IncomeOpportunity{}, fmt.Errorf("create opportunity: %w", err)
	}

	e.auditor.RecordEvent(ctx, "income_opportunity", uuid.MustParse(saved.ID), //nolint:errcheck
		"income.opportunity_created", "income_engine", "engine", map[string]any{
			"opportunity_id":   saved.ID,
			"title":            saved.Title,
			"opportunity_type": saved.OpportunityType,
			"estimated_value":  saved.EstimatedValue,
			"confidence":       saved.Confidence,
		})

	e.logger.Info("income opportunity created",
		zap.String("id", saved.ID),
		zap.String("title", saved.Title),
		zap.Float64("estimated_value", saved.EstimatedValue),
	)
	return saved, nil
}

// EvaluateOpportunity scores an existing opportunity and generates proposals.
// The opportunity status is updated to StatusEvaluated (or StatusProposed).
// Returns the generated proposals (may be nil if score < ScoreThreshold).
func (e *Engine) EvaluateOpportunity(ctx context.Context, id string) ([]IncomeActionProposal, error) {
	opp, err := e.oppStore.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get opportunity: %w", err)
	}

	score := ScoreOpportunity(opp)

	e.auditor.RecordEvent(ctx, "income_opportunity", uuid.MustParse(opp.ID), //nolint:errcheck
		"income.opportunity_evaluated", "income_engine", "engine", map[string]any{
			"opportunity_id": opp.ID,
			"score":          score,
		})

	frozen := false
	if e.governance != nil {
		mode := e.governance.GetMode(ctx)
		frozen = mode == "frozen" || mode == "safe_hold" || mode == "rollback_only"
	}

	proposals := GenerateProposals(opp, score, frozen)

	if len(proposals) == 0 {
		_ = e.oppStore.UpdateStatus(ctx, id, StatusEvaluated)
		e.logger.Info("income opportunity evaluated, no proposals generated",
			zap.String("id", id),
			zap.Float64("score", score),
		)
		return nil, nil
	}

	saved := make([]IncomeActionProposal, 0, len(proposals))
	for _, p := range proposals {
		sp, err := e.propStore.Create(ctx, p)
		if err != nil {
			e.logger.Error("failed to create income proposal", zap.Error(err), zap.String("action_type", p.ActionType))
			continue
		}
		saved = append(saved, sp)

		e.auditor.RecordEvent(ctx, "income_proposal", uuid.MustParse(sp.ID), //nolint:errcheck
			"income.proposal_created", "income_engine", "engine", map[string]any{
				"proposal_id":     sp.ID,
				"opportunity_id":  sp.OpportunityID,
				"action_type":     sp.ActionType,
				"expected_value":  sp.ExpectedValue,
				"risk_level":      sp.RiskLevel,
				"requires_review": sp.RequiresReview,
			})
	}

	_ = e.oppStore.UpdateStatus(ctx, id, StatusProposed)

	e.logger.Info("income opportunity proposals generated",
		zap.String("id", id),
		zap.Float64("score", score),
		zap.Int("proposals", len(saved)),
	)
	return saved, nil
}

// RecordOutcome persists an income outcome, closes the opportunity, and updates
// learning signals from the real outcome attribution (Iteration 39).
func (e *Engine) RecordOutcome(ctx context.Context, o IncomeOutcome) (IncomeOutcome, error) {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}
	if o.OutcomeStatus == "" {
		return IncomeOutcome{}, fmt.Errorf("outcome_status is required")
	}
	if o.OutcomeSource != "" && !validOutcomeSources[o.OutcomeSource] {
		return IncomeOutcome{}, fmt.Errorf("invalid outcome_source %q; must be manual|system|external", o.OutcomeSource)
	}

	saved, err := e.outcomeStr.Create(ctx, o)
	if err != nil {
		return IncomeOutcome{}, fmt.Errorf("create income outcome: %w", err)
	}

	_ = e.oppStore.UpdateStatus(ctx, o.OpportunityID, StatusClosed)

	e.auditor.RecordEvent(ctx, "income_outcome", uuid.MustParse(saved.ID), //nolint:errcheck
		"outcome.recorded", "income_engine", "engine", map[string]any{
			"outcome_id":       saved.ID,
			"opportunity_id":   saved.OpportunityID,
			"proposal_id":      saved.ProposalID,
			"outcome_status":   saved.OutcomeStatus,
			"actual_value":     saved.ActualValue,
			"owner_time_saved": saved.OwnerTimeSaved,
			"outcome_source":   saved.OutcomeSource,
			"verified":         saved.Verified,
		})

	// Attribution: link outcome back to opportunity and update learning (Iteration 39).
	e.processAttribution(ctx, saved)

	e.logger.Info("income outcome recorded",
		zap.String("id", saved.ID),
		zap.String("opportunity_id", saved.OpportunityID),
		zap.Float64("actual_value", saved.ActualValue),
		zap.String("outcome_source", saved.OutcomeSource),
		zap.Bool("verified", saved.Verified),
	)
	return saved, nil
}

// processAttribution links the outcome to the opportunity, computes accuracy,
// and updates per-type learning records. Fail-open: logs errors but does not
// propagate them to the caller.
func (e *Engine) processAttribution(ctx context.Context, outcome IncomeOutcome) {
	opp, err := e.oppStore.GetByID(ctx, outcome.OpportunityID)
	if err != nil {
		e.logger.Warn("attribution: could not retrieve opportunity",
			zap.String("opportunity_id", outcome.OpportunityID),
			zap.Error(err),
		)
		return
	}

	attr := BuildAttribution(opp, outcome)

	// Iteration 42: prefer verified financial truth over attributed value.
	// If a verified value exists for this opportunity, use it for accuracy
	// computation while preserving the original attributed actual_value.
	var verifiedValue float64
	var financiallyVerified bool
	if e.truthProvider != nil {
		vv := e.truthProvider.GetVerifiedValueForOpportunity(ctx, outcome.OpportunityID)
		if vv > 0 {
			verifiedValue = vv
			financiallyVerified = true
			// Recompute accuracy using verified value as ground truth.
			attr.Accuracy = ComputeAccuracy(opp.EstimatedValue, vv, outcome.OutcomeStatus)
		}
	}

	auditPayload := map[string]any{
		"outcome_id":           attr.OutcomeID,
		"opportunity_id":       attr.OpportunityID,
		"opportunity_type":     attr.OpportunityType,
		"estimated_value":      attr.EstimatedValue,
		"actual_value":         attr.ActualValue,
		"accuracy":             attr.Accuracy,
		"outcome_status":       attr.OutcomeStatus,
		"financially_verified": financiallyVerified,
	}
	if financiallyVerified {
		auditPayload["verified_value"] = verifiedValue
	}
	e.auditor.RecordEvent(ctx, "income_outcome", uuid.MustParse(outcome.ID), //nolint:errcheck
		"outcome.attributed", "income_engine", "engine", auditPayload)

	// Update learning store if available.
	if e.learningStore == nil {
		return
	}

	existing, err := e.learningStore.GetByType(ctx, attr.OpportunityType)
	if err != nil {
		e.logger.Warn("attribution: could not load learning record",
			zap.String("opportunity_type", attr.OpportunityType),
			zap.Error(err),
		)
		return
	}

	updated := UpdateLearningFromAttribution(existing, attr)
	if err := e.learningStore.Upsert(ctx, updated); err != nil {
		e.logger.Warn("attribution: could not persist learning record",
			zap.String("opportunity_type", attr.OpportunityType),
			zap.Error(err),
		)
		return
	}

	e.auditor.RecordEvent(ctx, "income_learning", uuid.Nil, //nolint:errcheck
		"learning.updated", "income_engine", "engine", map[string]any{
			"opportunity_type":      updated.OpportunityType,
			"total_outcomes":        updated.TotalOutcomes,
			"success_rate":          updated.SuccessRate,
			"avg_accuracy":          updated.AvgAccuracy,
			"confidence_adjustment": updated.ConfidenceAdjustment,
		})

	e.logger.Info("income learning updated",
		zap.String("opportunity_type", updated.OpportunityType),
		zap.Int("total_outcomes", updated.TotalOutcomes),
		zap.Float64("success_rate", updated.SuccessRate),
		zap.Float64("avg_accuracy", updated.AvgAccuracy),
	)
}

// ListOpportunities returns paginated opportunities.
func (e *Engine) ListOpportunities(ctx context.Context, limit, offset int) ([]IncomeOpportunity, error) {
	return e.oppStore.List(ctx, limit, offset)
}

// ListProposals returns paginated proposals.
func (e *Engine) ListProposals(ctx context.Context, limit, offset int) ([]IncomeActionProposal, error) {
	return e.propStore.List(ctx, limit, offset)
}

// ListOutcomes returns paginated outcomes.
func (e *Engine) ListOutcomes(ctx context.Context, limit, offset int) ([]IncomeOutcome, error) {
	return e.outcomeStr.List(ctx, limit, offset)
}

// GetBestOpenScore returns the highest score among open opportunities (0 if none).
func (e *Engine) GetBestOpenScore(ctx context.Context) float64 {
	return e.oppStore.BestOpenScore(ctx)
}

// GetSignal returns a lightweight income signal snapshot.
func (e *Engine) GetSignal(ctx context.Context) IncomeSignal {
	return IncomeSignal{
		BestOpenScore:     e.oppStore.BestOpenScore(ctx),
		OpenOpportunities: e.oppStore.CountOpen(ctx),
	}
}

// GetPerformance returns a summary of income attribution performance (Iteration 39).
// Returns a zero-value PerformanceStats if learning store is not available (fail-open).
func (e *Engine) GetPerformance(ctx context.Context) PerformanceStats {
	stats := PerformanceStats{}
	if e.learningStore == nil {
		return stats
	}
	records, err := e.learningStore.GetAll(ctx)
	if err != nil {
		e.logger.Warn("get performance: could not load learning records", zap.Error(err))
		return stats
	}
	stats.ByType = records
	stats.VerifiedOutcomes = e.outcomeStr.CountVerified(ctx)

	var totalOutcomes int
	var totalSuccess int
	var totalAccuracy float64
	for _, r := range records {
		totalOutcomes += r.TotalOutcomes
		totalSuccess += r.SuccessCount
		totalAccuracy += r.TotalAccuracy
	}
	stats.TotalOutcomes = totalOutcomes
	if totalOutcomes > 0 {
		stats.OverallAccuracy = totalAccuracy / float64(totalOutcomes)
		stats.OverallSuccessRate = float64(totalSuccess) / float64(totalOutcomes)
	}
	return stats
}

// GetLearningForType returns the learning record for a given opportunity type.
// Returns a zero-value record if learning store is nil or type not found (fail-open).
func (e *Engine) GetLearningForType(ctx context.Context, oppType string) LearningRecord {
	if e.learningStore == nil {
		return LearningRecord{OpportunityType: oppType}
	}
	r, _ := e.learningStore.GetByType(ctx, oppType)
	return r
}

// --- validation ---

func validateOpportunity(o IncomeOpportunity) error {
	if o.Title == "" {
		return fmt.Errorf("title is required")
	}
	if !validOpportunityTypes[o.OpportunityType] {
		return fmt.Errorf("invalid opportunity_type %q; must be one of consulting|automation|service|content|other", o.OpportunityType)
	}
	if o.EstimatedValue < 0 {
		return fmt.Errorf("estimated_value must be ≥ 0")
	}
	if o.EstimatedEffort < 0 || o.EstimatedEffort > 1 {
		return fmt.Errorf("estimated_effort must be in [0,1]")
	}
	if o.Confidence < 0 || o.Confidence > 1 {
		return fmt.Errorf("confidence must be in [0,1]")
	}
	return nil
}
