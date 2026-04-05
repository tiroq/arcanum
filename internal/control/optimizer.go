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

// Fallback rate threshold above which escalation to a better model should be
// considered, even when hard failures are below the escalation threshold.
const thresholdEscalateFallbackRate = 0.30

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

// DataQuality describes the signal quality used to produce a recommendation.
// Callers should treat recommendations backed by proxy signals with lower
// confidence than those backed by real stored signals.
type DataQuality struct {
	// HasRealFallbackSignal is true when at least one run in the window has a
	// stored used_fallback field in result_payload (populated by the runner
	// since the v2 enrichment). False means the FallbackRate is a proxy
	// derived from FailureRate.
	HasRealFallbackSignal bool `json:"has_real_fallback_signal"`
	// HasRealAcceptanceSignal is true when at least one proposal with a
	// terminal approval status (approved or rejected) exists for jobs in
	// the analysis window.
	HasRealAcceptanceSignal bool `json:"has_real_acceptance_signal"`
	// SampleSize is the number of runs in the analysis window for this pair.
	SampleSize int64 `json:"sample_size"`
}

// RunMetrics holds aggregated execution data for a single (provider, model_role) pair.
// All rates are in the [0, 1] range.
type RunMetrics struct {
	Provider     string `json:"provider"`
	ModelRole    string `json:"model_role"`
	TotalRuns    int64  `json:"total_runs"`
	SuccessRuns  int64  `json:"success_runs"`
	FailureRuns  int64  `json:"failure_runs"`
	AcceptedRuns int64  `json:"accepted_runs"`
	// RejectedRuns counts proposals explicitly rejected (approval_status = 'rejected').
	// Distinguished from "no proposal yet" (still pending or no proposal created).
	RejectedRuns int64   `json:"rejected_runs"`
	AcceptanceRate float64 `json:"acceptance_rate"`
	// RejectionRate is explicit rejections / total runs (not inversely derived from acceptance).
	RejectionRate float64 `json:"rejection_rate"`
	FailureRate   float64 `json:"failure_rate"`
	// FallbackRuns is the count of runs where the provider used a fallback model.
	// When HasRealFallbackSignal is true, this counts real stored used_fallback=true entries.
	// When false, it mirrors FailureRuns as a proxy (see DataQuality).
	FallbackRuns int64   `json:"fallback_runs"`
	FallbackRate float64 `json:"fallback_rate"`
	// AvgAttemptNumber is the average attempt_number stored in result_payload.
	// Values > 1 indicate that many executions were retries, a sign of instability.
	AvgAttemptNumber float64 `json:"avg_attempt_number"`
	AvgTokensTotal   float64 `json:"avg_tokens_total"`
	AvgLatencyMS     float64 `json:"avg_latency_ms"`
	Quality          DataQuality `json:"data_quality"`
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
	// DataQuality is a copy of Metrics.Quality exposed at the top level for
	// easy inspection without navigating the full metrics object.
	DataQuality DataQuality `json:"data_quality"`
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
			zap.Float64("rejection_rate", m.RejectionRate),
			zap.Float64("failure_rate", m.FailureRate),
			zap.Float64("fallback_rate", m.FallbackRate),
			zap.Bool("real_fallback_signal", m.Quality.HasRealFallbackSignal),
			zap.Bool("real_acceptance_signal", m.Quality.HasRealAcceptanceSignal),
			zap.Float64("avg_latency_ms", m.AvgLatencyMS),
			zap.Float64("avg_tokens_total", m.AvgTokensTotal),
			zap.Float64("avg_attempt_number", m.AvgAttemptNumber),
		)
	}

	return recs, nil
}

