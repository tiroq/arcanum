package scheduling

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SlotStore persists schedule slots in PostgreSQL.
type SlotStore struct {
	pool *pgxpool.Pool
}

// NewSlotStore creates a new slot store.
func NewSlotStore(pool *pgxpool.Pool) *SlotStore {
	return &SlotStore{pool: pool}
}

// SaveSlots persists a batch of schedule slots (UPSERT on id).
func (s *SlotStore) SaveSlots(ctx context.Context, slots []ScheduleSlot) error {
	if len(slots) == 0 {
		return nil
	}
	const q = `
		INSERT INTO agent_schedule_slots (
			id, start_time, end_time, slot_type, available, day_of_week, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			start_time = EXCLUDED.start_time,
			end_time   = EXCLUDED.end_time,
			slot_type  = EXCLUDED.slot_type,
			available  = EXCLUDED.available,
			day_of_week = EXCLUDED.day_of_week`

	for _, slot := range slots {
		_, err := s.pool.Exec(ctx, q,
			slot.ID, slot.StartTime, slot.EndTime, slot.SlotType,
			slot.Available, slot.DayOfWeek, slot.CreatedAt,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListSlots returns slots for a date range, ordered by start_time.
func (s *SlotStore) ListSlots(ctx context.Context, from, to time.Time) ([]ScheduleSlot, error) {
	const q = `
		SELECT id, start_time, end_time, slot_type, available, day_of_week, created_at
		FROM agent_schedule_slots
		WHERE start_time >= $1 AND end_time <= $2
		ORDER BY start_time ASC`

	rows, err := s.pool.Query(ctx, q, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []ScheduleSlot
	for rows.Next() {
		var slot ScheduleSlot
		if err := rows.Scan(
			&slot.ID, &slot.StartTime, &slot.EndTime, &slot.SlotType,
			&slot.Available, &slot.DayOfWeek, &slot.CreatedAt,
		); err != nil {
			continue
		}
		slots = append(slots, slot)
	}
	return slots, nil
}

// ListAvailableSlots returns only available work slots for a date range.
func (s *SlotStore) ListAvailableSlots(ctx context.Context, from, to time.Time) ([]ScheduleSlot, error) {
	const q = `
		SELECT id, start_time, end_time, slot_type, available, day_of_week, created_at
		FROM agent_schedule_slots
		WHERE start_time >= $1 AND end_time <= $2 AND available = true AND slot_type = 'work'
		ORDER BY start_time ASC`

	rows, err := s.pool.Query(ctx, q, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []ScheduleSlot
	for rows.Next() {
		var slot ScheduleSlot
		if err := rows.Scan(
			&slot.ID, &slot.StartTime, &slot.EndTime, &slot.SlotType,
			&slot.Available, &slot.DayOfWeek, &slot.CreatedAt,
		); err != nil {
			continue
		}
		slots = append(slots, slot)
	}
	return slots, nil
}

// CandidateStore persists scheduling candidates in PostgreSQL.
type CandidateStore struct {
	pool *pgxpool.Pool
}

// NewCandidateStore creates a new candidate store.
func NewCandidateStore(pool *pgxpool.Pool) *CandidateStore {
	return &CandidateStore{pool: pool}
}

// SaveCandidate persists a scheduling candidate (UPSERT on id).
func (s *CandidateStore) SaveCandidate(ctx context.Context, c SchedulingCandidate) (SchedulingCandidate, error) {
	const q = `
		INSERT INTO agent_schedule_candidates (
			id, item_type, item_id, estimated_effort_hours, urgency,
			expected_value, preferred_window, strategy_priority, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			item_type             = EXCLUDED.item_type,
			item_id               = EXCLUDED.item_id,
			estimated_effort_hours = EXCLUDED.estimated_effort_hours,
			urgency               = EXCLUDED.urgency,
			expected_value        = EXCLUDED.expected_value,
			preferred_window      = EXCLUDED.preferred_window,
			strategy_priority     = EXCLUDED.strategy_priority
		RETURNING id, item_type, item_id, estimated_effort_hours, urgency,
		          expected_value, preferred_window, strategy_priority, created_at`

	err := s.pool.QueryRow(ctx, q,
		c.ID, c.ItemType, c.ItemID, c.EstimatedEffortHours, c.Urgency,
		c.ExpectedValue, c.PreferredWindow, c.StrategyPriority, c.CreatedAt,
	).Scan(
		&c.ID, &c.ItemType, &c.ItemID, &c.EstimatedEffortHours, &c.Urgency,
		&c.ExpectedValue, &c.PreferredWindow, &c.StrategyPriority, &c.CreatedAt,
	)
	return c, err
}

// ListCandidates returns recent scheduling candidates.
func (s *CandidateStore) ListCandidates(ctx context.Context, limit int) ([]SchedulingCandidate, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, item_type, item_id, estimated_effort_hours, urgency,
		       expected_value, preferred_window, strategy_priority, created_at
		FROM agent_schedule_candidates
		ORDER BY created_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []SchedulingCandidate
	for rows.Next() {
		var c SchedulingCandidate
		if err := rows.Scan(
			&c.ID, &c.ItemType, &c.ItemID, &c.EstimatedEffortHours, &c.Urgency,
			&c.ExpectedValue, &c.PreferredWindow, &c.StrategyPriority, &c.CreatedAt,
		); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

// GetCandidate returns a single candidate by ID.
func (s *CandidateStore) GetCandidate(ctx context.Context, id string) (SchedulingCandidate, error) {
	const q = `
		SELECT id, item_type, item_id, estimated_effort_hours, urgency,
		       expected_value, preferred_window, strategy_priority, created_at
		FROM agent_schedule_candidates
		WHERE id = $1`

	var c SchedulingCandidate
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.ItemType, &c.ItemID, &c.EstimatedEffortHours, &c.Urgency,
		&c.ExpectedValue, &c.PreferredWindow, &c.StrategyPriority, &c.CreatedAt,
	)
	return c, err
}

// DecisionStore persists schedule decisions in PostgreSQL.
type DecisionStore struct {
	pool *pgxpool.Pool
}

// NewDecisionStore creates a new decision store.
func NewDecisionStore(pool *pgxpool.Pool) *DecisionStore {
	return &DecisionStore{pool: pool}
}

// SaveDecision persists a schedule decision.
func (s *DecisionStore) SaveDecision(ctx context.Context, d ScheduleDecision) (ScheduleDecision, error) {
	const q = `
		INSERT INTO agent_schedule_decisions (
			id, candidate_id, chosen_slot_id, fit_score,
			requires_review, review_reason, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, candidate_id, chosen_slot_id, fit_score,
		          requires_review, review_reason, status, created_at`

	err := s.pool.QueryRow(ctx, q,
		d.ID, d.CandidateID, d.ChosenSlotID, d.FitScore,
		d.RequiresReview, d.ReviewReason, d.Status, d.CreatedAt,
	).Scan(
		&d.ID, &d.CandidateID, &d.ChosenSlotID, &d.FitScore,
		&d.RequiresReview, &d.ReviewReason, &d.Status, &d.CreatedAt,
	)
	return d, err
}

// UpdateDecisionStatus updates the status of a schedule decision.
func (s *DecisionStore) UpdateDecisionStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE agent_schedule_decisions SET status = $1 WHERE id = $2`
	_, err := s.pool.Exec(ctx, q, status, id)
	return err
}

// GetDecision returns a single decision by ID.
func (s *DecisionStore) GetDecision(ctx context.Context, id string) (ScheduleDecision, error) {
	const q = `
		SELECT id, candidate_id, chosen_slot_id, fit_score,
		       requires_review, review_reason, status, created_at
		FROM agent_schedule_decisions
		WHERE id = $1`

	var d ScheduleDecision
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&d.ID, &d.CandidateID, &d.ChosenSlotID, &d.FitScore,
		&d.RequiresReview, &d.ReviewReason, &d.Status, &d.CreatedAt,
	)
	return d, err
}

// ListDecisions returns recent schedule decisions.
func (s *DecisionStore) ListDecisions(ctx context.Context, limit int) ([]ScheduleDecision, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, candidate_id, chosen_slot_id, fit_score,
		       requires_review, review_reason, status, created_at
		FROM agent_schedule_decisions
		ORDER BY created_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []ScheduleDecision
	for rows.Next() {
		var d ScheduleDecision
		if err := rows.Scan(
			&d.ID, &d.CandidateID, &d.ChosenSlotID, &d.FitScore,
			&d.RequiresReview, &d.ReviewReason, &d.Status, &d.CreatedAt,
		); err != nil {
			continue
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

// CalendarStore persists calendar records in PostgreSQL.
type CalendarStore struct {
	pool *pgxpool.Pool
}

// NewCalendarStore creates a new calendar store.
func NewCalendarStore(pool *pgxpool.Pool) *CalendarStore {
	return &CalendarStore{pool: pool}
}

// SaveRecord persists a calendar record.
func (s *CalendarStore) SaveRecord(ctx context.Context, r CalendarRecord) (CalendarRecord, error) {
	const q = `
		INSERT INTO agent_calendar_records (
			id, decision_id, external_calendar_id, event_ref,
			status, error_message, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, decision_id, external_calendar_id, event_ref,
		          status, error_message, created_at`

	err := s.pool.QueryRow(ctx, q,
		r.ID, r.DecisionID, r.ExternalCalendarID, r.EventRef,
		r.Status, r.ErrorMessage, r.CreatedAt,
	).Scan(
		&r.ID, &r.DecisionID, &r.ExternalCalendarID, &r.EventRef,
		&r.Status, &r.ErrorMessage, &r.CreatedAt,
	)
	return r, err
}

// GetRecordByDecision returns the calendar record for a decision.
func (s *CalendarStore) GetRecordByDecision(ctx context.Context, decisionID string) (CalendarRecord, error) {
	const q = `
		SELECT id, decision_id, external_calendar_id, event_ref,
		       status, error_message, created_at
		FROM agent_calendar_records
		WHERE decision_id = $1
		ORDER BY created_at DESC
		LIMIT 1`

	var r CalendarRecord
	err := s.pool.QueryRow(ctx, q, decisionID).Scan(
		&r.ID, &r.DecisionID, &r.ExternalCalendarID, &r.EventRef,
		&r.Status, &r.ErrorMessage, &r.CreatedAt,
	)
	return r, err
}

// ListRecords returns recent calendar records.
func (s *CalendarStore) ListRecords(ctx context.Context, limit int) ([]CalendarRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, decision_id, external_calendar_id, event_ref,
		       status, error_message, created_at
		FROM agent_calendar_records
		ORDER BY created_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CalendarRecord
	for rows.Next() {
		var r CalendarRecord
		if err := rows.Scan(
			&r.ID, &r.DecisionID, &r.ExternalCalendarID, &r.EventRef,
			&r.Status, &r.ErrorMessage, &r.CreatedAt,
		); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, nil
}
