package scheduling

import "time"

// --- Constants ---

const (
	// SlotDurationHours is the default granularity of schedule slots (1 hour).
	SlotDurationHours = 1.0

	// MaxSlotsPerDay limits the number of generated slots per day.
	MaxSlotsPerDay = 16

	// MaxCandidatesPerBatch limits how many candidates are scored at once.
	MaxCandidatesPerBatch = 50

	// FitScoreThreshold is the minimum fit score for a slot to be recommended.
	FitScoreThreshold = 0.25

	// WeightValuePerHour is the scoring weight for value component (35%).
	WeightValuePerHour = 0.35

	// WeightUrgency is the scoring weight for urgency component (25%).
	WeightUrgency = 0.25

	// WeightEffortFit is the scoring weight for effort fit component (25%).
	WeightEffortFit = 0.25

	// WeightLoadPenalty is the scoring weight for owner load penalty (15%).
	WeightLoadPenalty = 0.15

	// HighValuePerHourThreshold normalises value-per-hour into [0,1].
	HighValuePerHourThreshold = 100.0

	// StrategyPriorityBoostMax is the maximum boost from strategy priority (10%).
	StrategyPriorityBoostMax = 0.10

	// DefaultWorkStartHour is the fallback work start hour (09:00).
	DefaultWorkStartHour = 9

	// DefaultWorkEndHour is the fallback work end hour (18:00).
	DefaultWorkEndHour = 18

	// MaxDaysAhead limits how far ahead slots can be generated.
	MaxDaysAhead = 7
)

// --- Slot Types ---

const (
	SlotTypeWork          = "work"
	SlotTypeFamilyBlocked = "family_blocked"
	SlotTypeBuffer        = "buffer"
	SlotTypeMeeting       = "meeting"
)

// --- Item Types ---

const (
	ItemTypeRevenue     = "revenue"
	ItemTypeAssistance  = "assistance"
	ItemTypeExtension   = "extension"
	ItemTypeMaintenance = "maintenance"
)

// --- Decision Statuses ---

const (
	DecisionStatusProposed  = "proposed"
	DecisionStatusApproved  = "approved"
	DecisionStatusScheduled = "scheduled"
	DecisionStatusRejected  = "rejected"
)

// ValidDecisionStatuses defines allowed statuses.
var ValidDecisionStatuses = []string{
	DecisionStatusProposed,
	DecisionStatusApproved,
	DecisionStatusScheduled,
	DecisionStatusRejected,
}

// ValidDecisionTransitions defines allowed state transitions.
var ValidDecisionTransitions = map[string][]string{
	DecisionStatusProposed:  {DecisionStatusApproved, DecisionStatusRejected},
	DecisionStatusApproved:  {DecisionStatusScheduled, DecisionStatusRejected},
	DecisionStatusScheduled: {DecisionStatusRejected}, // allow post-schedule cancellation
}

// IsValidDecisionTransition checks whether from→to is allowed.
func IsValidDecisionTransition(from, to string) bool {
	allowed, ok := ValidDecisionTransitions[from]
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

// --- Calendar Record Statuses ---

const (
	CalendarStatusPending  = "pending"
	CalendarStatusCreated  = "created"
	CalendarStatusFailed   = "failed"
	CalendarStatusDryRun   = "dry_run"
	CalendarStatusCanceled = "canceled"
)

// --- Entities ---

// ScheduleSlot is a concrete time window with availability status.
type ScheduleSlot struct {
	ID        string    `json:"id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	SlotType  string    `json:"slot_type"`
	Available bool      `json:"available"`
	DayOfWeek string    `json:"day_of_week"`
	CreatedAt time.Time `json:"created_at"`
}

// DurationHours returns the length of the slot in hours.
func (s ScheduleSlot) DurationHours() float64 {
	return s.EndTime.Sub(s.StartTime).Hours()
}

// SchedulingCandidate is a task/opportunity paired with scheduling metadata.
type SchedulingCandidate struct {
	ID                   string    `json:"id"`
	ItemType             string    `json:"item_type"`
	ItemID               string    `json:"item_id"`
	EstimatedEffortHours float64   `json:"estimated_effort_hours"`
	Urgency              float64   `json:"urgency"`                     // [0,1]
	ExpectedValue        float64   `json:"expected_value"`              // USD or abstract
	PreferredWindow      string    `json:"preferred_window,omitempty"`  // "morning", "afternoon", ""
	StrategyPriority     float64   `json:"strategy_priority,omitempty"` // [0,1] from portfolio
	CreatedAt            time.Time `json:"created_at"`
}

// ScheduleDecision is the system's recommendation for when to place work.
type ScheduleDecision struct {
	ID             string    `json:"id"`
	CandidateID    string    `json:"candidate_id"`
	ChosenSlotID   string    `json:"chosen_slot_id"`
	FitScore       float64   `json:"fit_score"`
	RequiresReview bool      `json:"requires_review"`
	ReviewReason   string    `json:"review_reason,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// CalendarRecord tracks external calendar actions linked to schedule decisions.
type CalendarRecord struct {
	ID                 string    `json:"id"`
	DecisionID         string    `json:"decision_id"`
	ExternalCalendarID string    `json:"external_calendar_id,omitempty"`
	EventRef           string    `json:"event_ref,omitempty"`
	Status             string    `json:"status"`
	ErrorMessage       string    `json:"error_message,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

// SlotScore pairs a slot with a computed fit score for a candidate.
type SlotScore struct {
	Slot     ScheduleSlot `json:"slot"`
	FitScore float64      `json:"fit_score"`
}

// ScheduleRecommendation is the output of the recommendation engine.
type ScheduleRecommendation struct {
	CandidateID    string      `json:"candidate_id"`
	BestSlot       *SlotScore  `json:"best_slot,omitempty"`
	AlternateSlots []SlotScore `json:"alternate_slots,omitempty"`
	RequiresReview bool        `json:"requires_review"`
	ReviewReason   string      `json:"review_reason,omitempty"`
	NoValidSlots   bool        `json:"no_valid_slots"`
}

// SlotGenerationConfig carries parameters for slot generation.
type SlotGenerationConfig struct {
	MaxDailyWorkHours  float64
	MinFamilyTimeHours float64
	BlockedRanges      []BlockedRange
	WorkingWindows     []string // "HH:MM-HH:MM"
	OwnerLoadScore     float64
	Date               time.Time
	DaysAhead          int
}

// BlockedRange represents a blocked time window.
type BlockedRange struct {
	Reason string `json:"reason"`
	Range  string `json:"range"` // "HH:MM-HH:MM"
}
