# Iteration 48 — Autonomous Scheduling & Calendar Control — Validation Report

**Date:** 2026-07-17
**Status:** READY_WITH_GUARDS

---

## 1. Objective

Implement a bounded scheduling layer that turns existing time reasoning
(capacity, family context) and value reasoning (income scoring, portfolio strategy)
into structured calendar-aware scheduling decisions — with approval-gated calendar
control and hard family time constraints.

---

## 2. What Was Built

| Component | File | Purpose |
|-----------|------|---------|
| Types | `internal/agent/scheduling/types.go` | Constants, slot types, item types, decision statuses, entities |
| Store | `internal/agent/scheduling/store.go` | PostgreSQL persistence for 4 tables |
| Slot Generator | `internal/agent/scheduling/slots.go` | Deterministic hourly slot generation from family config |
| Fit Scorer | `internal/agent/scheduling/scorer.go` | 4-component bounded [0,1] scoring with strategy boost |
| Engine | `internal/agent/scheduling/engine.go` | Lifecycle orchestration with audit events |
| Adapter | `internal/agent/scheduling/adapter.go` | Nil-safe fail-open GraphAdapter wrapper |
| Tests | `internal/agent/scheduling/scheduling_test.go` | 32 unit tests covering all required scenarios |
| Migration Up | `internal/db/migrations/000052_create_agent_scheduling.up.sql` | 4 tables + indexes |
| Migration Down | `internal/db/migrations/000052_create_agent_scheduling.down.sql` | Rollback |
| API Handlers | `internal/api/handlers.go` | 7 handler methods for scheduling endpoints |
| API Routes | `internal/api/router.go` | 7 routes under `/api/v1/agent/schedule/` |
| Wiring | `cmd/api-gateway/main.go` | Engine init, bridge adapters, handler chain |

---

## 3. Architecture Decisions

### 3.1 Slot Generation

- Slots are 1-hour fixed-duration intervals generated deterministically from family config
- Family blocked time (e.g., 18:00–21:00) creates `family_blocked` slots that are **always unavailable**
- Daily work cap is enforced by converting excess work slots to `buffer` (unavailable)
- High owner load (>0.6) reduces available work hours proportionally
- Slot IDs use `uuid.NewSHA1` for deterministic generation — same config always produces same slots

### 3.2 Fit Scoring

4-component weighted score bounded [0,1]:
- `value_per_hour` (35%): `expectedValue / estimatedEffort / HighValuePerHourThreshold`
- `urgency` (25%): candidate urgency score
- `effort_fit` (25%): `1 - |slotDuration - effort| / max(slotDuration, effort)`
- `load_penalty` (15%): `1 - ownerLoadScore`

Plus optional `strategy_priority * StrategyPriorityBoostMax` (up to 10% boost), clamped [0,1].

### 3.3 Review Gating

Review is required when:
- Item type is `meeting` (external calendar interaction)
- Calendar write is requested (`calendarWrite=true`)

This aligns with the existing external_actions governance pattern.

### 3.4 State Machine

```
proposed → approved → scheduled
proposed → rejected (terminal)
```

Calendar writes require `approved` status. No implicit transitions.

### 3.5 Bridge Adapter Pattern

Two bridge adapters in main.go avoid import cycles:
- `schedulingCapacityAdapter` wraps `capacity.GraphAdapter` → `scheduling.CapacityProvider`
- `schedulingPortfolioAdapter` wraps `portfolio.GraphAdapter` → `scheduling.PortfolioProvider`

Both are fail-open with zero-value defaults when upstream returns errors.

---

## 4. API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/v1/agent/schedule/slots` | List current schedule slots |
| POST | `/api/v1/agent/schedule/recompute` | Regenerate slots from family config |
| GET/POST | `/api/v1/agent/schedule/candidates` | List or add scheduling candidates |
| POST | `/api/v1/agent/schedule/recommend` | Score candidate against slots, pick best |
| POST | `/api/v1/agent/schedule/approve/{id}` | Approve a proposed scheduling decision |
| GET | `/api/v1/agent/schedule/decisions` | List scheduling decisions |
| POST | `/api/v1/agent/schedule/calendar/{id}` | Write approved decision to external calendar |

---

## 5. Audit Events

| Event | Trigger |
|-------|---------|
| `schedule.slots_recomputed` | Slots regenerated |
| `schedule.candidate_added` | New candidate registered |
| `schedule.recommendation_made` | Best slot selected for candidate |
| `schedule.decision_approved` | Decision approved for calendar write |
| `schedule.calendar_written` | Decision written to external calendar |
| `schedule.calendar_write_failed` | Calendar write attempt failed |

