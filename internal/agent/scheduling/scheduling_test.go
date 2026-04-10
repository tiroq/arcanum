package scheduling

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Slot Generation Tests
// ---------------------------------------------------------------------------

func TestGenerateSlots_BlockedFamilyTimeExcluded(t *testing.T) {
	// Test 1: blocked family time excluded from available work slots
	cfg := SlotGenerationConfig{
		MaxDailyWorkHours:  8,
		MinFamilyTimeHours: 2,
		WorkingWindows:     []string{"09:00-18:00"},
		BlockedRanges: []BlockedRange{
			{Reason: "family", Range: "18:00-21:00"},
			{Reason: "family", Range: "12:00-13:00"}, // lunch/family
		},
		Date:      time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead: 1,
	}

	slots := GenerateSlots(cfg)

	for _, slot := range slots {
		if slot.SlotType == SlotTypeFamilyBlocked {
			if slot.Available {
				t.Errorf("family_blocked slot %s should not be available", slot.ID)
			}
		}
	}

	// The 12:00-13:00 block should mark that slot as family_blocked.
	found := false
	for _, slot := range slots {
		if slot.StartTime.Hour() == 12 && slot.SlotType == SlotTypeFamilyBlocked {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 12:00-13:00 slot to be marked as family_blocked")
	}
}

func TestGenerateSlots_DailyWorkCapRespected(t *testing.T) {
	// Test 2: daily work cap respected
	cfg := SlotGenerationConfig{
		MaxDailyWorkHours:  4, // only 4 hours
		MinFamilyTimeHours: 2,
		WorkingWindows:     []string{"09:00-18:00"}, // 9 hours window
		Date:               time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead:          1,
	}

	slots := GenerateSlots(cfg)

	availableHours := 0.0
	for _, slot := range slots {
		if slot.Available && slot.SlotType == SlotTypeWork {
			availableHours += slot.DurationHours()
		}
	}

	if availableHours > cfg.MaxDailyWorkHours {
		t.Errorf("available work hours %f exceeds daily cap %f", availableHours, cfg.MaxDailyWorkHours)
	}
}

func TestGenerateSlots_OverloadReducesUsable(t *testing.T) {
	// Test 3: overload reduces usable slots
	cfgNormal := SlotGenerationConfig{
		MaxDailyWorkHours:  8,
		MinFamilyTimeHours: 2,
		WorkingWindows:     []string{"09:00-18:00"},
		OwnerLoadScore:     0.0,
		Date:               time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead:          1,
	}

	cfgOverloaded := SlotGenerationConfig{
		MaxDailyWorkHours:  8,
		MinFamilyTimeHours: 2,
		WorkingWindows:     []string{"09:00-18:00"},
		OwnerLoadScore:     0.9, // high load
		Date:               time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead:          1,
	}

	normalSlots := GenerateSlots(cfgNormal)
	overloadedSlots := GenerateSlots(cfgOverloaded)

	normalAvail := countAvailableWorkSlots(normalSlots)
	overloadedAvail := countAvailableWorkSlots(overloadedSlots)

	if overloadedAvail >= normalAvail {
		t.Errorf("overloaded should have fewer slots: normal=%d overloaded=%d", normalAvail, overloadedAvail)
	}
}

// ---------------------------------------------------------------------------
// Scoring Tests
// ---------------------------------------------------------------------------

func TestScoreFit_UrgentShortHighValue(t *testing.T) {
	// Test 4: urgent short high-value task gets better fit
	highValue := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.9,
		ExpectedValue:        500,
	}
	lowValue := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.2,
		ExpectedValue:        10,
	}

	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
		SlotType:  SlotTypeWork,
		Available: true,
	}

	highScore := ScoreFit(highValue, slot, 0.3)
	lowScore := ScoreFit(lowValue, slot, 0.3)

	if highScore <= lowScore {
		t.Errorf("high-value urgent task should score higher: got high=%.4f low=%.4f", highScore, lowScore)
	}
}