// recommend derives a single deterministic recommendation from observed metrics.
//
// Priority order (highest first):
//  1. remove     — failure_rate ≥ 50 %
//  2. escalate   — failure_rate ≥ 20 %
//  3. escalate   — fallback_rate ≥ 30 % (real signal preferred over proxy)
//  4. escalate   — acceptance_rate < 40 % (real signal preferred)
//  5. reduce     — high latency + healthy acceptance + low failure
//  6. keep       — everything within acceptable parameters
func (o *Optimizer) recommend(m RunMetrics) RoutingRecommendation {
	policy := o.policyFor(m.ModelRole)

	base := RoutingRecommendation{
		Role:          m.ModelRole,
		Provider:      m.Provider,
		CurrentPolicy: policy,
		Metrics:       m,
		DataQuality:   m.Quality,
	}

	if m.TotalRuns < thresholdMinSampleSize {
		base.Action = ActionKeep
		base.Explanation = fmt.Sprintf(
			"Insufficient data: %d run(s) available; minimum %d required before "+
				"routing changes can be advised. "+
				"[acceptance_signal=%s fallback_signal=%s]",
			m.TotalRuns, thresholdMinSampleSize,
			signalLabel(m.Quality.HasRealAcceptanceSignal),
			signalLabel(m.Quality.HasRealFallbackSignal),
		)
		return base
	}

	// Build a human-readable signal-source suffix appended to explanations.
	signalSuffix := fmt.Sprintf(
		"[acceptance_signal=%s fallback_signal=%s sample=%d]",
		signalLabel(m.Quality.HasRealAcceptanceSignal),
		signalLabel(m.Quality.HasRealFallbackSignal),
		m.TotalRuns,
	)

	// Derive the effective fallback rate label for explanations.
	fallbackLabel := "inferred"
	if m.Quality.HasRealFallbackSignal {
		fallbackLabel = "measured"
	}

	switch {
	case m.FailureRate >= thresholdRemoveFailureRate:
		base.Action = ActionRemoveProvider
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q has a %.1f%% failure rate over %d runs "+
				"(threshold: %.0f%%). Persistent failure at this level indicates "+
				"the provider is unreliable for this role. %s",
			m.Provider, m.ModelRole, m.FailureRate*100, m.TotalRuns,
			thresholdRemoveFailureRate*100, signalSuffix,
		)

	case m.FailureRate >= thresholdEscalateFailureRate:
		base.Action = ActionIncreaseEscalation
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q has a %.1f%% failure rate over %d runs "+
				"(threshold: %.0f%%). Adding a cloud tier as fallback may reduce failures. %s",
			m.Provider, m.ModelRole, m.FailureRate*100, m.TotalRuns,
			thresholdEscalateFailureRate*100, signalSuffix,
		)

	case m.FallbackRate >= thresholdEscalateFallbackRate:
		base.Action = ActionIncreaseEscalation
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q has a %s fallback rate of %.1f%% over %d runs "+
				"(threshold: %.0f%%). The assigned model for this role is frequently "+
				"unavailable or unset; a dedicated role-specific model is recommended. %s",
			m.Provider, m.ModelRole, fallbackLabel, m.FallbackRate*100, m.TotalRuns,
			thresholdEscalateFallbackRate*100, signalSuffix,
		)

	case m.AcceptanceRate < thresholdEscalateLowAccept &&
		m.Quality.HasRealAcceptanceSignal:
		// Only use acceptance signal to escalate when we have real approved/rejected data.
		// Without real signal, low acceptance could just mean proposals are still pending.
		base.Action = ActionIncreaseEscalation
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q has a real acceptance rate of %.1f%% "+
				"(threshold: %.0f%%) over %d runs. "+
				"A stronger model or cloud tier may produce higher-quality outputs. %s",
			m.Provider, m.ModelRole, m.AcceptanceRate*100,
			thresholdEscalateLowAccept*100, m.TotalRuns, signalSuffix,
		)

	case m.AvgLatencyMS > thresholdHighLatencyMS &&
		m.AcceptanceRate >= thresholdHealthyAcceptance &&
		m.FailureRate < thresholdEscalateFailureRate &&
		m.FallbackRate < thresholdEscalateFallbackRate:
		base.Action = ActionReduceEscalation
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q shows healthy acceptance (%.1f%%) and low failure rate (%.1f%%), "+
				"but average latency is %.0fms (threshold: %.0fms). "+
				"A lighter local model may maintain quality with lower latency. %s",
			m.Provider, m.ModelRole,
			m.AcceptanceRate*100, m.FailureRate*100,
			m.AvgLatencyMS, thresholdHighLatencyMS, signalSuffix,
		)

	default:
		base.Action = ActionKeep
		base.Explanation = fmt.Sprintf(
			"Provider %q / role %q is performing within acceptable parameters: "+
				"failure rate %.1f%%, fallback rate %.1f%% (%s), acceptance rate %.1f%%, "+
				"avg latency %.0fms, avg tokens %.0f, avg attempt %.1f over %d runs. %s",
			m.Provider, m.ModelRole,
			m.FailureRate*100, m.FallbackRate*100, fallbackLabel,
			m.AcceptanceRate*100,
			m.AvgLatencyMS, m.AvgTokensTotal, m.AvgAttemptNumber,
			m.TotalRuns, signalSuffix,
		)
	}

	return base
}

