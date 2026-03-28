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
	ProposalApprovals  prometheus.Counter
	ProposalRejections prometheus.Counter
	WritebackSuccess   prometheus.Counter
	WritebackFailure   prometheus.Counter

	ProviderCalls    *prometheus.CounterVec
	ProviderFailures *prometheus.CounterVec
	TokensUsed       *prometheus.CounterVec

	OperationDuration *prometheus.HistogramVec
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
	}

	collectors := []prometheus.Collector{
		m.TasksFetched,
		m.TasksChanged,
		m.JobsCreated,
		m.JobsSucceeded,
		m.JobsFailed,
		m.JobsRetried,
		m.ProposalApprovals,
		m.ProposalRejections,
		m.WritebackSuccess,
		m.WritebackFailure,
		m.ProviderCalls,
		m.ProviderFailures,
		m.TokensUsed,
		m.OperationDuration,
	}

	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			return nil, err
		}
	}

	return m, nil
}