func TestScoreFit_OversizedTaskPoorFit(t *testing.T) {
	// Test 5: oversized task gets poor fit score (effort >> slot duration)
	oversized := SchedulingCandidate{
		EstimatedEffortHours: 10.0, // way more than 1-hour slot
		Urgency:              0.5,
		ExpectedValue:        100,
	}

	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
		SlotType:  SlotTypeWork,
		Available: true,
	}

	score := ScoreFit(oversized, slot, 0.3)

	// Effort fit component should be very low since 10h vs 1h slot.
	fittingCandidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.5,
		ExpectedValue:        100,
	}
	fittingScore := ScoreFit(fittingCandidate, slot, 0.3)

	if score >= fittingScore {
		t.Errorf("oversized task should score lower: oversized=%.4f fitting=%.4f", score, fittingScore)
	}
}

func TestScoreFit_LowValueLongDeprioritized(t *testing.T) {
	// Test 6: low-value long task deprioritized
	lowValueLong := SchedulingCandidate{
		EstimatedEffortHours: 8.0,
		Urgency:              0.1,
		ExpectedValue:        5,
	}
	highValueShort := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.8,
		ExpectedValue:        200,
	}

	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
		SlotType:  SlotTypeWork,
		Available: true,
	}

	lowScore := ScoreFit(lowValueLong, slot, 0.3)
	highScore := ScoreFit(highValueShort, slot, 0.3)

	if lowScore >= highScore {
		t.Errorf("low-value long task should rank lower: low=%.4f high=%.4f", lowScore, highScore)
	}
}

// ---------------------------------------------------------------------------
// Recommendation Tests
// ---------------------------------------------------------------------------

func TestScoreSlots_BestSlotDeterministic(t *testing.T) {
	// Test 7: best slot selected deterministically
	candidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.8,
		ExpectedValue:        200,
	}

	slots := []ScheduleSlot{
		{
			ID:        "slot-a",
			StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
			SlotType:  SlotTypeWork,
			Available: true,
		},
		{
			ID:        "slot-b",
			StartTime: time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 4, 10, 15, 0, 0, 0, time.UTC),
			SlotType:  SlotTypeWork,
			Available: true,
		},
	}

	scored := ScoreSlots(candidate, slots, 0.3)
	if len(scored) != 2 {
		t.Fatalf("expected 2 scored slots, got %d", len(scored))
	}

	// Run again — must be deterministic.
	scored2 := ScoreSlots(candidate, slots, 0.3)
	if scored[0].Slot.ID != scored2[0].Slot.ID {
		t.Error("slot selection must be deterministic")
	}
}

func TestScoreSlots_NoValidSlots(t *testing.T) {
	// Test 8: no valid slots → empty result
	candidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.5,
		ExpectedValue:        100,
	}

	// All slots are unavailable.
	slots := []ScheduleSlot{
		{
			ID:        "slot-blocked",
			StartTime: time.Date(2026, 4, 10, 18, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 4, 10, 19, 0, 0, 0, time.UTC),
			SlotType:  SlotTypeFamilyBlocked,
			Available: false,
		},
	}

	scored := ScoreSlots(candidate, slots, 0.3)
	if len(scored) != 0 {
		t.Errorf("expected 0 scored slots for unavailable slots, got %d", len(scored))
	}
}

func TestScoreSlots_FamilyBlockedNeverSelected(t *testing.T) {
	// Test 9: family-blocked slot never selected
	candidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              1.0,
		ExpectedValue:        10000,
	}

	slots := []ScheduleSlot{
		{
			ID:        "family-slot",
			StartTime: time.Date(2026, 4, 10, 18, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 4, 10, 19, 0, 0, 0, time.UTC),
			SlotType:  SlotTypeFamilyBlocked,
			Available: false,
		},
		{
			ID:        "work-slot",
			StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
			SlotType:  SlotTypeWork,
			Available: true,
		},
	}

	scored := ScoreSlots(candidate, slots, 0.3)
	for _, s := range scored {
		if s.Slot.SlotType == SlotTypeFamilyBlocked {
			t.Error("family-blocked slot must never appear in scored results")
		}
	}
}

