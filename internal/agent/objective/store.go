package objective

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ObjectiveStateStore manages persistence for ObjectiveState (single-row UPSERT).
type ObjectiveStateStore struct {
	pool *pgxpool.Pool
}

// NewObjectiveStateStore creates a new ObjectiveStateStore.
func NewObjectiveStateStore(pool *pgxpool.Pool) *ObjectiveStateStore {
	return &ObjectiveStateStore{pool: pool}
}

// Upsert persists the current objective state.
func (s *ObjectiveStateStore) Upsert(ctx context.Context, state ObjectiveState) error {
	if state.ComputedAt.IsZero() {
		state.ComputedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO agent_objective_state (id, verified_income_score, income_growth_score, owner_relief_score, family_stability_score, strategy_quality_score, execution_readiness_score, computed_at)
		VALUES ('current', $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
			SET verified_income_score     = EXCLUDED.verified_income_score,
				income_growth_score       = EXCLUDED.income_growth_score,
				owner_relief_score        = EXCLUDED.owner_relief_score,
				family_stability_score    = EXCLUDED.family_stability_score,
				strategy_quality_score    = EXCLUDED.strategy_quality_score,
				execution_readiness_score = EXCLUDED.execution_readiness_score,
				computed_at               = EXCLUDED.computed_at`
	_, err := s.pool.Exec(ctx, q,
		state.VerifiedIncomeScore, state.IncomeGrowthScore, state.OwnerReliefScore,
		state.FamilyStabilityScore, state.StrategyQualityScore, state.ExecutionReadinessScore,
		state.ComputedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert objective state: %w", err)
	}
	return nil
}

// Get retrieves the current objective state.
func (s *ObjectiveStateStore) Get(ctx context.Context) (ObjectiveState, error) {
	const q = `
		SELECT verified_income_score, income_growth_score, owner_relief_score,
			family_stability_score, strategy_quality_score, execution_readiness_score, computed_at
		FROM agent_objective_state WHERE id = 'current'`
	var st ObjectiveState
	err := s.pool.QueryRow(ctx, q).Scan(
		&st.VerifiedIncomeScore, &st.IncomeGrowthScore, &st.OwnerReliefScore,
		&st.FamilyStabilityScore, &st.StrategyQualityScore, &st.ExecutionReadinessScore,
		&st.ComputedAt,
	)
	if err == pgx.ErrNoRows {
		return ObjectiveState{}, nil
	}
	if err != nil {
		return ObjectiveState{}, fmt.Errorf("get objective state: %w", err)
	}
	return st, nil
}

// RiskStateStore manages persistence for RiskState (single-row UPSERT).
type RiskStateStore struct {
	pool *pgxpool.Pool
}

// NewRiskStateStore creates a new RiskStateStore.
func NewRiskStateStore(pool *pgxpool.Pool) *RiskStateStore {
	return &RiskStateStore{pool: pool}
}

// Upsert persists the current risk state.
func (s *RiskStateStore) Upsert(ctx context.Context, state RiskState) error {
	if state.ComputedAt.IsZero() {
		state.ComputedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO agent_risk_state (id, financial_instability_risk, overload_risk, execution_risk, strategy_concentration_risk, pricing_confidence_risk, computed_at)
		VALUES ('current', $1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE
			SET financial_instability_risk   = EXCLUDED.financial_instability_risk,
				overload_risk                = EXCLUDED.overload_risk,
				execution_risk               = EXCLUDED.execution_risk,
				strategy_concentration_risk   = EXCLUDED.strategy_concentration_risk,
				pricing_confidence_risk       = EXCLUDED.pricing_confidence_risk,
				computed_at                   = EXCLUDED.computed_at`
	_, err := s.pool.Exec(ctx, q,
		state.FinancialInstabilityRisk, state.OverloadRisk, state.ExecutionRisk,
		state.StrategyConcentrationRisk, state.PricingConfidenceRisk,
		state.ComputedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert risk state: %w", err)
	}
	return nil
}

// Get retrieves the current risk state.
func (s *RiskStateStore) Get(ctx context.Context) (RiskState, error) {
	const q = `
		SELECT financial_instability_risk, overload_risk, execution_risk,
			strategy_concentration_risk, pricing_confidence_risk, computed_at
		FROM agent_risk_state WHERE id = 'current'`
	var rs RiskState
	err := s.pool.QueryRow(ctx, q).Scan(
		&rs.FinancialInstabilityRisk, &rs.OverloadRisk, &rs.ExecutionRisk,
		&rs.StrategyConcentrationRisk, &rs.PricingConfidenceRisk,
		&rs.ComputedAt,
	)
	if err == pgx.ErrNoRows {
		return RiskState{}, nil
	}
	if err != nil {
		return RiskState{}, fmt.Errorf("get risk state: %w", err)
	}
	return rs, nil
}

// SummaryStore manages persistence for ObjectiveSummary (single-row UPSERT).
type SummaryStore struct {
	pool *pgxpool.Pool
}

// NewSummaryStore creates a new SummaryStore.
func NewSummaryStore(pool *pgxpool.Pool) *SummaryStore {
	return &SummaryStore{pool: pool}
}

// Upsert persists the current objective summary.
func (s *SummaryStore) Upsert(ctx context.Context, summary ObjectiveSummary) error {
	if summary.UpdatedAt.IsZero() {
		summary.UpdatedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO agent_objective_summary (id, utility_score, risk_score, net_utility, dominant_positive_factor, dominant_risk_factor, updated_at)
		VALUES ('current', $1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE
			SET utility_score            = EXCLUDED.utility_score,
				risk_score               = EXCLUDED.risk_score,
				net_utility              = EXCLUDED.net_utility,
				dominant_positive_factor = EXCLUDED.dominant_positive_factor,
				dominant_risk_factor     = EXCLUDED.dominant_risk_factor,
				updated_at               = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, q,
		summary.UtilityScore, summary.RiskScore, summary.NetUtility,
		summary.DominantPositiveFactor, summary.DominantRiskFactor,
		summary.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert objective summary: %w", err)
	}
	return nil
}

// Get retrieves the current objective summary.
func (s *SummaryStore) Get(ctx context.Context) (ObjectiveSummary, error) {
	const q = `
		SELECT utility_score, risk_score, net_utility, dominant_positive_factor, dominant_risk_factor, updated_at
		FROM agent_objective_summary WHERE id = 'current'`
	var sum ObjectiveSummary
	err := s.pool.QueryRow(ctx, q).Scan(
		&sum.UtilityScore, &sum.RiskScore, &sum.NetUtility,
		&sum.DominantPositiveFactor, &sum.DominantRiskFactor,
		&sum.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return ObjectiveSummary{}, nil
	}
	if err != nil {
		return ObjectiveSummary{}, fmt.Errorf("get objective summary: %w", err)
	}
	return sum, nil
}
