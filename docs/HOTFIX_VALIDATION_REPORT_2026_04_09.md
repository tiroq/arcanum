# Hotfix Validation Report — 2026-04-09

## Iteration: HOTFIX — EXECUTION + REPLAY PERSISTENCE UNBLOCK

**Date**: 2026-04-09  
**Scope**: Fix 3 blocking issues from [VALIDATION_RUN_2026_04_09.md](VALIDATION_RUN_2026_04_09.md)  
**Constraint**: Smallest safe fix — no architecture redesign, no governance/scoring changes

---

## Issues Fixed

### R-1: Executor Auth Header Mismatch (100% Action Failure)

**Root Cause**: `internal/agent/actions/executor.go:doPost()` sent `Authorization: Bearer <token>` but the API middleware (`internal/api/middleware.go:authMiddleware`) expects `X-Admin-Token: <token>`.

**Fix**: Changed line 122 of `executor.go`:
```go
// Before (broken):
req.Header.Set("Authorization", "Bearer "+e.apiToken)

// After (fixed):
req.Header.Set("X-Admin-Token", e.apiToken)
```

**Files Changed**: `internal/agent/actions/executor.go` (1 line)

---

### A-1: Replay Packs Not Persisted (0 rows in `agent_replay_packs`)

**Root Cause**: In `internal/agent/decision_graph/planner_adapter.go:EvaluateForPlanner()`, the replay recording code (line 731) was unreachable when the decision graph agreed with tactical selection. An early return at line 635-637 exited before reaching the `RecordReplayPack` call.

**Fix**: Removed the early return from the agreement branch. Restructured flow so that:
- Decision ID, path metadata, snapshot capture, resource metrics, and replay recording execute **regardless** of whether graph agrees or overrides
- Override-specific logic (Applied flag, provider routing, audit override event, governance review) is guarded by `if override.Applied` / `if !graphAgreesWithTactical`

**Files Changed**: `internal/agent/decision_graph/planner_adapter.go` (4 edits)

---

### A-2: Path Snapshots Not Persisted (0 rows in `agent_path_decision_snapshots`)

**Root Cause**: Same as A-1 — the `CaptureAndSave` call (line 684) was also blocked by the same early return at line 635-637.

**Fix**: Same structural change as A-1. Snapshot capture now executes unconditionally after path selection.

**Files Changed**: Same file as A-1 (included in the same 4 edits)

---

## Changes Summary

| File | Change | Lines |
|------|--------|-------|
| `internal/agent/actions/executor.go` | Auth header: `Authorization: Bearer` → `X-Admin-Token` | 1 |
| `internal/agent/decision_graph/planner_adapter.go` | Remove early return; guard override-only logic; always run snapshot+replay | ~30 |
| `internal/agent/actions/executor_test.go` | Update test assertion to expect `X-Admin-Token` | 2 |
| `internal/agent/decision_graph/decision_graph_test.go` | Add 2 integration tests (agrees + overrides) | ~110 |

**Total**: 4 files, ~143 lines changed. No new packages, no new migrations, no architecture changes.

---

## Test Results

### Unit Tests

| Package | Tests | Status |
|---------|-------|--------|
| `internal/agent/actions` | 7 (including updated auth test) | ✅ PASS |
| `internal/agent/decision_graph` | All + 2 new hotfix tests | ✅ PASS |
| `internal/agent/...` (all 25 packages) | Full suite | ✅ PASS |
| Full build (`go build ./...`) | Compilation | ✅ PASS |

### New Hotfix Tests

1. **`TestEvaluateForPlanner_RecordsReplayAndSnapshot_WhenGraphAgreesWithTactical`**: Verifies mock replay recorder and snapshot capturer are called even when graph's first action matches tactical selection. Asserts both share the same decisionID.
2. **`TestEvaluateForPlanner_RecordsReplayAndSnapshot_WhenGraphOverrides`**: Verifies replay and snapshot are recorded when graph overrides tactical (regression guard for existing behavior).

---

## Runtime Validation

### Configuration
- **Services**: api-gateway (port 8090), worker (port 8083)
- **Governance**: mode=frozen, freeze_learning=false, freeze_exploration=true, freeze_policy_updates=true, require_human_review=true
- **LLM**: ollama, qwen3:1.7b (local)
- **Data**: 7 failed + 14 dead-lettered processing jobs (dead_letter_rate 10.4% > 10% threshold)

### R-1 Validation

| Test | Expected | Actual | Status |
|------|----------|--------|--------|
| POST with `X-Admin-Token` header | Accepted (past auth) | HTTP 400 (invalid UUID, past auth) | ✅ |
| POST with `Authorization: Bearer` header | Rejected | HTTP 401 (missing X-Admin-Token) | ✅ |
| Agent self-actions via `/api/v1/agent/run-actions` | `"status": "executed"` | Both actions `"executed"` (durations: 2208ms, 890ms) | ✅ |

### A-1 / A-2 Validation

| Table | Before Hotfix | After Hotfix | Status |
|-------|---------------|--------------|--------|
| `agent_replay_packs` | 0 rows | 1 row | ✅ |
| `agent_path_decision_snapshots` | 0 rows | 1 row | ✅ |

**Replay Pack Row**:
- decision_id: `fb0d0985-c857-4e96-8b01-d8a8c9f6c9bb`
- goal_type: `investigate_failed_jobs`
- selected_mode: `direct`
- selected_path: `log_recommendation`
- confidence: `0.95`

**Snapshot Row**:
- decision_id: `fb0d0985-c857-4e96-8b01-d8a8c9f6c9bb` (matches replay)
- goal_type: `investigate_failed_jobs`
- selected_path: `log_recommendation`
- selected_score: `0.587`

Both rows share the same `decision_id`, confirming the full decision→replay→snapshot chain is intact.

### Full Agent Cycle Output

```json
{
  "cycle_id": "a23cb03e-4a21-4f5c-b765-264a43535dab",
  "planned": 2,
  "executed": 2,
  "failed": 0
}
```

Goal derived: `investigate_failed_jobs` (dead_letter_rate 10.4% > 10% threshold)  
Actions planned: 2x `retry_job` for dead-lettered test jobs  
Actions executed: 2/2 successful  
Replay recorded: ✅  
Snapshot captured: ✅  

---

## Regression Safety

- No governance changes
- No scoring/weight changes
- No migration changes
- No new packages or dependencies
- All 25 existing test packages pass unchanged
- Full `go build ./...` compiles clean
- Override path (graph differs from tactical) continues to work correctly (verified by second hotfix test)

---

## Rollout Recommendation

### **UNBLOCKED_FOR_REVALIDATION**

All three blocking issues are resolved. The autonomous agent can now:
1. Execute self-actions (retry_job, trigger_resync) through the API without auth failures
2. Persist decision replay packs for post-hoc review
3. Persist decision snapshots for comparative learning

The system is ready for a full re-validation run under governance control to confirm end-to-end autonomous operation.