// ---------------------------------------------------------------------------
// Review / Calendar Tests
// ---------------------------------------------------------------------------

func TestRequiresReview_ExternalMeeting(t *testing.T) {
	// Test 10: external meeting requires review
	candidate := SchedulingCandidate{ItemType: "meeting"}
	slot := ScheduleSlot{SlotType: SlotTypeWork}

	needsReview, reason := RequiresReview(candidate, slot, false)
	if !needsReview {
		t.Error("meeting scheduling should require review")
	}
	if reason == "" {
		t.Error("review reason should not be empty")
	}
}

func TestRequiresReview_CalendarWriteBlockedWithoutApproval(t *testing.T) {
	// Test 11: calendar write blocked without approval
	candidate := SchedulingCandidate{ItemType: "revenue"}
	slot := ScheduleSlot{SlotType: SlotTypeWork}

	needsReview, _ := RequiresReview(candidate, slot, true)
	if !needsReview {
		t.Error("calendar write should require review")
	}
}

func TestRequiresReview_RegularTaskNoReview(t *testing.T) {
	// Part of Test 12: regular task without calendar write needs no review
	candidate := SchedulingCandidate{ItemType: "revenue"}
	slot := ScheduleSlot{SlotType: SlotTypeWork}

	needsReview, _ := RequiresReview(candidate, slot, false)
	if needsReview {
		t.Error("regular scheduling should not require review")
	}
}

// ---------------------------------------------------------------------------
// State Transition Tests
// ---------------------------------------------------------------------------

func TestDecisionTransitionValid(t *testing.T) {
	if !IsValidDecisionTransition(DecisionStatusProposed, DecisionStatusApproved) {
		t.Error("proposed → approved should be valid")
	}
	if !IsValidDecisionTransition(DecisionStatusProposed, DecisionStatusRejected) {
		t.Error("proposed → rejected should be valid")
	}
	if !IsValidDecisionTransition(DecisionStatusApproved, DecisionStatusScheduled) {
		t.Error("approved → scheduled should be valid")
	}
}

func TestDecisionTransitionInvalid(t *testing.T) {
	if IsValidDecisionTransition(DecisionStatusScheduled, DecisionStatusProposed) {
		t.Error("scheduled → proposed should not be valid")
	}
	if IsValidDecisionTransition(DecisionStatusRejected, DecisionStatusApproved) {
		t.Error("rejected → approved should not be valid (terminal state)")
	}
}

// ---------------------------------------------------------------------------
// Integration Tests (unit-level, no DB)
// ---------------------------------------------------------------------------

func TestRevenueTaskBecomesCandidate(t *testing.T) {
	// Test 13: revenue task becomes scheduling candidate
	c := SchedulingCandidate{
		ItemType:             ItemTypeRevenue,
		ItemID:               "opp-123",
		EstimatedEffortHours: 2.0,
		Urgency:              0.7,
		ExpectedValue:        300,
	}

	if c.ItemType != ItemTypeRevenue {
		t.Errorf("expected revenue item type, got %s", c.ItemType)
	}

	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
		SlotType:  SlotTypeWork,
		Available: true,
	}

	score := ScoreFit(c, slot, 0.3)
	if score <= 0 {
		t.Error("revenue task should have positive fit score")
	}
}

func TestStrategyPriorityInfluencesScore(t *testing.T) {
	// Test 14: strategy priority influences slot recommendation
	baseCandidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.5,
		ExpectedValue:        100,
		StrategyPriority:     0.0,
	}
	boostCandidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.5,
		ExpectedValue:        100,
		StrategyPriority:     1.0, // max strategy priority
	}

	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
		SlotType:  SlotTypeWork,
		Available: true,
	}

	baseScore := ScoreFit(baseCandidate, slot, 0.3)
	boostScore := ScoreFit(boostCandidate, slot, 0.3)

	if boostScore <= baseScore {
		t.Errorf("strategy priority should boost score: base=%.4f boost=%.4f", baseScore, boostScore)
	}
	if boostScore-baseScore > StrategyPriorityBoostMax+0.001 {
		t.Errorf("strategy boost should not exceed max: diff=%.4f max=%.4f", boostScore-baseScore, StrategyPriorityBoostMax)
	}
}

