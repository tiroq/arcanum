package control

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/config"
)

// optimizerLookback is the default historical window used for analysis.
const optimizerLookback = 7 * 24 * time.Hour

// Minimum number of runs required before the optimizer emits a recommendation
// other than "keep_current_policy – insufficient data".
const thresholdMinSampleSize int64 = 5

// Failure rate thresholds.
const (
	thresholdRemoveFailureRate   = 0.50 // ≥50 % failures → remove provider
	thresholdEscalateFailureRate = 0.20 // ≥20 % failures → increase escalation
)

// Acceptance rate threshold below which escalation is recommended.
const thresholdEscalateLowAccept = 0.40

// Average latency (ms) above which escalation reduction is worth considering.
const thresholdHighLatencyMS = 30_000.0

// Acceptance rate above which a currently fast/local policy is considered
// healthy despite high latency.
const thresholdHealthyAcceptance = 0.70

// ──────────────────────────────────────────────────────────────────────────────
// Types
// ──────────────────────────────────────────────────────────────────────────────

// RecommendedAction classifies the optimizer's advisory output.
type RecommendedAction string

const (
	// ActionKeep indicates the current routing policy is performing acceptably.
	ActionKeep RecommendedAction = "keep_current_policy"
	// ActionReduceEscalation indicates a lighter/local model may maintain quality.
	ActionReduceEscalation RecommendedAction = "reduce_escalation"
	// ActionIncreaseEscalation indicates a stronger/cloud model is needed.
	ActionIncreaseEscalation RecommendedAction = "increase_escalation"
	// ActionRemoveProvider indicates the provider is consistently failing this role.
	ActionRemoveProvider RecommendedAction = "remove_provider_from_chain"
)

// RunMetrics holds aggregated execution data for a single (provider, model_role) pair.
// All rates are in the [0, 1] range.
type RunMetrics struct {
	Provider       string  `json:"provider"`
	ModelRole      string  `json:"model_role"`
	TotalRuns      int64   `json:"total_runs"`
	SuccessRuns    int64   `json:"success_runs"`
	FailureRuns    int64   `json:"failure_runs"`
	AcceptedRuns   int64   `json:"accepted_runs"`
	AcceptanceRate float64 `json:"acceptance_rate"`
	FailureRate    float64 `json:"failure_rate"`
	// FallbackRate is a proxy metric derived from failure_rate.
	// No explicit fallback flag is stored in the DB (the used_fallback boolean
	// lives only in the provider's runtime log). A failed run is treated as an
	// indicator that the role's assigned model was insufficient.
	FallbackRate   float64 `json:"fallback_rate"`
	AvgTokensTotal float64 `json:"avg_tokens_total"`
	AvgLatencyMS   float64 `json:"avg_latency_ms"`
}

// RoutingRecommendation is a structured, data-driven routing advisory.
// It is strictly read-only: it NEVER mutates configuration automatically.
// Consumers must apply suggested changes explicitly through the admin API or config.
type RoutingRecommendation struct {
	Role          string            `json:"role"`
	Provider      string            `json:"provider"`
	CurrentPolicy string            `json:"current_policy"`
	Metrics       RunMetrics        `json:"metrics"`
	Action        RecommendedAction `json:"action"`
	Explanation   string            `json:"explanation"`
}

// ──────────────────────────────────────────────────────────────────────────────
// OptimizerStore interface
// ──────────────────────────────────────────────────────────────────────────────

// OptimizerStore is the data access contract for the optimizer.
// Declared as an interface so production uses DBOptimizerStore and tests use
// a stub — no live database required for unit testing.
type OptimizerStore interface {
	// QueryRunMetrics returns aggregated per-(provider, model_role) execution
	// metrics from runs that started on or after since.
	QueryRunMetrics(ctx context.Context, since time.Time) ([]RunMetrics, error)
}

// ──────────────────────────────────────────────────────────────────────────────
// Optimizer
// ──────────────────────────────────────────────────────────────────────────────

// Optimizer reads historical execution data and produces routing recommendations.
// It is safe for concurrent use after construction.
type Optimizer struct {
	store   OptimizerStore
	routing config.RoutingPolicyConfig
	logger  *zap.Logger
}

// NewOptimizer creates an Optimizer.
func NewOptimizer(store OptimizerStore, routing config.RoutingPolicyConfig, logger *zap.Logger) *Optimizer {
	return &Optimizer{store: store, routing: routing, logger: logger}
}

