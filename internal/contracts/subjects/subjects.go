package subjects

// NATS subject constants for the Runeforge platform.
// This is the single authoritative source of all subject strings.
// Business logic MUST import these constants; hardcoding subject strings elsewhere is forbidden.
const (
	// SubjectWildcard matches all runeforge subjects. Used for JetStream stream configuration.
	SubjectWildcard = "runeforge.>"

	SubjectSourceTaskDetected = "runeforge.source.task.detected"
	SubjectSourceTaskChanged  = "runeforge.source.task.changed"
	SubjectJobCreated         = "runeforge.job.created"
	SubjectJobRetry           = "runeforge.job.retry"
	SubjectJobDead            = "runeforge.job.dead"
	SubjectProposalCreated    = "runeforge.proposal.created"
	SubjectProposalApproved   = "runeforge.proposal.approved"
	SubjectWritebackRequested = "runeforge.writeback.requested"
	SubjectWritebackCompleted = "runeforge.writeback.completed"
	SubjectWritebackFailed    = "runeforge.writeback.failed"
	SubjectNotifyRequested    = "runeforge.notify.requested"

	// Command subjects — API publishes; orchestrator subscribes and executes.
	SubjectCommandTaskResync = "runeforge.commands.task.resync"
	SubjectCommandJobRetry   = "runeforge.commands.job.retry"

	// Control alert subjects — control loop publishes when anomalies are detected.
	SubjectControlAlertLeaseExpired = "runeforge.control.alert.lease_expired"
	SubjectControlAlertRetryOverdue = "runeforge.control.alert.retry_overdue"
	SubjectControlAlertQueueBacklog = "runeforge.control.alert.queue_backlog"
	SubjectControlAlertLeaseLost    = "runeforge.control.alert.lease_lost"

	// Control result subjects — control loop publishes when recovery actions complete.
	SubjectControlResultReclaimCompleted      = "runeforge.control.result.reclaim_completed"
	SubjectControlResultRetryRequeueCompleted = "runeforge.control.result.retry_requeue_completed"
)

// AllSubjects lists every defined NATS subject. Used in enforcement tests.
var AllSubjects = []string{
	SubjectSourceTaskDetected,
	SubjectSourceTaskChanged,
	SubjectJobCreated,
	SubjectJobRetry,
	SubjectJobDead,
	SubjectProposalCreated,
	SubjectProposalApproved,
	SubjectWritebackRequested,
	SubjectWritebackCompleted,
	SubjectWritebackFailed,
	SubjectNotifyRequested,
	SubjectCommandTaskResync,
	SubjectCommandJobRetry,
	SubjectControlAlertLeaseExpired,
	SubjectControlAlertRetryOverdue,
	SubjectControlAlertQueueBacklog,
	SubjectControlAlertLeaseLost,
	SubjectControlResultReclaimCompleted,
	SubjectControlResultRetryRequeueCompleted,
}