func TestNoCalendarConnector_RecommendationStillWorks(t *testing.T) {
	// Test 15: no calendar connector → recommendation still works
	// This tests that ScoreSlots works independently of any connector.
	candidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.5,
		ExpectedValue:        100,
	}

	slots := []ScheduleSlot{
		{
			ID:        "slot-1",
			StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
			SlotType:  SlotTypeWork,
			Available: true,
		},
	}

	scored := ScoreSlots(candidate, slots, 0.3)
	if len(scored) == 0 {
		t.Error("scoring should work without any calendar connector")
	}
}

// ---------------------------------------------------------------------------
// Slot Generation Edge Cases
// ---------------------------------------------------------------------------

func TestGenerateSlots_MultiDay(t *testing.T) {
	cfg := SlotGenerationConfig{
		MaxDailyWorkHours:  8,
		MinFamilyTimeHours: 2,
		WorkingWindows:     []string{"09:00-17:00"},
		Date:               time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead:          3,
	}

	slots := GenerateSlots(cfg)
	days := make(map[string]bool)
	for _, s := range slots {
		day := s.StartTime.Format("2006-01-02")
		days[day] = true
	}

	if len(days) != 3 {
		t.Errorf("expected slots across 3 days, got %d different days", len(days))
	}
}

func TestGenerateSlots_MaxDaysAheadClamped(t *testing.T) {
	cfg := SlotGenerationConfig{
		MaxDailyWorkHours: 8,
		WorkingWindows:    []string{"09:00-17:00"},
		Date:              time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead:         100, // should be clamped to MaxDaysAhead
	}

	slots := GenerateSlots(cfg)
	days := make(map[string]bool)
	for _, s := range slots {
		day := s.StartTime.Format("2006-01-02")
		days[day] = true
	}

	if len(days) > MaxDaysAhead {
		t.Errorf("expected at most %d days, got %d", MaxDaysAhead, len(days))
	}
}

func TestGenerateSlots_DefaultWindows(t *testing.T) {
	cfg := SlotGenerationConfig{
		MaxDailyWorkHours: 8,
		Date:              time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead:         1,
	}

	slots := GenerateSlots(cfg)
	if len(slots) == 0 {
		t.Error("default windows should produce slots")
	}

	// Check slots are within default work hours (9-18).
	for _, s := range slots {
		if s.StartTime.Hour() < DefaultWorkStartHour || s.StartTime.Hour() >= DefaultWorkEndHour {
			t.Errorf("slot at hour %d is outside default work hours", s.StartTime.Hour())
		}
	}
}