// AnalyzeAndRecommend queries the last optimizerLookback window of run history
// and returns one RoutingRecommendation per (provider, model_role) pair found.
//
// The function is deterministic for a given dataset: the same metrics always
// produce the same recommendation. It performs only reads and emits advisory
// output — it never modifies config or routing policy.
func (o *Optimizer) AnalyzeAndRecommend(ctx context.Context) ([]RoutingRecommendation, error) {
	since := time.Now().UTC().Add(-optimizerLookback)

	metrics, err := o.store.QueryRunMetrics(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("optimizer: query run metrics: %w", err)
	}

	recs := make([]RoutingRecommendation, 0, len(metrics))
	for _, m := range metrics {
		rec := o.recommend(m)
		recs = append(recs, rec)
		o.logger.Info("optimizer: recommendation",
			zap.String("provider", rec.Provider),
			zap.String("role", rec.Role),
			zap.String("action", string(rec.Action)),
			zap.String("current_policy", rec.CurrentPolicy),
			zap.Int64("total_runs", m.TotalRuns),
			zap.Float64("acceptance_rate", m.AcceptanceRate),
			zap.Float64("failure_rate", m.FailureRate),
			zap.Float64("avg_latency_ms", m.AvgLatencyMS),
			zap.Float64("avg_tokens_total", m.AvgTokensTotal),
		)
	}

	return recs, nil
}

// recommend derives a single deterministic recommendation from observed metrics.
// Priority order (highest first): remove → escalate_failure → escalate_low_accept
//
//	→ reduce_latency → keep.
func (o *Optimizer) recommend(m RunMetrics) RoutingRecommendation {
	policy := o.policyFor(m.ModelRole)

	base := RoutingRecommendation{
		Role:          m.ModelRole,
		Provider:      m.Provider,
		CurrentPolicy: policy,
		Metrics:       m,
	}

	if m.TotalRuns < thresholdMinSampleSize {
		base.Action = ActionKeep
		base.Explanation = fmt.Sprintf(
			"Insufficient sample size (%d runs; minimum %d required). No recommendation issued.",
			m.TotalRuns, thresholdMinSampleSize,
		)
		return base
	}

	switch {
	case m.FailureRate >= thresholdRemoveFailureRate:
		base.Action = ActionRemoveProvider
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q has a %.1f%% failure rate over %d runs "+
				"(threshold: %.0f%%). Persistent failure at this level indicates "+
				"the provider is unreliable for this role.",
			m.Provider, m.ModelRole, m.FailureRate*100, m.TotalRuns,
			thresholdRemoveFailureRate*100,
		)

	case m.FailureRate >= thresholdEscalateFailureRate:
		base.Action = ActionIncreaseEscalation
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q has a %.1f%% failure rate over %d runs "+
				"(threshold: %.0f%%). Adding a cloud tier as fallback may reduce failures.",
			m.Provider, m.ModelRole, m.FailureRate*100, m.TotalRuns,
			thresholdEscalateFailureRate*100,
		)

	case m.AcceptanceRate < thresholdEscalateLowAccept:
		base.Action = ActionIncreaseEscalation
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q acceptance rate is %.1f%% "+
				"(threshold: %.0f%%) over %d runs. A stronger model or cloud tier "+
				"may produce higher-quality outputs.",
			m.Provider, m.ModelRole, m.AcceptanceRate*100,
			thresholdEscalateLowAccept*100, m.TotalRuns,
		)

	case m.AvgLatencyMS > thresholdHighLatencyMS &&
		m.AcceptanceRate >= thresholdHealthyAcceptance &&
		m.FailureRate < thresholdEscalateFailureRate:
		base.Action = ActionReduceEscalation
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q shows healthy acceptance (%.1f%%) and low failure rate (%.1f%%), "+
				"but average latency is %.0fms (threshold: %.0fms). "+
				"A lighter local model may maintain quality with lower latency.",
			m.Provider, m.ModelRole,
			m.AcceptanceRate*100, m.FailureRate*100,
			m.AvgLatencyMS, thresholdHighLatencyMS,
		)

	default:
		base.Action = ActionKeep
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q is performing within acceptable parameters: "+
				"failure rate %.1f%%, acceptance rate %.1f%%, avg latency %.0fms, "+
				"avg tokens %.0f over %d runs.",
			m.Provider, m.ModelRole,
			m.FailureRate*100, m.AcceptanceRate*100,
			m.AvgLatencyMS, m.AvgTokensTotal, m.TotalRuns,
		)
	}

	return base
}

