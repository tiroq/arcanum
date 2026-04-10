package capacity

import "time"

// --- Constants ---

const (
	// CapacityPenaltyMax is the maximum penalty applied to paths in the
	// decision graph based on capacity constraints. Bounds influence at 15%.
	CapacityPenaltyMax = 0.15

	// CapacityBoostMax is the maximum boost for small, high-value tasks
	// when capacity is limited. Bounds influence at 10%.
	CapacityBoostMax = 0.10

	// MinimumEffortFloor prevents division by zero in value-per-hour.
	// Clamps estimated_effort to a minimum of 0.5 hours.
	MinimumEffortFloor = 0.5

	// OverloadThreshold is the owner_load_score above which overload
	// penalties are applied. Range [0,1].
	OverloadThreshold = 0.60

	// SmallTaskThreshold is the maximum effort (hours) for a task to be
	// considered "small" and eligible for capacity boost.
	SmallTaskThreshold = 2.0

	// HighValuePerHourThreshold is the minimum value-per-hour for a task
	// to be considered "high value". Used for boost eligibility.
	HighValuePerHourThreshold = 50.0

	// DefaultMaxDailyWorkHours is the fallback if family_context is missing.
	DefaultMaxDailyWorkHours = 8.0

	// DefaultMinFamilyTimeHours is the fallback if family_context is missing.
	DefaultMinFamilyTimeHours = 2.0

	// OverloadPenaltyWeight controls how much owner_load_score above
	// OverloadThreshold reduces available capacity. Range [0,1].
	// penalty = (load - threshold) / (1 - threshold) * weight * base
	OverloadPenaltyWeight = 0.50

	// RecommendThreshold is the minimum capacity_fit_score for an item
	// to be recommended (not deferred).
	RecommendThreshold = 0.40
)

// --- Entities ---

// CapacityState represents the owner's current time availability.
type CapacityState struct {
	AvailableHoursToday float64   `json:"available_hours_today"`
	AvailableHoursWeek  float64   `json:"available_hours_week"`
	BlockedHoursToday   float64   `json:"blocked_hours_today"`
	OwnerLoadScore      float64   `json:"owner_load_score"`
	MaxDailyWorkHours   float64   `json:"max_daily_work_hours"`
	MinFamilyTimeHours  float64   `json:"min_family_time_hours"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// CapacityItem is a generic item to evaluate against capacity.
type CapacityItem struct {
	ItemType        string  `json:"item_type"` // opportunity | proposal | task
	ItemID          string  `json:"item_id"`
	EstimatedEffort float64 `json:"estimated_effort"` // hours
	ExpectedValue   float64 `json:"expected_value"`   // USD or abstract value
	Urgency         float64 `json:"urgency"`          // [0,1] — 0 = not urgent, 1 = critical
}

// CapacityDecision is the result of evaluating a single item against capacity.
type CapacityDecision struct {
	ID               string    `json:"id"`
	ItemType         string    `json:"item_type"`
	ItemID           string    `json:"item_id"`
	EstimatedEffort  float64   `json:"estimated_effort"`
	ExpectedValue    float64   `json:"expected_value"`
	ValuePerHour     float64   `json:"value_per_hour"`
	CapacityFitScore float64   `json:"capacity_fit_score"`
	Recommended      bool      `json:"recommended"`
	DeferReason      string    `json:"defer_reason,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// CapacitySummary is an aggregate view of the most recent evaluation.
type CapacitySummary struct {
	TotalItemsEvaluated int       `json:"total_items_evaluated"`
	RecommendedCount    int       `json:"recommended_count"`
	DeferredCount       int       `json:"deferred_count"`
	TotalEstimatedHours float64   `json:"total_estimated_hours"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// FamilyConfig holds time/family constraints loaded from family_context.yaml.
type FamilyConfig struct {
	MaxDailyWorkHours  float64       `json:"max_daily_work_hours"`
	MinFamilyTimeHours float64       `json:"min_family_time_hours"`
	StressLimit        string        `json:"stress_limit"`
	BlockedRanges      []BlockedTime `json:"blocked_ranges"`
	WorkingWindows     []string      `json:"working_windows"`
}

// BlockedTime represents a time range blocked from work.
type BlockedTime struct {
	Reason string `json:"reason"`
	Range  string `json:"range"` // "HH:MM-HH:MM"
}