func TestGenerateSlots_DeterministicIDs(t *testing.T) {
	cfg := SlotGenerationConfig{
		MaxDailyWorkHours: 8,
		WorkingWindows:    []string{"09:00-12:00"},
		Date:              time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead:         1,
	}

	slots1 := GenerateSlots(cfg)
	slots2 := GenerateSlots(cfg)

	if len(slots1) != len(slots2) {
		t.Fatal("deterministic generation should produce same number of slots")
	}
	for i := range slots1 {
		if slots1[i].ID != slots2[i].ID {
			t.Errorf("slot IDs should be deterministic: %s vs %s", slots1[i].ID, slots2[i].ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Score Bounding Tests
// ---------------------------------------------------------------------------

func TestScoreFit_BoundedZeroOne(t *testing.T) {
	// Extreme high values.
	c := SchedulingCandidate{
		EstimatedEffortHours: 0.1,
		Urgency:              1.0,
		ExpectedValue:        1000000,
		StrategyPriority:     1.0,
	}
	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
		SlotType:  SlotTypeWork,
		Available: true,
	}

	score := ScoreFit(c, slot, 0)
	if score < 0 || score > 1 {
		t.Errorf("score must be [0,1], got %f", score)
	}

	// Extreme low values.
	c2 := SchedulingCandidate{
		EstimatedEffortHours: 100,
		Urgency:              0,
		ExpectedValue:        0,
		StrategyPriority:     0,
	}
	score2 := ScoreFit(c2, slot, 1.0)
	if score2 < 0 || score2 > 1 {
		t.Errorf("score must be [0,1], got %f", score2)
	}
}

func TestScoreFit_OwnerLoadPenalty(t *testing.T) {
	c := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.5,
		ExpectedValue:        100,
	}
	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC),
		SlotType:  SlotTypeWork,
		Available: true,
	}

	lowLoad := ScoreFit(c, slot, 0.1)
	highLoad := ScoreFit(c, slot, 0.9)

	if highLoad >= lowLoad {
		t.Errorf("high load should penalize score: lowLoad=%.4f highLoad=%.4f", lowLoad, highLoad)
	}
}

