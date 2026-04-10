# Manual Startup + Runtime Validation Report

**Date:** 2026-04-10  
**Validator:** Automated (Copilot agent)  
**System:** Arcanum api-gateway (local)

---

## 1. Summary

**Verdict: BOOTS_CLEAN**

The system builds, tests pass, migrations apply cleanly, the api-gateway starts without errors or warnings, all tested endpoints respond correctly, write-paths execute and persist data, audit events are recorded, and no runtime errors were observed.

---

## 2. Build/Test Results

| Check | Result |
|-------|--------|
| Go version | go1.25.5 linux/amd64 |
| `go build ./...` | **PASS** — zero errors |
| `go test ./... -count=1` | **PASS** — 1602 tests, 0 failures |
| Failing packages | None |
| Git status | Clean (main branch) |

---

## 3. Infrastructure + Migration Results

### Infrastructure

| Service | Status | Port |
|---------|--------|------|
| PostgreSQL 16 (docker) | Healthy, running 7+ days | 5432 |
| NATS 2.10 (docker) | Healthy, running 7+ days | 4222, 8222 |
| Redis | Not required by api-gateway | — |

### Migrations

| Check | Result |
|-------|--------|
| Migration files | 55 up + 55 down = 110 files |
| Numbering | Sequential 000001–000055, no gaps or duplicates |
| DB state before | Version 36, clean |
| Migrations applied | 37–55 (19 migrations), all clean |
| DB state after | Version 55, dirty=false |
| Agent tables created | 79 tables verified |
| Key tables confirmed | `agent_objective_state`, `agent_risk_state`, `agent_objective_summary`, `agent_reflection_reports`, `agent_pricing_profiles`, `agent_negotiation_records`, `agent_pricing_outcomes`, `agent_pricing_performance`, `agent_schedule_slots`, `agent_schedule_candidates`, `agent_schedule_decisions`, `agent_calendar_records`, `agent_strategies`, `agent_strategy_allocations`, `agent_strategy_performance`, `agent_external_actions`, `agent_external_action_results`, `agent_self_proposals`, `agent_self_sandbox_runs`, `agent_financial_events`, `agent_financial_facts`, `agent_financial_matches`, `agent_actuation_decisions` |

---

## 4. Service Startup Results

### api-gateway

| Check | Result |
|-------|--------|
| Process start | Clean, PID assigned |
| Database connection | Connected |
| NATS connection | Connected |
| Startup errors | **None** |
| Startup warnings | **None** |
| Subsystems initialized | All 16 subsystems logged as initialized |

**Startup log subsystem initialization (all confirmed):**

1. database connected
2. nats connected
3. provider catalog loaded (0 models — no catalog dir, expected in dev)
4. system goals loaded (6 goals)
5. income engine initialised
6. signal ingestion engine initialised
7. financial pressure layer initialised
8. financial truth layer initialised
9. opportunity discovery engine initialised
10. time allocation layer initialised (max_daily_hours=8, min_family_hours=2)
11. self-extension sandbox initialised
12. external action connectors initialised (4 connectors)
13. strategic revenue portfolio initialised
14. pricing intelligence initialised
15. autonomous scheduling layer initialised (max_daily_hours=8)
16. meta-reflection layer initialised
17. global objective function initialised
18. closed feedback actuation initialised
19. starting api-gateway on port 8090

**No nil pointer warnings, no missing adapter warnings, no schema mismatch errors.**

---

## 5. Endpoint Validation Matrix

### GET Endpoints (Read)