// policyFor returns the configured escalation policy string for a model role.
// Returns "unknown" for unrecognised roles so recommendations remain readable.
func (o *Optimizer) policyFor(role string) string {
	switch role {
	case "fast":
		return o.routing.FastEscalation
	case "default":
		return o.routing.DefaultEscalation
	case "planner":
		return o.routing.PlannerEscalation
	case "review":
		return o.routing.ReviewEscalation
	default:
		return "unknown"
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DBOptimizerStore — production implementation
// ──────────────────────────────────────────────────────────────────────────────

// DBOptimizerStore is the production OptimizerStore backed by a Postgres pool.
type DBOptimizerStore struct {
	db *pgxpool.Pool
}

// NewDBOptimizerStore creates a DBOptimizerStore.
func NewDBOptimizerStore(db *pgxpool.Pool) *DBOptimizerStore {
	return &DBOptimizerStore{db: db}
}

// QueryRunMetrics aggregates per-(provider, model_role) execution metrics.
//
// Data sources:
//   - processing_runs: execution outcome, duration, token counts (via result_payload JSONB).
//   - suggestion_proposals: approval status joined on job_id (LEFT JOIN — not every
//     run produces a proposal).
//
// "fallback_runs" is not a first-class DB field; FallbackRate is computed as
// FailureRate since the provider's usedFallback flag is not persisted to the DB.
func (s *DBOptimizerStore) QueryRunMetrics(ctx context.Context, since time.Time) ([]RunMetrics, error) {
	const q = `
		SELECT
			pr.result_payload->>'provider'          AS provider,
			pr.result_payload->>'model_role'         AS model_role,
			COUNT(*)                                 AS total_runs,
			COUNT(*) FILTER (WHERE pr.outcome = 'success')   AS success_runs,
			COUNT(*) FILTER (WHERE pr.outcome != 'success')  AS failure_runs,
			COUNT(sp.id) FILTER (WHERE sp.approval_status = 'approved') AS accepted_runs,
			COALESCE(
				AVG((pr.result_payload->>'tokens_total')::NUMERIC), 0
			)                                        AS avg_tokens_total,
			COALESCE(AVG(pr.duration_ms), 0)         AS avg_latency_ms
		FROM processing_runs pr
		LEFT JOIN suggestion_proposals sp ON sp.job_id = pr.job_id
		WHERE pr.started_at >= $1
			AND pr.result_payload IS NOT NULL
			AND pr.result_payload->>'provider' IS NOT NULL
			AND pr.result_payload->>'provider' != ''
		GROUP BY
			pr.result_payload->>'provider',
			pr.result_payload->>'model_role'
		ORDER BY
			total_runs DESC,
			provider,
			model_role`

	rows, err := s.db.Query(ctx, q, since)
	if err != nil {
		return nil, fmt.Errorf("db query run metrics: %w", err)
	}
	defer rows.Close()

	var results []RunMetrics
	for rows.Next() {
		var m RunMetrics
		// result_payload JSONB fields may be NULL if an older run predates the
		// enrichment logic — use pointer scan to handle gracefully.
		var provider, role *string
		var avgTokens, avgLatency float64

		if err := rows.Scan(
			&provider, &role,
			&m.TotalRuns, &m.SuccessRuns, &m.FailureRuns, &m.AcceptedRuns,
			&avgTokens, &avgLatency,
		); err != nil {
			return nil, fmt.Errorf("scan run metrics row: %w", err)
		}

		if provider != nil {
			m.Provider = *provider
		}
		if role != nil {
			m.ModelRole = *role
		}
		m.AvgTokensTotal = avgTokens
		m.AvgLatencyMS = avgLatency

		if m.TotalRuns > 0 {
			m.FailureRate = float64(m.FailureRuns) / float64(m.TotalRuns)
			m.AcceptanceRate = float64(m.AcceptedRuns) / float64(m.TotalRuns)
			m.FallbackRate = m.FailureRate // proxy — see type comment
		}

		results = append(results, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run metrics rows: %w", err)
	}

	return results, nil
}
