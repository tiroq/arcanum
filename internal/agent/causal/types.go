package causal

import (
	"time"

	"github.com/google/uuid"
)

// Attribution classifies the likely cause of an observed change.
type Attribution string

const (
	AttributionInternal  Attribution = "internal"
	AttributionExternal  Attribution = "external"
	AttributionMixed     Attribution = "mixed"
	AttributionAmbiguous Attribution = "ambiguous"
)

// SubjectType identifies the kind of event being analyzed.
type SubjectType string

const (
	SubjectPolicyChange   SubjectType = "policy_change"
	SubjectStabilityEvent SubjectType = "stability_event"
	SubjectPlannerShift   SubjectType = "planner_shift"
)

// CausalAttribution is a single causal reasoning record.
type CausalAttribution struct {
	ID                    uuid.UUID      `json:"id"`
	SubjectType           SubjectType    `json:"subject_type"`
	SubjectID             uuid.UUID      `json:"subject_id"`
	Hypothesis            string         `json:"hypothesis"`
	Attribution           Attribution    `json:"attribution"`
	Confidence            float64        `json:"confidence"`
	Evidence              map[string]any `json:"evidence"`
	CompetingExplanations []string       `json:"competing_explanations"`
	CreatedAt             time.Time      `json:"created_at"`
}

// AnalysisInput bundles all signals needed for causal analysis.
// Collected once before running rules — rules never query the database.
type AnalysisInput struct {
	// Policy changes applied in the recent window.
	RecentPolicyChanges []PolicyChangeRecord
	// Current action memory stats.
	ActionMemory []ActionMemorySummary
	// Stability state at analysis time.
	StabilityMode string
	// Whether stability mode changed recently.
	StabilityChanged bool
	PreviousMode     string
	// External instability signals.
	ProviderInstability bool
	CycleInstability    bool
	HighSystemFailure   bool
	// How many simultaneous changes occurred recently.
	SimultaneousChanges int
	// Timestamp of analysis.
	Timestamp time.Time
}

// PolicyChangeRecord is a simplified view of a policy change for causal analysis.
type PolicyChangeRecord struct {
	ID                  uuid.UUID
	Parameter           string
	OldValue            float64
	NewValue            float64
	Applied             bool
	CreatedAt           time.Time
	ImprovementDetected *bool
}

// ActionMemorySummary is a simplified view of action memory for causal analysis.
type ActionMemorySummary struct {
	ActionType  string
	TotalRuns   int
	SuccessRate float64
	FailureRate float64
}

// AnalysisResult is the output of one causal analysis pass.
type AnalysisResult struct {
	Attributions []CausalAttribution `json:"attributions"`
	Analyzed     int                 `json:"analyzed"`
	Timestamp    time.Time           `json:"timestamp"`
}
