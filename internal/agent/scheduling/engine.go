package scheduling

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// CapacityProvider retrieves capacity state. Defined here to avoid import cycles.
type CapacityProvider interface {
	GetAvailableHoursToday(ctx context.Context) float64
	GetOwnerLoadScore(ctx context.Context) float64
}

// PortfolioProvider retrieves strategy priority for an item. Defined here to avoid import cycles.
type PortfolioProvider interface {
	GetStrategyPriority(ctx context.Context, itemType string) float64
}

// CalendarConnector defines the interface for calendar integration through the
// external actions system. Defined here to avoid import cycles.
type CalendarConnector interface {
	// CreateEvent creates (or dry-runs) a calendar event.
	// Returns external_id, event_ref, error.
	CreateEvent(ctx context.Context, decision ScheduleDecision, candidate SchedulingCandidate, slot ScheduleSlot, dryRun bool) (string, string, error)
}

// Engine orchestrates scheduling: slot generation, candidate scoring,
// recommendation, approval, and calendar integration.
type Engine struct {
	slotStore      *SlotStore
	candidateStore *CandidateStore
	decisionStore  *DecisionStore
	calendarStore  *CalendarStore

	capacity  CapacityProvider
	portfolio PortfolioProvider
	calendar  CalendarConnector

	familyConfig SlotGenerationConfig

	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewEngine creates a new scheduling engine.
func NewEngine(
	slotStore *SlotStore,
	candidateStore *CandidateStore,
	decisionStore *DecisionStore,
	calendarStore *CalendarStore,
	familyConfig SlotGenerationConfig,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		slotStore:      slotStore,
		candidateStore: candidateStore,
		decisionStore:  decisionStore,
		calendarStore:  calendarStore,
		familyConfig:   familyConfig,
		auditor:        auditor,
		logger:         logger,
	}
}

// WithCapacity sets the capacity provider.
func (e *Engine) WithCapacity(c CapacityProvider) *Engine {
	e.capacity = c
	return e
}

// WithPortfolio sets the portfolio provider.
func (e *Engine) WithPortfolio(p PortfolioProvider) *Engine {
	e.portfolio = p
	return e
}

// WithCalendar sets the calendar connector.
func (e *Engine) WithCalendar(c CalendarConnector) *Engine {
	e.calendar = c
	return e
}

// RecomputeSlots generates schedule slots for the current day + configured days ahead.
// Persists them and returns the full list.
func (e *Engine) RecomputeSlots(ctx context.Context) ([]ScheduleSlot, error) {
	cfg := e.familyConfig
	cfg.Date = time.Now().UTC()
	if cfg.DaysAhead <= 0 {
		cfg.DaysAhead = 1
	}

	// Get live owner load from capacity.
	if e.capacity != nil {
		cfg.OwnerLoadScore = e.capacity.GetOwnerLoadScore(ctx)
	}

	slots := GenerateSlots(cfg)

	if err := e.slotStore.SaveSlots(ctx, slots); err != nil {
		e.logger.Warn("scheduling_slots_persist_failed", zap.Error(err))
		// Fail-open: return generated slots even if persist fails.
	}

	available := 0
	blocked := 0
	for _, s := range slots {
		if s.Available {
			available++
		} else {
			blocked++
		}
	}

	e.emitAudit(ctx, "schedule.slots_generated", map[string]any{
		"total_slots":     len(slots),
		"available_slots": available,
		"blocked_slots":   blocked,
		"days_ahead":      cfg.DaysAhead,
		"owner_load":      cfg.OwnerLoadScore,
	})

	return slots, nil
}

// GetSlots returns stored slots for the given date range.
func (e *Engine) GetSlots(ctx context.Context, from, to time.Time) ([]ScheduleSlot, error) {
	return e.slotStore.ListSlots(ctx, from, to)
}

// AddCandidate creates a scheduling candidate. Enriches with strategy priority if available.
func (e *Engine) AddCandidate(ctx context.Context, c SchedulingCandidate) (SchedulingCandidate, error) {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}

	// Enrich with portfolio strategy priority.
	if e.portfolio != nil && c.StrategyPriority == 0 {
		c.StrategyPriority = e.portfolio.GetStrategyPriority(ctx, c.ItemType)
	}

	saved, err := e.candidateStore.SaveCandidate(ctx, c)
	if err != nil {
		return SchedulingCandidate{}, fmt.Errorf("save candidate: %w", err)
	}

	e.emitAudit(ctx, "schedule.candidate_scored", map[string]any{
		"candidate_id":      saved.ID,
		"item_type":         saved.ItemType,
		"item_id":           saved.ItemID,
		"effort_hours":      saved.EstimatedEffortHours,
		"urgency":           saved.Urgency,
		"expected_value":    saved.ExpectedValue,
		"strategy_priority": saved.StrategyPriority,
	})

	return saved, nil
}

