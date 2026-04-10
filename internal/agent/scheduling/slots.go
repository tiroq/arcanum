package scheduling

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GenerateSlots produces deterministic schedule slots for a date range.
//
// For each day:
//  1. Generate 1-hour work slots within working windows.
//  2. Mark slots overlapping blocked ranges as family_blocked + unavailable.
//  3. Apply daily work cap — mark excess slots as buffer + unavailable.
//  4. Apply overload reduction — further reduce available slots when load is high.
//
// All computation is deterministic given the same config + date.
func GenerateSlots(cfg SlotGenerationConfig) []ScheduleSlot {
	days := cfg.DaysAhead
	if days <= 0 {
		days = 1
	}
	if days > MaxDaysAhead {
		days = MaxDaysAhead
	}
	if cfg.MaxDailyWorkHours <= 0 {
		cfg.MaxDailyWorkHours = 8
	}

	windows := cfg.WorkingWindows
	if len(windows) == 0 {
		windows = []string{
			fmt.Sprintf("%02d:00-%02d:00", DefaultWorkStartHour, DefaultWorkEndHour),
		}
	}

	now := time.Now().UTC()
	var allSlots []ScheduleSlot

	baseDate := cfg.Date
	if baseDate.IsZero() {
		baseDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	} else {
		baseDate = time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(), 0, 0, 0, 0, time.UTC)
	}

	for d := 0; d < days; d++ {
		dayStart := baseDate.AddDate(0, 0, d)
		daySlots := generateDaySlots(dayStart, windows, cfg, now)
		allSlots = append(allSlots, daySlots...)
	}

	return allSlots
}

// generateDaySlots generates all slots for a single day.
func generateDaySlots(dayStart time.Time, windows []string, cfg SlotGenerationConfig, now time.Time) []ScheduleSlot {
	var slots []ScheduleSlot

	blocked := parseBlockedWindows(dayStart, cfg.BlockedRanges)

	// Generate hourly slots within working windows.
	for _, w := range windows {
		parts := strings.SplitN(w, "-", 2)
		if len(parts) != 2 {
			continue
		}
		wStart, err1 := parseHHMM(parts[0])
		wEnd, err2 := parseHHMM(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}

		slotStart := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(),
			wStart.Hour(), wStart.Minute(), 0, 0, time.UTC)
		windowEnd := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(),
			wEnd.Hour(), wEnd.Minute(), 0, 0, time.UTC)

		for slotStart.Before(windowEnd) && len(slots) < MaxSlotsPerDay {
			slotEnd := slotStart.Add(time.Duration(SlotDurationHours * float64(time.Hour)))
			if slotEnd.After(windowEnd) {
				slotEnd = windowEnd
			}

			slot := ScheduleSlot{
				ID:        uuid.NewSHA1(uuid.NameSpaceDNS, []byte(fmt.Sprintf("slot-%s-%s", slotStart.Format(time.RFC3339), slotEnd.Format(time.RFC3339)))).String(),
				StartTime: slotStart,
				EndTime:   slotEnd,
				SlotType:  SlotTypeWork,
				Available: true,
				DayOfWeek: dayStart.Weekday().String(),
				CreatedAt: now,
			}

			// Check if blocked.
			if isBlocked(slotStart, slotEnd, blocked) {
				slot.SlotType = SlotTypeFamilyBlocked
				slot.Available = false
			}

			slots = append(slots, slot)
			slotStart = slotEnd
		}
	}

	// Enforce daily work cap: count available work hours and mark excess as buffer.
	availableWorkHours := 0.0
	maxWork := cfg.MaxDailyWorkHours
	if cfg.MinFamilyTimeHours > 0 {
		effectiveMax := 24 - cfg.MinFamilyTimeHours
		if effectiveMax < maxWork {
			maxWork = effectiveMax
		}
	}

	// Apply overload reduction.
	if cfg.OwnerLoadScore > 0.6 {
		overloadFraction := (cfg.OwnerLoadScore - 0.6) / 0.4
		reduction := overloadFraction * 0.5 * maxWork
		maxWork -= reduction
		if maxWork < 0 {
			maxWork = 0
		}
	}

	for i := range slots {
		if slots[i].Available && slots[i].SlotType == SlotTypeWork {
			slotHours := slots[i].DurationHours()
			if availableWorkHours+slotHours > maxWork {
				slots[i].SlotType = SlotTypeBuffer
				slots[i].Available = false
			} else {
				availableWorkHours += slotHours
			}
		}
	}

	return slots
}

// blockedWindow is a parsed time range.
type blockedWindow struct {
	Start time.Time
	End   time.Time
}

// parseBlockedWindows converts blocked ranges to absolute times for a day.
func parseBlockedWindows(dayStart time.Time, ranges []BlockedRange) []blockedWindow {
	var windows []blockedWindow
	for _, br := range ranges {
		parts := strings.SplitN(br.Range, "-", 2)
		if len(parts) != 2 {
			continue
		}
		bStart, err1 := parseHHMM(parts[0])
		bEnd, err2 := parseHHMM(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}

		start := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(),
			bStart.Hour(), bStart.Minute(), 0, 0, time.UTC)
		end := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(),
			bEnd.Hour(), bEnd.Minute(), 0, 0, time.UTC)
		if end.Before(start) {
			end = end.AddDate(0, 0, 1) // crosses midnight
		}

		windows = append(windows, blockedWindow{Start: start, End: end})
	}
	return windows
}

// isBlocked checks whether [slotStart, slotEnd) overlaps any blocked window.
func isBlocked(slotStart, slotEnd time.Time, blocked []blockedWindow) bool {
	for _, bw := range blocked {
		// Overlap if slotStart < bw.End AND slotEnd > bw.Start
		if slotStart.Before(bw.End) && slotEnd.After(bw.Start) {
			return true
		}
	}
	return false
}

// parseHHMM parses "HH:MM" into a time.Time on a zero date for hour/minute extraction.
func parseHHMM(s string) (time.Time, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time: %s", s)
	}
	return t, nil
}