| Endpoint | HTTP Status | Response Shape | Notes |
|----------|-------------|----------------|-------|
| `GET /health` | 200 | `{"status":"ok"}` | No auth required |
| `GET /ready` | 200 | `{"status":"ok","checks":{"database":"ok","nats":"ok"}}` | No auth required |
| `GET /api/v1/agent/objective/state` | 200 | ObjectiveState JSON | Zero values (empty DB), correct schema |
| `GET /api/v1/agent/objective/risk` | 200 | RiskState JSON | Zero values, correct schema |
| `GET /api/v1/agent/objective/summary` | 200 | ObjectiveSummary JSON | Zero values, correct schema |
| `GET /api/v1/agent/reflection/latest` | 200 | `{"report":null}` | Correct for empty state |
| `GET /api/v1/agent/reflection/reports` | 200 | `{"reports":null}` | Correct for empty state |
| `GET /api/v1/agent/strategies` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/portfolio` | 200 | Portfolio JSON with summary | Correct shape with zeros |
| `GET /api/v1/agent/portfolio/allocations` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/strategies/performance` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/pricing/profiles` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/pricing/performance` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/pricing/negotiations` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/schedule/slots` | 200 | Array of slot objects | Auto-generated slots for today |
| `GET /api/v1/agent/schedule/decisions` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/external/actions` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/financial/state` | 200 | Pressure + State JSON | Zero values, correct schema |
| `GET /api/v1/agent/financial/pressure` | 200 | FinancialPressure JSON | Zero values, correct schema |
| `GET /api/v1/agent/financial/truth/summary` | 200 | Truth summary JSON | Zero values, correct |
| `GET /api/v1/agent/self/proposals` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/actuation/decisions` | 200 | `[]` | Empty list, correct |
| `GET /api/v1/agent/capacity/state` | 200 | CapacityState JSON | Zero values, correct |
| `GET /api/v1/agent/governance/state` | 200 | GovernanceState JSON | Mode "frozen", correct |
| `GET /api/v1/agent/income/opportunities` | 200 | `{"opportunities":null}` | Correct for empty |
| `GET /api/v1/agent/income/performance` | 200 | PerformanceStats JSON | Zeros, correct schema |
| `GET /api/v1/agent/income/discovery/candidates` | 200 | `{"candidates":[]}` | Correct |
| `GET /api/v1/agent/income/discovery/stats` | 200 | DiscoveryStats JSON | Zeros, correct |
| `GET /api/v1/agent/signals` | 200 | `{"signals":[]}` | Correct |
| `GET /api/v1/agent/signals/derived` | 200 | `{"derived":[]}` | Correct |
| `GET /api/v1/agent/decision-graph/status` | 200 | `{"status":"no_evaluation"}` | Expected — no tasks processed |

**Results: 31/31 GET endpoints returned HTTP 200 with correct response shapes.**

### Auth Validation

| Test | Result |
|------|--------|
| Request without `X-Admin-Token` header | HTTP 401 — `{"error":"missing X-Admin-Token header"}` |
| Request with correct `X-Admin-Token` header | HTTP 200 — authorized |

---

## 6. Safe Runtime Actions Verified

### POST / Write-Path Endpoints

| Action | Endpoint | HTTP Status | Result |
|--------|----------|-------------|--------|
| Objective recompute | `POST /api/v1/agent/objective/recompute` | 200 | Computed: utility=0.465, risk=0.1795, net_utility=0.357 |
| Reflection run | `POST /api/v1/agent/reflection/run` | 200 | Generated report ID 06122038, persisted and retrievable via GET /latest |
| Schedule recompute | `POST /api/v1/agent/schedule/recompute` | 200 | Generated 9 time slots for today (8 work + 1 buffer) |
| Portfolio rebalance | `POST /api/v1/agent/portfolio/rebalance` | 200 | No active strategies (correct for empty state) |
| Actuation run | `POST /api/v1/agent/actuation/run` | 200 | No decisions (no reflection signals), inputs properly gathered |
| Signals recompute | `POST /api/v1/agent/signals/recompute` | 200 | 5 derived signals computed (all zero values) |
| Financial state update | `POST /api/v1/agent/financial/state` | 200 | Updated income=3500, target=5000; pressure=0.15, urgency=low |
| Financial event ingest | `POST /api/v1/agent/financial/events` | 201 | Created event+fact, truth summary updated to income_verified=500 |
| External action create | `POST /api/v1/agent/external/actions` | 201 | Created draft_message action, status=ready, risk=low |
| External action dry-run | `POST /actions/{id}/dry-run` | 200 | Dry-run success via noop connector |
| External action execute | `POST /actions/{id}/execute` | 200 | Execution success via noop connector |
| Capacity recompute | `POST /api/v1/agent/capacity/recompute` | 200 | 8 hours available, 0 blocked |

**Results: 12/12 write-path operations succeeded with correct responses.**

### Integration Verification

- After financial state update, objective recompute reflected the new financial data (risk_score changed from 0.1525 to 0.1795).
- Reflection run result persisted and retrievable via GET /latest endpoint.
- Financial event ingest updated truth summary (verified_income from 0 to 500).
- Actuation run properly gathered inputs from both reflection (signals) and objective (net_utility=0.396).

---

## 7. Log Findings

### Startup Logs (21 lines)

- **Errors:** 0
- **Warnings:** 0
- **Panics:** 0
- **Nil pointer references:** 0
- **Missing relation errors:** 0
- **Missing column errors:** 0
- **Invalid transitions:** 0
- **Missing routes:** 0
- **Auth mismatches:** 0
- **Connector not found:** 0

### Runtime Logs (127 total lines after endpoint testing)

- All request logs show HTTP 200/201 status codes (except initial 401s from incorrect auth header format during testing).
- No 500 errors.
- No error-level log entries.
- No warning-level log entries.

### Minor Notes (Informational, not errors)

- `provider catalog directory not found, skipping` — Expected in local dev without providers/ directory.
- `global routing policy file not found; using defaults` — Expected, _global.yaml not present.

---

## 8. Migration / Wiring / Route Risks

### Migration Numbering

| Check | Status |
|-------|--------|
| Sequential numbering (001-055) | **OK** — No gaps, no duplicates |
| Up/down file pairs | **OK** — All 55 have both |
| Runtime application (36→55) | **OK** — All 19 pending migrations applied cleanly |
| Dirty flag | **OK** — false |

### Known Discrepancy: Memory vs Actual Migrations

Repository memories reference migration numbers that don't match actual file numbering:
- Memory says pricing = 000050, actual = 000051
- Memory says scheduling = 000050, actual = 000052
- Memory says reflection_reports = —, actual = 000053
- Memory says objective = 000054, actual = 000054 ✓
- Memory says actuation = 000055, actual = 000055 ✓

**Impact:** Informational only — the actual migrations are sequential and apply cleanly. The memory references are stale from earlier iterations before renumbering.

### Wiring Risks

| Check | Status |
|-------|--------|
| Planner wiring bootable | **YES** — All adapters initialized without error |
| Newest adapters wired correctly | **YES** — Objective, actuation, reflection all initialized |
| All subsystem bridge adapters connected | **YES** — No nil warnings in startup |
| Route conflicts | **NONE** detected |
| Missing handler wiring | **NONE** — All routes respond |

### Governance State

Governance mode is **frozen** (set 2026-04-09). This blocks:
- Policy updates (`freeze_policy_updates: true`)
- Exploration (`freeze_exploration: true`)
- Requires human review (`require_human_review: true`)

This is a **deliberate safety setting**, not a bug. To enable bounded autonomy, governance mode should be transitioned to `autonomous` or `supervised`.

---

## 9. Readiness Assessment

**READY_FOR_BOUNDED_AUTONOMY**

The system is fully operational:
- All code compiles and 1602 tests pass
- All 55 migrations apply cleanly  
- The api-gateway starts without any errors or warnings
- All 31 tested GET endpoints return correct responses
- All 12 tested write-path operations succeed and persist data
- Audit events are properly recorded (16 distinct event types captured during testing)
- No runtime errors, panics, nil pointers, or schema mismatches observed
- All subsystem integrations work (objective↔financial, reflection↔actuation, external actions full lifecycle)

The only prerequisite for bounded autonomy is transitioning governance mode from `frozen` to an appropriate operational mode.

---

## 10. Required Fixes

**No fixes required.**

The system is in a fully functional state. All builds pass, all migrations clean, all endpoints operational, all write-paths verified, zero runtime errors.

### Optional Improvements (Not Required)

1. **Governance mode transition** — Currently `frozen`. Transition to `supervised` or `autonomous` when ready for bounded autonomy operation.
2. **Provider catalog** — Not loaded (directory not found). If LLM-driven processing is needed, configure `PROVIDERS_CATALOG_DIR` or add provider YAML files to `providers/`.
3. **Stale memory references** — Repository memories reference old migration numbers for some subsystems. Non-blocking, informational only.
