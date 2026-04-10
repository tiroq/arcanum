package selfextension

import "time"

// --- Status Constants ---

const (
	// Proposal statuses (explicit state machine).
	StatusProposed   = "proposed"
	StatusRejected   = "rejected"
	StatusApproved   = "approved"
	StatusInProgress = "in_progress"
	StatusValidated  = "validated"
	StatusDeployed   = "deployed"

	// Sandbox execution results.
	ResultSuccess = "success"
	ResultFail    = "fail"

	// Deployment statuses.
	DeployActive     = "active"
	DeployRolledBack = "rolled_back"

	// Proposal sources.
	SourceDiscovery  = "discovery"
	SourceReflection = "reflection"
	SourceManual     = "manual"
	SourceFailure    = "failure_pattern"

	// Approval types.
	ApprovalManual    = "manual"
	ApprovalRuleBased = "rule_based"

	// Sandbox resource limits.
	SandboxDefaultTimeoutSec = 60
	SandboxMaxTimeoutSec     = 300
	SandboxDefaultMemoryMB   = 256
	SandboxMaxMemoryMB       = 1024
	SandboxDefaultCPUPercent = 50

	// MaxVersionsRetained is the maximum number of deployed versions kept per proposal.
	MaxVersionsRetained = 10
)

// ValidProposalSources lists allowed proposal source values.
var ValidProposalSources = []string{SourceDiscovery, SourceReflection, SourceManual, SourceFailure}

// ValidProposalStatuses defines the valid status values for proposals.
var ValidProposalStatuses = []string{
	StatusProposed, StatusRejected, StatusApproved,
	StatusInProgress, StatusValidated, StatusDeployed,
}

// ValidTransitions defines allowed state transitions for proposals.
// Key is the current status, value is the set of allowed next statuses.
var ValidTransitions = map[string][]string{
	StatusProposed:   {StatusApproved, StatusRejected},
	StatusApproved:   {StatusInProgress, StatusRejected},
	StatusInProgress: {StatusValidated, StatusRejected},
	StatusValidated:  {StatusDeployed, StatusRejected},
	// Terminal states: deployed, rejected — no further transitions.
}

// --- Entities ---

// ComponentProposal represents an intention to build a new component.
type ComponentProposal struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	Source             string    `json:"source"`
	GoalAlignmentScore float64   `json:"goal_alignment_score"`
	ExpectedValue      float64   `json:"expected_value"`
	EstimatedEffort    float64   `json:"estimated_effort"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ComponentSpec is a formal, deterministic description of what a component must do.
type ComponentSpec struct {
	ID               string    `json:"id"`
	ProposalID       string    `json:"proposal_id"`
	InputContract    string    `json:"input_contract"`
	OutputContract   string    `json:"output_contract"`
	Dependencies     []string  `json:"dependencies"`
	Constraints      []string  `json:"constraints"`
	TestRequirements []string  `json:"test_requirements"`
	CreatedAt        time.Time `json:"created_at"`
}

// SandboxRun records a single isolated execution of a component.
type SandboxRun struct {
	ID              string         `json:"id"`
	ProposalID      string         `json:"proposal_id"`
	Version         int            `json:"version"`
	ExecutionResult string         `json:"execution_result"`
	TestResults     []TestResult   `json:"test_results"`
	Logs            string         `json:"logs"`
	Metrics         SandboxMetrics `json:"metrics"`
	CreatedAt       time.Time      `json:"created_at"`
}

// TestResult captures a single test outcome within a sandbox run.
type TestResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Output string `json:"output,omitempty"`
}

// SandboxMetrics captures resource usage and correctness signals from a sandbox run.
type SandboxMetrics struct {
	LatencyMs      int64   `json:"latency_ms"`
	Errors         int     `json:"errors"`
	CorrectnessOK  bool    `json:"correctness_ok"`
	MemoryUsedMB   float64 `json:"memory_used_mb,omitempty"`
	CPUPercentUsed float64 `json:"cpu_percent_used,omitempty"`
}

// DeploymentRecord tracks a versioned deployment of a component.
type DeploymentRecord struct {
	ID                string    `json:"id"`
	ProposalID        string    `json:"proposal_id"`
	Version           int       `json:"version"`
	Status            string    `json:"status"`
	DeployedAt        time.Time `json:"deployed_at"`
	RollbackAvailable bool      `json:"rollback_available"`
}

// RollbackPoint enables restoring a previous working version.
type RollbackPoint struct {
	ID              string    `json:"id"`
	DeploymentID    string    `json:"deployment_id"`
	PreviousVersion int       `json:"previous_version"`
	CreatedAt       time.Time `json:"created_at"`
}

// SandboxConfig defines resource constraints for sandbox execution.
type SandboxConfig struct {
	TimeoutSec   int  `json:"timeout_sec"`
	MemoryMB     int  `json:"memory_mb"`
	CPUPercent   int  `json:"cpu_percent"`
	NetworkAllow bool `json:"network_allow"`
}

// DefaultSandboxConfig returns a restrictive default sandbox configuration.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		TimeoutSec:   SandboxDefaultTimeoutSec,
		MemoryMB:     SandboxDefaultMemoryMB,
		CPUPercent:   SandboxDefaultCPUPercent,
		NetworkAllow: false,
	}
}

// ApprovalDecision records who approved or rejected a deployment.
type ApprovalDecision struct {
	ProposalID   string    `json:"proposal_id"`
	ApprovedBy   string    `json:"approved_by"`
	ApprovalType string    `json:"approval_type"`
	Approved     bool      `json:"approved"`
	Reason       string    `json:"reason,omitempty"`
	DecidedAt    time.Time `json:"decided_at"`
}

// CodeArtifact represents stored, versioned generated code.
type CodeArtifact struct {
	ID         string    `json:"id"`
	ProposalID string    `json:"proposal_id"`
	Version    int       `json:"version"`
	Language   string    `json:"language"`
	Source     string    `json:"source"`
	Content    string    `json:"content"`
	Checksum   string    `json:"checksum"`
	CreatedAt  time.Time `json:"created_at"`
}

// IsValidSource returns true if the given source string is a known proposal source.
func IsValidSource(s string) bool {
	for _, v := range ValidProposalSources {
		if v == s {
			return true
		}
	}
	return false
}

// IsValidTransition returns true if the status transition is allowed.
func IsValidTransition(from, to string) bool {
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
