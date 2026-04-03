package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the Runeforge platform.
type Metrics struct {
	TasksFetched       prometheus.Counter
	TasksChanged       prometheus.Counter
	JobsCreated        prometheus.Counter
	JobsSucceeded      prometheus.Counter
	JobsFailed         prometheus.Counter
	JobsRetried        prometheus.Counter
	JobsReclaimed      prometheus.Counter
	ProposalApprovals  prometheus.Counter
	ProposalRejections prometheus.Counter
	WritebackSuccess   prometheus.Counter
	WritebackFailure   prometheus.Counter

	ProviderCalls    *prometheus.CounterVec
	ProviderFailures *prometheus.CounterVec
	TokensUsed       *prometheus.CounterVec

	OperationDuration *prometheus.HistogramVec

	ExecutionCandidatesTried    *prometheus.CounterVec
	ExecutionFallbacksTotal     *prometheus.CounterVec
	ExecutionOutcomeTotal       *prometheus.CounterVec
	ExecutionDuration           *prometheus.HistogramVec
	ExecutionValidationFailures *prometheus.CounterVec
}

// NewMetrics creates and registers all Prometheus metrics with the given registry.
func NewMetrics(registry *prometheus.Registry) (*Metrics, error) {
	m := &Metrics{
		TasksFetched: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "tasks_fetched_total",
			Help:      "Total number of source tasks fetched.",
		}),
		TasksChanged: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "tasks_changed_total",
			Help:      "Total number of source tasks that changed.",
		}),
		JobsCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "jobs_created_total",
			Help:      "Total number of processing jobs created.",
		}),
		JobsSucceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "jobs_succeeded_total",
			Help:      "Total number of processing jobs that succeeded.",
		}),
		JobsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "jobs_failed_total",
			Help:      "Total number of processing jobs that failed.",
		}),
		JobsRetried: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "jobs_retried_total",
			Help:      "Total number of processing job retries.",
		}),
		JobsReclaimed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "jobs_reclaimed_total",
			Help:      "Total number of expired-lease jobs reclaimed.",
		}),
		ProposalApprovals: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "proposal_approvals_total",
			Help:      "Total number of proposals approved.",
		}),
		ProposalRejections: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "proposal_rejections_total",
			Help:      "Total number of proposals rejected.",
		}),
		WritebackSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "writeback_success_total",
			Help:      "Total number of successful writeback operations.",
		}),
		WritebackFailure: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "writeback_failure_total",
			Help:      "Total number of failed writeback operations.",
		}),
		ProviderCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "provider_calls_total",
			Help:      "Total number of AI provider calls.",
		}, []string{"provider"}),
		ProviderFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "provider_failures_total",
			Help:      "Total number of AI provider call failures.",
		}, []string{"provider"}),
		TokensUsed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "tokens_used_total",
			Help:      "Total number of tokens consumed by AI providers.",
		}, []string{"provider"}),
		OperationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "runeforge",
			Name:      "operation_duration_seconds",
			Help:      "Duration of operations in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"operation"}),
		ExecutionCandidatesTried: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "execution_candidates_tried_total",
			Help:      "Total number of candidates attempted per execution chain.",
		}, []string{"role"}),
		ExecutionFallbacksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "execution_fallbacks_total",
			Help:      "Total number of fallbacks to next candidate in execution chains.",
		}, []string{"role", "failure_class"}),
		ExecutionOutcomeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "execution_outcome_total",
			Help:      "Total executions by outcome (success, fallback, exhausted, aborted).",
		}, []string{"role", "outcome"}),
		ExecutionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "runeforge",
			Name:      "execution_duration_seconds",
			Help:      "Duration of full candidate chain execution in seconds.",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{"role"}),
		ExecutionValidationFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "runeforge",
			Name:      "execution_validation_failures_total",
			Help:      "Total validation failures during execution.",
		}, []string{"role", "validator"}),
	}

	collectors := []prometheus.Collector{
		m.TasksFetched,
		m.TasksChanged,
		m.JobsCreated,
		m.JobsSucceeded,
		m.JobsFailed,
		m.JobsRetried,
		m.JobsReclaimed,
		m.ProposalApprovals,
		m.ProposalRejections,
		m.WritebackSuccess,
		m.WritebackFailure,
		m.ProviderCalls,
		m.ProviderFailures,
		m.TokensUsed,
		m.OperationDuration,
		m.ExecutionCandidatesTried,
		m.ExecutionFallbacksTotal,
		m.ExecutionOutcomeTotal,
		m.ExecutionDuration,
		m.ExecutionValidationFailures,
	}

	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			return nil, err
		}
	}

	return m, nil
}
