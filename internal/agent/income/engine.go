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

// Engine orchestrates the full income pipeline:
//
//	create opportunity → score → generate proposals → persist → audit
type Engine struct {
	oppStore   *OpportunityStore
	propStore  *ProposalStore
	outcomeStr *OutcomeStore
	governance EngineGovernanceProvider
	auditor    audit.AuditRecorder
	logger     *zap.Logger
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

// RecordOutcome persists an income outcome and closes the opportunity.
func (e *Engine) RecordOutcome(ctx context.Context, o IncomeOutcome) (IncomeOutcome, error) {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}
	if o.OutcomeStatus == "" {
		return IncomeOutcome{}, fmt.Errorf("outcome_status is required")
	}

	saved, err := e.outcomeStr.Create(ctx, o)
	if err != nil {
		return IncomeOutcome{}, fmt.Errorf("create income outcome: %w", err)
	}

	_ = e.oppStore.UpdateStatus(ctx, o.OpportunityID, StatusClosed)

	e.auditor.RecordEvent(ctx, "income_outcome", uuid.MustParse(saved.ID), //nolint:errcheck
		"income.outcome_recorded", "income_engine", "engine", map[string]any{
			"outcome_id":       saved.ID,
			"opportunity_id":   saved.OpportunityID,
			"proposal_id":      saved.ProposalID,
			"outcome_status":   saved.OutcomeStatus,
			"actual_value":     saved.ActualValue,
			"owner_time_saved": saved.OwnerTimeSaved,
		})

	e.logger.Info("income outcome recorded",
		zap.String("id", saved.ID),
		zap.String("opportunity_id", saved.OpportunityID),
		zap.Float64("actual_value", saved.ActualValue),
	)
	return saved, nil
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