// signalLabel returns a human-readable label for signal quality in explanations.
func signalLabel(real bool) string {
	if real {
		return "real"
	}
	return "proxy"
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
//   - processing_runs: execution outcome, duration, token counts, and real
//     fallback signal (result_payload->>'used_fallback') stored by the runner
//     since the v2 enrichment.
//   - suggestion_proposals: explicit approval outcome (approved / rejected)
//     joined on job_id. LEFT JOIN because not every run produces a proposal.
//
// Signal quality:
//   - HasRealFallbackSignal is true when at least one run in the window has
//     a non-NULL result_payload->>'used_fallback', meaning it was written by
//     a runner version that persists the field.
//   - HasRealAcceptanceSignal is true when at least one proposal with a
//     terminal status (approved or rejected) exists in the window.
//   - When real signals are unavailable, FallbackRuns = FailureRuns (proxy).
func (s *DBOptimizerStore) QueryRunMetrics(ctx context.Context, since time.Time) ([]RunMetrics, error) {
	const q = `
		SELECT
			pr.result_payload->>'provider'                          AS provider,
			pr.result_payload->>'model_role'                        AS model_role,
			COUNT(*)                                                AS total_runs,
			COUNT(*) FILTER (WHERE pr.outcome = 'success')          AS success_runs,
			COUNT(*) FILTER (WHERE pr.outcome != 'success')         AS failure_runs,
			-- Real acceptance signal: proposals explicitly approved.
			COUNT(sp.id) FILTER (WHERE sp.approval_status = 'approved')  AS accepted_runs,
			-- Real rejection signal: proposals explicitly rejected.
			COUNT(sp.id) FILTER (WHERE sp.approval_status = 'rejected')  AS rejected_runs,
			-- Real fallback signal: persisted used_fallback=true in result_payload.
			COUNT(*) FILTER (
				WHERE (pr.result_payload->>'used_fallback')::BOOLEAN = true
			)                                                       AS fallback_runs,
			-- Signal quality flags.
			-- has_real_fallback_signal: true when at least one run in this group
			-- has the used_fallback field present in result_payload.
			BOOL_OR(pr.result_payload ? 'used_fallback')            AS has_real_fallback_signal,
			-- has_real_acceptance_signal: true when at least one terminal proposal exists.
			BOOL_OR(sp.approval_status IN ('approved', 'rejected'))  AS has_real_acceptance_signal,
			COALESCE(
				AVG((pr.result_payload->>'tokens_total')::NUMERIC), 0
			)                                                       AS avg_tokens_total,
			COALESCE(AVG(pr.duration_ms), 0)                        AS avg_latency_ms,
			COALESCE(
				AVG((pr.result_payload->>'attempt_number')::NUMERIC), 1
			)                                                       AS avg_attempt_number
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
		// JSONB text fields may be NULL for runs that predate the enrichment logic.
		var provider, role *string
		var avgTokens, avgLatency, avgAttempt float64
		var hasRealFallback, hasRealAcceptance bool

		if err := rows.Scan(
			&provider, &role,
			&m.TotalRuns, &m.SuccessRuns, &m.FailureRuns,
			&m.AcceptedRuns, &m.RejectedRuns, &m.FallbackRuns,
			&hasRealFallback, &hasRealAcceptance,
			&avgTokens, &avgLatency, &avgAttempt,
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
		m.AvgAttemptNumber = avgAttempt

		m.Quality = DataQuality{
			HasRealFallbackSignal:   hasRealFallback,
			HasRealAcceptanceSignal: hasRealAcceptance,
			SampleSize:              m.TotalRuns,
		}

		if m.TotalRuns > 0 {
			m.FailureRate = float64(m.FailureRuns) / float64(m.TotalRuns)
			m.AcceptanceRate = float64(m.AcceptedRuns) / float64(m.TotalRuns)
			m.RejectionRate = float64(m.RejectedRuns) / float64(m.TotalRuns)

			if hasRealFallback {
				// Use real stored signal.
				m.FallbackRate = float64(m.FallbackRuns) / float64(m.TotalRuns)
			} else {
				// Fall back to failure-rate proxy and mirror FallbackRuns.
				m.FallbackRuns = m.FailureRuns
				m.FallbackRate = m.FailureRate
			}
		}

		results = append(results, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run metrics rows: %w", err)
	}

	return results, nil
}