// ListCandidates returns recent scheduling candidates.
func (e *Engine) ListCandidates(ctx context.Context, limit int) ([]SchedulingCandidate, error) {
	return e.candidateStore.ListCandidates(ctx, limit)
}

// Recommend produces a scheduling recommendation for a candidate.
// Scores all available slots and selects the best fit deterministically.
func (e *Engine) Recommend(ctx context.Context, candidateID string) (ScheduleRecommendation, error) {
	candidate, err := e.candidateStore.GetCandidate(ctx, candidateID)
	if err != nil {
		return ScheduleRecommendation{}, fmt.Errorf("get candidate: %w", err)
	}

	// Get available slots for the next week.
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, MaxDaysAhead)

	slots, err := e.slotStore.ListAvailableSlots(ctx, from, to)
	if err != nil {
		e.logger.Warn("scheduling_slots_query_failed", zap.Error(err))
		// Fail-open: generate slots in-memory.
		cfg := e.familyConfig
		cfg.Date = from
		cfg.DaysAhead = MaxDaysAhead
		if e.capacity != nil {
			cfg.OwnerLoadScore = e.capacity.GetOwnerLoadScore(ctx)
		}
		allSlots := GenerateSlots(cfg)
		for _, s := range allSlots {
			if s.Available && s.SlotType == SlotTypeWork {
				slots = append(slots, s)
			}
		}
	}

	ownerLoad := 0.0
	if e.capacity != nil {
		ownerLoad = e.capacity.GetOwnerLoadScore(ctx)
	}

	scored := ScoreSlots(candidate, slots, ownerLoad)

	rec := ScheduleRecommendation{
		CandidateID: candidateID,
	}

	if len(scored) == 0 {
		rec.NoValidSlots = true
		return rec, nil
	}

	// Best slot.
	best := scored[0]
	rec.BestSlot = &best

	// Review check for the best slot.
	needsReview, reviewReason := RequiresReview(candidate, best.Slot, false)
	rec.RequiresReview = needsReview
	rec.ReviewReason = reviewReason

	// Alternate slots (up to 3).
	if len(scored) > 1 {
		limit := 4
		if limit > len(scored) {
			limit = len(scored)
		}
		rec.AlternateSlots = scored[1:limit]
	}

	// Persist decision.
	decision := ScheduleDecision{
		ID:             uuid.New().String(),
		CandidateID:    candidateID,
		ChosenSlotID:   best.Slot.ID,
		FitScore:       best.FitScore,
		RequiresReview: needsReview,
		ReviewReason:   reviewReason,
		Status:         DecisionStatusProposed,
		CreatedAt:      time.Now().UTC(),
	}

	saved, err := e.decisionStore.SaveDecision(ctx, decision)
	if err != nil {
		e.logger.Warn("scheduling_decision_persist_failed", zap.Error(err))
		// Fail-open: return recommendation even if persist fails.
	} else {
		decision = saved
	}

	e.emitAudit(ctx, "schedule.decision_proposed", map[string]any{
		"decision_id":     decision.ID,
		"candidate_id":    candidateID,
		"chosen_slot_id":  decision.ChosenSlotID,
		"fit_score":       decision.FitScore,
		"requires_review": decision.RequiresReview,
		"review_reason":   decision.ReviewReason,
		"alternate_count": len(rec.AlternateSlots),
	})

	return rec, nil
}

// ApproveDecision transitions a decision from proposed → approved.
func (e *Engine) ApproveDecision(ctx context.Context, decisionID string) (ScheduleDecision, error) {
	decision, err := e.decisionStore.GetDecision(ctx, decisionID)
	if err != nil {
		return ScheduleDecision{}, fmt.Errorf("get decision: %w", err)
	}

	if !IsValidDecisionTransition(decision.Status, DecisionStatusApproved) {
		return ScheduleDecision{}, fmt.Errorf("invalid transition: %s → approved", decision.Status)
	}

	if err := e.decisionStore.UpdateDecisionStatus(ctx, decisionID, DecisionStatusApproved); err != nil {
		return ScheduleDecision{}, fmt.Errorf("update status: %w", err)
	}
	decision.Status = DecisionStatusApproved

	e.emitAudit(ctx, "schedule.decision_approved", map[string]any{
		"decision_id":  decisionID,
		"candidate_id": decision.CandidateID,
		"fit_score":    decision.FitScore,
	})

	return decision, nil
}

