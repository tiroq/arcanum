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
}