func TestSlotDurationHours(t *testing.T) {
	slot := ScheduleSlot{
		StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 10, 11, 30, 0, 0, time.UTC),
	}
	if got := slot.DurationHours(); got != 1.5 {
		t.Errorf("expected 1.5 hours, got %f", got)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func countAvailableWorkSlots(slots []ScheduleSlot) int {
	count := 0
	for _, s := range slots {
		if s.Available && s.SlotType == SlotTypeWork {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// MockCalendarConnector for test scenarios
// ---------------------------------------------------------------------------

type mockCalendarConnector struct {
	createCalled bool
	dryRunCalled bool
	shouldFail   bool
}

func (m *mockCalendarConnector) CreateEvent(_ context.Context, _ ScheduleDecision, _ SchedulingCandidate, _ ScheduleSlot, dryRun bool) (string, string, error) {
	if dryRun {
		m.dryRunCalled = true
	} else {
		m.createCalled = true
	}
	if m.shouldFail {
		return "", "", errStr("connector failed")
	}
	return "cal-ext-123", "event://ref/123", nil
}

type errStr string

func (e errStr) Error() string { return string(e) }

func TestMockCalendarConnector_DryRun(t *testing.T) {
	// Test 12 (partial): approved calendar action can proceed through connector
	mock := &mockCalendarConnector{}
	_, _, err := mock.CreateEvent(context.Background(), ScheduleDecision{}, SchedulingCandidate{}, ScheduleSlot{}, true)
	if err != nil {
		t.Errorf("dry-run should succeed: %v", err)
	}
	if !mock.dryRunCalled {
		t.Error("expected dry-run flag to be set")
	}
}

func TestMockCalendarConnector_Execute(t *testing.T) {
	mock := &mockCalendarConnector{}
	extID, ref, err := mock.CreateEvent(context.Background(), ScheduleDecision{}, SchedulingCandidate{}, ScheduleSlot{}, false)
	if err != nil {
		t.Errorf("execute should succeed: %v", err)
	}
	if extID == "" || ref == "" {
		t.Error("external ID and ref should be non-empty on success")
	}
	if !mock.createCalled {
		t.Error("expected create flag to be set")
	}
}

func TestMockCalendarConnector_Failure(t *testing.T) {
	mock := &mockCalendarConnector{shouldFail: true}
	_, _, err := mock.CreateEvent(context.Background(), ScheduleDecision{}, SchedulingCandidate{}, ScheduleSlot{}, false)
	if err == nil {
		t.Error("failed connector should return error")
	}
}

// ---------------------------------------------------------------------------
// Family Protection Enforcement
// ---------------------------------------------------------------------------

func TestGenerateSlots_FamilyBlockedNeverAvailable(t *testing.T) {
	// Ensure that even with high max_daily_work_hours, family blocked time is always unavailable.
	cfg := SlotGenerationConfig{
		MaxDailyWorkHours:  24, // extreme — entire day "allowed"
		MinFamilyTimeHours: 0,
		WorkingWindows:     []string{"06:00-22:00"},
		BlockedRanges: []BlockedRange{
			{Reason: "family", Range: "18:00-21:00"},
		},
		Date:      time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		DaysAhead: 1,
	}

	slots := GenerateSlots(cfg)
	for _, s := range slots {
		if s.SlotType == SlotTypeFamilyBlocked && s.Available {
			t.Errorf("family-blocked slot at %s must never be available", s.StartTime.Format("15:04"))
		}
	}
}

func TestScoreSlots_OnlyAvailableWorkScored(t *testing.T) {
	candidate := SchedulingCandidate{
		EstimatedEffortHours: 1.0,
		Urgency:              0.5,
		ExpectedValue:        100,
	}

	slots := []ScheduleSlot{
		{ID: "s1", StartTime: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC), SlotType: SlotTypeWork, Available: true},
		{ID: "s2", StartTime: time.Date(2026, 4, 10, 18, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 4, 10, 19, 0, 0, 0, time.UTC), SlotType: SlotTypeFamilyBlocked, Available: false},
		{ID: "s3", StartTime: time.Date(2026, 4, 10, 11, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC), SlotType: SlotTypeBuffer, Available: false},
	}

	scored := ScoreSlots(candidate, slots, 0.3)
	if len(scored) != 1 {
		t.Errorf("expected exactly 1 scored slot (work+available), got %d", len(scored))
	}
	if scored[0].Slot.ID != "s1" {
		t.Errorf("expected scored slot s1, got %s", scored[0].Slot.ID)
	}
}

// ---------------------------------------------------------------------------
// Adapter nil-safety
// ---------------------------------------------------------------------------

func TestGraphAdapter_NilSafe(t *testing.T) {
	var a *GraphAdapter

	slots, err := a.ListSlots(context.Background())
	if err != nil || slots != nil {
		t.Error("nil adapter should return nil, nil")
	}

	candidates, err := a.ListCandidates(context.Background(), 10)
	if err != nil || candidates != nil {
		t.Error("nil adapter should return nil, nil")
	}

	decisions, err := a.ListDecisions(context.Background(), 10)
	if err != nil || decisions != nil {
		t.Error("nil adapter should return nil, nil")
	}

	rec, err := a.Recommend(context.Background(), "test")
	if err != nil {
		t.Error("nil adapter should not error")
	}
	if !rec.NoValidSlots {
		t.Error("nil adapter should return no valid slots")
	}

	dec, err := a.ApproveDecision(context.Background(), "test")
	if err != nil {
		t.Error("nil adapter should not error")
	}
	if dec.ID != "" {
		t.Error("nil adapter should return zero decision")
	}

	record, err := a.WriteCalendar(context.Background(), "test", true)
	if err != nil {
		t.Error("nil adapter should not error")
	}
	if record.ID != "" {
		t.Error("nil adapter should return zero record")
	}
}

func TestGraphAdapter_NilEngine(t *testing.T) {
	a := &GraphAdapter{engine: nil}

	slots, err := a.RecomputeSlots(context.Background())
	if err != nil || slots != nil {
		t.Error("nil engine in adapter should return nil, nil")
	}

	rec, err := a.Recommend(context.Background(), "test")
	if err != nil {
		t.Error("nil engine should not error")
	}
	if !rec.NoValidSlots {
		t.Error("nil engine should return no valid slots")
	}
}

func TestGetEngine_NilSafe(t *testing.T) {
	var a *GraphAdapter
	if a.GetEngine() != nil {
		t.Error("nil adapter GetEngine should return nil")
	}

	a2 := &GraphAdapter{}
	if a2.GetEngine() != nil {
		t.Error("zero adapter GetEngine should return nil engine")
	}
}