// ListDecisions returns recent schedule decisions.
func (e *Engine) ListDecisions(ctx context.Context, limit int) ([]ScheduleDecision, error) {
	return e.decisionStore.ListDecisions(ctx, limit)
}

// WriteCalendar creates a calendar event for an approved decision.
// Requires the decision to be approved. Goes through the calendar connector.
// If no connector is set, fails gracefully.
func (e *Engine) WriteCalendar(ctx context.Context, decisionID string, dryRun bool) (CalendarRecord, error) {
	decision, err := e.decisionStore.GetDecision(ctx, decisionID)
	if err != nil {
		return CalendarRecord{}, fmt.Errorf("get decision: %w", err)
	}

	// Calendar writes require approval.
	if decision.Status != DecisionStatusApproved && decision.Status != DecisionStatusScheduled {
		return CalendarRecord{}, fmt.Errorf("decision must be approved before calendar write (current: %s)", decision.Status)
	}

	candidate, err := e.candidateStore.GetCandidate(ctx, decision.CandidateID)
	if err != nil {
		return CalendarRecord{}, fmt.Errorf("get candidate: %w", err)
	}

	// Get the chosen slot (from DB or regenerate).
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -1)
	to := now.AddDate(0, 0, MaxDaysAhead+1)
	slots, _ := e.slotStore.ListSlots(ctx, from, to)
	var chosenSlot ScheduleSlot
	found := false
	for _, s := range slots {
		if s.ID == decision.ChosenSlotID {
			chosenSlot = s
			found = true
			break
		}
	}
	if !found {
		return CalendarRecord{}, fmt.Errorf("chosen slot %s not found", decision.ChosenSlotID)
	}

	record := CalendarRecord{
		ID:         uuid.New().String(),
		DecisionID: decisionID,
		Status:     CalendarStatusPending,
		CreatedAt:  time.Now().UTC(),
	}

	if e.calendar == nil {
		// No calendar connector — record as dry_run with note.
		if dryRun {
			record.Status = CalendarStatusDryRun
		} else {
			record.Status = CalendarStatusFailed
			record.ErrorMessage = "no calendar connector available"
		}
		saved, saveErr := e.calendarStore.SaveRecord(ctx, record)
		if saveErr != nil {
			e.logger.Warn("calendar_record_persist_failed", zap.Error(saveErr))
		} else {
			record = saved
		}

		e.emitAudit(ctx, "schedule.calendar_failed", map[string]any{
			"decision_id": decisionID,
			"reason":      "no_connector",
			"dry_run":     dryRun,
		})

		return record, nil
	}

	externalID, eventRef, execErr := e.calendar.CreateEvent(ctx, decision, candidate, chosenSlot, dryRun)
	if execErr != nil {
		record.Status = CalendarStatusFailed
		record.ErrorMessage = execErr.Error()

		saved, saveErr := e.calendarStore.SaveRecord(ctx, record)
		if saveErr != nil {
			e.logger.Warn("calendar_record_persist_failed", zap.Error(saveErr))
		} else {
			record = saved
		}

		e.emitAudit(ctx, "schedule.calendar_failed", map[string]any{
			"decision_id": decisionID,
			"error":       execErr.Error(),
			"dry_run":     dryRun,
		})

		return record, execErr
	}

	if dryRun {
		record.Status = CalendarStatusDryRun
	} else {
		record.Status = CalendarStatusCreated
		// Mark decision as scheduled.
		if updateErr := e.decisionStore.UpdateDecisionStatus(ctx, decisionID, DecisionStatusScheduled); updateErr != nil {
			e.logger.Warn("scheduling_decision_status_update_failed", zap.Error(updateErr))
		}
	}
	record.ExternalCalendarID = externalID
	record.EventRef = eventRef

	saved, saveErr := e.calendarStore.SaveRecord(ctx, record)
	if saveErr != nil {
		e.logger.Warn("calendar_record_persist_failed", zap.Error(saveErr))
	} else {
		record = saved
	}

	e.emitAudit(ctx, "schedule.calendar_written", map[string]any{
		"decision_id":  decisionID,
		"calendar_id":  externalID,
		"event_ref":    eventRef,
		"dry_run":      dryRun,
		"candidate_id": candidate.ID,
		"item_type":    candidate.ItemType,
	})

	return record, nil
}

func (e *Engine) emitAudit(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "scheduling", uuid.Nil, eventType, "system", "scheduling_engine", payload)
}
