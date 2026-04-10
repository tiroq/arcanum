package reflection

import "time"

// Trigger evaluates whether a meta-reflection run should be initiated.
type Trigger struct {
	config TriggerConfig
	state  TriggerState
}

// NewTrigger creates a Trigger with the given configuration.
func NewTrigger(config TriggerConfig) *Trigger {
	return &Trigger{
		config: config,
		state: TriggerState{
			LastRunAt: time.Time{}, // zero = never run
		},
	}
}

// RecordAction increments the action counter since last run.
func (t *Trigger) RecordAction() {
	t.state.ActionsSinceRun++
}

// RecordFailure increments the failure counter since last run.
func (t *Trigger) RecordFailure() {
	t.state.FailuresSinceRun++
}

// UpdateIncome updates the latest verified income for event-based trigger.
func (t *Trigger) UpdateIncome(verified float64) {
	t.state.LastIncomeVerified = verified
}

// UpdatePressure updates the latest pressure level for event-based trigger.
func (t *Trigger) UpdatePressure(pressure float64) {
	t.state.LastPressure = pressure
}

// ShouldTrigger evaluates all trigger conditions and returns true if any fires.
func (t *Trigger) ShouldTrigger(now time.Time) bool {
	if t.shouldTriggerTimeBased(now) {
		return true
	}
	if t.shouldTriggerEventBased() {
		return true
	}
	if t.shouldTriggerAccumulative() {
		return true
	}
	return false
}

// Reset resets the trigger state after a successful run.
func (t *Trigger) Reset(now time.Time) {
	t.state.LastRunAt = now
	t.state.ActionsSinceRun = 0
	t.state.FailuresSinceRun = 0
}

// GetState returns the current trigger state (for inspection/testing).
func (t *Trigger) GetState() TriggerState {
	return t.state
}

// shouldTriggerTimeBased checks if enough time has passed since last run.
func (t *Trigger) shouldTriggerTimeBased(now time.Time) bool {
	if t.config.IntervalHours <= 0 {
		return false
	}
	if t.state.LastRunAt.IsZero() {
		return true // never run before
	}
	elapsed := now.Sub(t.state.LastRunAt).Hours()
	return elapsed >= t.config.IntervalHours
}

// shouldTriggerEventBased checks for event-based conditions.
func (t *Trigger) shouldTriggerEventBased() bool {
	// Failure spike
	if t.config.FailureSpikeCount > 0 && t.state.FailuresSinceRun >= t.config.FailureSpikeCount {
		return true
	}

	// Pressure threshold
	if t.config.PressureThreshold > 0 && t.state.LastPressure > t.config.PressureThreshold {
		return true
	}

	return false
}

// shouldTriggerAccumulative checks accumulated activity thresholds.
func (t *Trigger) shouldTriggerAccumulative() bool {
	if t.config.AccumActionCount > 0 && t.state.ActionsSinceRun >= t.config.AccumActionCount {
		return true
	}
	return false
}