---

## 6. Database Schema (Migration 000050)

4 tables:
- `agent_schedule_slots` — generated time slots with type and availability
- `agent_schedule_candidates` — items (revenue, task, meeting) awaiting scheduling
- `agent_schedule_decisions` — proposed/approved/scheduled/rejected decisions
- `agent_calendar_records` — external calendar write records with status tracking

All tables have proper indexes, FK constraints, and `created_at` timestamps.

---

## 7. Test Results

### Scheduling Package: 32 tests — ALL PASS

**Slot Generation (7 tests):**
- Blocked family time excluded from available slots ✅
- Daily work cap respected ✅
- Overload reduces usable slots ✅
- Multi-day generation ✅
- Max days ahead clamped ✅
- Default windows produce valid slots ✅
- Deterministic IDs ✅

**Scoring (4 tests):**
- Urgent short high-value gets better fit ✅
- Oversized task gets poor fit ✅
- Low-value long deprioritized ✅
- Score bounded [0,1] for extreme inputs ✅

**Recommendation (3 tests):**
- Best slot selected deterministically ✅
- No valid slots → empty result ✅
- Family-blocked slot never selected ✅

**Review / Calendar (6 tests):**
- External meeting requires review ✅
- Calendar write blocked without approval ✅
- Regular task no review ✅
- Mock connector dry-run ✅
- Mock connector execute ✅
- Mock connector failure ✅

**State Transitions (2 tests):**
- Valid transitions (proposed→approved, proposed→rejected, approved→scheduled) ✅
- Invalid transitions (scheduled→proposed, rejected→approved) ✅

**Integration (3 tests):**
- Revenue task becomes candidate with positive score ✅
- Strategy priority influences recommendation score ✅
- No calendar connector → recommendation still works ✅

**Safety (5 tests):**
- Owner load penalty reduces score ✅
- Family blocked never available (extreme config) ✅
- Only available work slots scored ✅
- Nil adapter safe ✅
- Nil engine safe ✅

**Additional (2 tests):**
- Slot duration calculation ✅
- GetEngine nil safety ✅

### Full Agent Suite: 35 packages — ALL PASS

No regressions across any existing agent package.

### Full Build: `go build ./...` — CLEAN

Zero compilation errors across all binaries and packages.

---

## 8. Integration Points

| Layer | Integration | Direction |
|-------|-------------|-----------|
| Capacity | `CapacityProvider` via bridge adapter | scheduling reads load/hours |
| Portfolio | `PortfolioProvider` via bridge adapter | scheduling reads strategy priority |
| Family Context | `SlotGenerationConfig` from `family_context.yaml` | scheduling reads blocked time |
| External Actions | Aligned governance pattern | scheduling uses same review gating |
| Audit | `audit.AuditRecorder` | scheduling emits all events |

---

## 9. Invariants Preserved

- **Family time is sacred**: blocked ranges are ALWAYS unavailable, never scored
- **No silent scheduling**: all decisions require explicit state transitions
- **Approval gating**: calendar writes require `approved` status
- **Bounded scoring**: all fit scores clamped [0,1], strategy boost capped at 10%
- **Deterministic**: same config produces same slots (SHA1-based UUIDs)
- **Fail-open**: nil adapters, nil engines, and missing connectors degrade gracefully
- **Observable**: 6 audit event types cover the full scheduling lifecycle
- **Recoverable**: explicit state machine prevents stuck states

---

## 10. Known Limitations / Future Work

1. **No CalendarConnector implementation**: interface defined, but no concrete Google Calendar / CalDAV connector exists yet. Calendar writes will fail gracefully until a connector is wired.
2. **No decision graph integration**: scheduling is a standalone API layer — not yet integrated into the decision graph scoring pipeline. A future iteration could add a `SchedulingProvider` to influence path scoring.
3. **Single-slot recommendations**: current implementation picks the single best slot. Multi-slot block scheduling for long tasks is not yet supported.
4. **No recurring schedule patterns**: each `RecomputeSlots` generates fresh slots. Recurring templates could be added later.

---

## 11. Rollout Recommendation

**READY_WITH_GUARDS**

Guards:
- Run migration 000050 in a staging environment before production
- Verify `family_context.yaml` blocked time entries are correctly formatted
- CalendarConnector is not wired — calendar write endpoint will return graceful errors
- Monitor audit events for `schedule.*` to verify lifecycle correctness
- Consider rate-limiting `/api/v1/agent/schedule/recompute` to prevent excessive slot regeneration
