# Arcanum End-to-End Validation Report

**Date:** 2026-04-06  
**Duration:** ~3 hours  
**Engineer:** GitHub Copilot (autonomous agent)  
**Environment:** Local — PostgreSQL 16 (Docker), NATS 2.10 JetStream (Docker), Ollama (systemd), 6 Go binaries  
**Overall Verdict:** ⚠️ PARTIAL — Core pipeline functional; 7 bugs identified (2 HIGH, 4 MEDIUM, 1 LOW)

---

## Phase Summary

| Phase | Name | Result |
|-------|------|--------|
| 1 | Clean startup & health checks | ✅ PASS |
| 2 | Baseline happy path | ✅ PASS |
| 3 | Retry & failure handling | ✅ PASS |
| 4 | Lease & concurrency safety | ✅ PASS |
| 5 | Multi-provider routing | ✅ PASS |
| 6 | Optimizer | ⚠️ PARTIAL |
| 7 | Control loop | ✅ PASS |
| 8 | API correctness | ⚠️ PARTIAL |
| 9 | Data integrity | ✅ PASS |
| 10 | Chaos / edge cases | ⚠️ PARTIAL |

---

## Phase 1 — Clean Startup & Health Checks ✅

**Pre-conditions:** All 6 binaries rebuilt from source.  
**Issue resolved:** api-gateway conflicted with cAdvisor on port 8080 → fixed via `SVC_PORT_API_GATEWAY=8090` in `.env`.

| Service | Port | Health | Ready |
|---------|------|--------|-------|
| source-sync | 8081 | ✅ | ✅ |
| orchestrator | 8082 | ✅ | ✅ |
| worker | 8083 | ✅ | ✅ |
| writeback | 8084 | ✅ | ✅ |
| notification | 8085 | ✅ | ✅ |
| api-gateway | 8090 | ✅ | ✅ |

All services responded healthy within 5 seconds of launch.

---

## Phase 2 — Baseline Happy Path ✅

**Test:** `POST /api/v1/source-tasks/a1111111.../resync`  
**Traced job:** `23f439d8-98bb-448f-9ece-de39484ac79f`

Pipeline trace:

```
queued → leased → running → succeeded
                               └─ suggestion_proposals row created
                               └─ NATS: runeforge.proposal.created published
                               └─ Telegram notification delivered
```

`result_payload` fields populated:
- `tokens_total`, `tokens_prompt`, `tokens_completion` ✅
- `used_fallback`, `attempt_number`, `execution_trace` ✅

Audit events captured: `job.leased`, `llm.started`, `llm.finished`, `proposal.created`, `job.completed` ✅  
Missing: `job.created` → **BUG #1** (see below)

---

## Phase 3 — Retry & Failure Handling ✅

**Test:** Both jobs processed with Ollama stopped (connection refused).  
**Outcomes:**
- `attempt_count` incremented correctly on each failure
- Backoff scheduling applied (`retry_scheduled` status with `scheduled_at` in future)
- After 3 attempts: status → `dead_letter`
- Control loop correctly requeued overdue `retry_scheduled` jobs (logged: `requeued N jobs`)
- Dead-letter records confirmed: `attempt_count = max_attempts = 3` ✅

Missing NATS events: `runeforge.job.retry` and `runeforge.job.dead` → **BUG #3 / #4** (0 delivered to notification consumers)

---

## Phase 4 — Lease & Concurrency Safety ✅

**Test:** Injected fake leased job with `lease_expiry` set 10 minutes in the past.

```sql
INSERT INTO processing_jobs (id, ..., status='leased', lease_expiry=NOW()-600s)
```

- Control loop reclaimed it within 35s: `"control: reclaimed expired leases" count=1` ✅
- `control.alert.lease_expired` NATS event published and delivered ✅
- Worker picked up reclaimed job and completed it successfully ✅
- Ownership guard (`leased_by_worker_id` check) prevents stale goroutine from completing a reclaimed job ✅

---

## Phase 5 — Multi-Provider Routing ✅

**Test:** `llm_routing` job injected directly into DB with `model_role=fast`

- Routed to `ollama/fast` model (qwen3:1.7b) ✅
- `used_fallback = false` ✅
- `execution_trace` populated with routing metadata ✅
- Cloud providers (OpenAI, OpenRouter) correctly bypassed due to empty API keys ✅

Fallback demonstration not completed — cloud providers not configured in this environment.

---

## Phase 6 — Optimizer ⚠️

**Test:** Ran optimizer unit tests; queried real DB for provider stats.

- All 33 optimizer unit tests pass ✅
- `AnalyzeAndRecommend()` is well-implemented ✅
- **NOT wired**: no API endpoint, no control loop integration, no scheduled invocation → **OBS #1**
- Real data: `ollama/default` has 25+ runs, 0 failures, low acceptance rate (< 40% threshold)
  - Optimizer would recommend `increase_escalation` — likely a false signal since most proposals are in `pending` status

---

## Phase 7 — Control Loop ✅

**Verified on each tick (30s interval):**
1. `ReclaimExpiredLeases` — reclaimed 1 expired lease ✅  
2. `RequeueScheduledRetries` — requeued 2 overdue retries (total 4 across session) ✅  
3. `QueueStats()` — backlog count evaluated, `queue_backlog` alert not triggered (count < threshold of 50) ✅  
4. NATS alerts published: `control.alert.lease_expired`, `control.alert.retry_overdue` ✅

---

## Phase 8 — API Correctness ⚠️

### Response Format: PascalCase JSON — **BUG #5** (MEDIUM)

All API responses use PascalCase field names. No `json:"..."` struct tags on response models.

**Affected endpoints:** `/jobs`, `/proposals`, `/audit-events`, `/source-connections`, `/source-tasks`

```json
// Actual (broken):
{"ID": "...", "SourceTaskID": "...", "JobType": "llm_rewrite", ...}

// Expected:
{"id": "...", "source_task_id": "...", "job_type": "llm_rewrite", ...}
```

**File:** All response structs in `internal/db/models/models.go` and inline structs in `internal/api/handlers.go`

---

### Metrics Summary: Wrong Status Query — **BUG #6** (MEDIUM)

`GET /api/v1/metrics/summary` queries `status = 'running'` which does not exist.  
The actual active-job status is `'leased'`.

```go
// internal/api/handlers.go:575 (broken)
countByStatus(ctx, db, "running")  // 'running' never exists in DB

// Should be:
countByStatus(ctx, db, "leased")
```

Also missing from summary: `retry_scheduled` count.  
Current response:
```json
{"jobs_dead":2,"jobs_failed":0,"jobs_queued":0,"jobs_running":0,"jobs_succeeded":27}
```

Expected additional fields: `jobs_leased`, `jobs_retry_scheduled`

---

### Pagination Naming — API Documentation Issue

Pagination params are `page` and `per_page` (not `limit`/`offset`).  
Calling `?limit=50` silently falls back to default per_page=20. Should be documented.

---

### Missing Endpoint

`GET /api/v1/routing-recommendations` → 404. Optimizer not exposed via API.

---

## Phase 9 — Data Integrity ✅

All integrity checks passed:

| Check | Result |
|-------|--------|
| Succeeded jobs without proposal | 0 ✅ |
| Orphan proposals (no parent job) | 0 ✅ |
| Stuck leased jobs | 0 ✅ |
| Overdue `retry_scheduled` jobs | 0 ✅ |
| Duplicate proposals per job | 0 ✅ |
| `dead_letter` jobs with `attempt_count ≠ max_attempts` | 0 ✅ |
| proposals count == succeeded job count | 27 == 27 ✅ |

Final DB state at report time:
- `succeeded`: 27 jobs, 27 proposals
- `dead_letter`: 2 jobs (attempt_count=3=max_attempts)
- `queued`: 0
- Audit events: 58

---

## Phase 10 — Chaos / Edge Cases ⚠️

### Unknown Job Type — Permanent Stuck State — **BUG #7** (MEDIUM)

Injected a job with `job_type = 'nonexistent_type'` into the DB.

**Result:** Job remained in `queued` state indefinitely — never leased, never failed, no alert published.

**Root cause:** `Lease()` uses `job_type = ANY($4)` with a hardcoded whitelist in `worker.go:119`.  
Unknown types are silently excluded from lease consideration.

**Impact:** Any job with an unrecognized `job_type` will permanently occupy the queue with no visibility, no SLA, and no recovery path. Violates the "No Permanent Stuck States" design principle.

**File:** `internal/worker/worker.go:119` / `internal/jobs/queue.go:Lease()`

---

### Duplicate Job Creation — **BUG #2** (HIGH) — Re-confirmed

Two rapid `POST /resync` calls for the same `source_task_id` created two separate jobs (36ms apart).  
No `dedupe_key` set, no DB uniqueness constraint.  
Both jobs processed, creating duplicate proposals.

---

### API Input Validation ✅

| Test | Status Code | Body |
|------|-------------|------|
| Invalid UUID in path | 400 | `{"error":"invalid id"}` |
| Wrong HTTP method | 405 | `{"error":"method not allowed"}` |
| Missing auth token | 401 | `{"error":"missing X-Admin-Token header"}` |
| Wrong auth token | 401 | `{"error":"invalid admin token"}` |

All error responses include `request_id` for traceability ✅

---

## Bug Registry

| ID | Severity | Component | Description | File |
|----|----------|-----------|-------------|------|
| BUG#1 | HIGH | Orchestrator | `job.created` + `job.reclaimed` audit events never recorded — queue missing `.WithAudit(auditor)` | `cmd/orchestrator/main.go:96` |
| BUG#2 | HIGH | Orchestrator | No `DedupeKey` in `EnqueueParams` → duplicate jobs per source_task | `internal/orchestrator/orchestrator.go:~134` |
| BUG#3 | MEDIUM | Worker | `job.dead` NATS event never published — `failJob()` has no NATS publish for dead-letter | `internal/worker/runner.go:failJob()` |
| BUG#4 | MEDIUM | Worker | `job.retry` NATS event never published — same root cause as BUG#3 | `internal/worker/runner.go:failJob()` |
| BUG#5 | MEDIUM | API Gateway | All API responses use PascalCase JSON — missing `json:"snake_case"` tags | `internal/db/models/models.go` |
| BUG#6 | MEDIUM | API Gateway | MetricsSummary queries `status='running'` (never exists); actual is `'leased'`; missing `retry_scheduled` | `internal/api/handlers.go:575` |
| BUG#7 | MEDIUM | Worker | Jobs with unknown `job_type` permanently stuck in `queued` — no failure, no alert | `internal/worker/worker.go:119` |
| OBS#1 | LOW | Control | `AnalyzeAndRecommend()` never invoked in production — no endpoint, no schedule | `internal/control/optimizer.go` |
| CONFIG#1 | LOW | Config | `RETRY_INITIAL_INTERVAL_SECONDS=5` in `.env` unused; backoff hardcoded to `(n+1)^2 * 30s` | `internal/jobs/queue.go:234` |

---

## Recommended Fixes

### Immediate (HIGH)

**BUG#1 — Wire auditor:**
```go
// cmd/orchestrator/main.go:96
queue := jobs.NewQueue(pool, logger).WithAudit(auditor)
```

**BUG#2 — Add DedupeKey + DB constraint:**
```go
// internal/orchestrator/orchestrator.go
DedupeKey: pgtype.Text{String: task.ID.String() + ":llm_rewrite", Valid: true},
```
```sql
CREATE UNIQUE INDEX ON processing_jobs (dedupe_key) WHERE dedupe_key IS NOT NULL;
```

### High-Priority (MEDIUM)

**BUG#3+4 — Publish NATS events on failure:**
```go
// internal/worker/runner.go: failJob()
if newStatus == models.JobStatusDeadLetter {
    w.bus.Publish(ctx, subjects.SubjectJobDead, ...)
} else {
    w.bus.Publish(ctx, subjects.SubjectJobRetry, ...)
}
```

**BUG#5 — Add JSON tags to all model structs**

**BUG#6 — Fix metrics query:**
```go
"jobs_leased":          countByStatus(ctx, db, "leased"),
"jobs_retry_scheduled": countByStatus(ctx, db, "retry_scheduled"),
```

**BUG#7 — Fail unknown job types explicitly:**
```go
// internal/worker/runner.go after Lease()
if !knownJobType(job.JobType) {
    w.queue.Fail(ctx, job.ID, w.workerID, "UNKNOWN_JOB_TYPE", ...)
    return
}
```

### Later (LOW)

- **OBS#1:** Expose optimizer via `POST /api/v1/optimizer/analyze` or integrate into control loop tick
- **CONFIG#1:** Read `RETRY_INITIAL_INTERVAL_SECONDS` from config

---

## Final System Integrity Assessment

| Dimension | Verdict | Notes |
|-----------|---------|-------|
| Core pipeline execution | ✅ PASS | Happy path fully operational |
| Retry/dead-letter lifecycle | ✅ PASS | State machine correct |
| Lease expiry recovery | ✅ PASS | Control loop reclaims correctly |
| Audit trail completeness | ⚠️ PARTIAL | Missing `job.created` (BUG#1) |
| NATS event completeness | ⚠️ PARTIAL | Missing `job.retry`, `job.dead` (BUG#3,4) |
| API JSON correctness | ⚠️ PARTIAL | PascalCase keys (BUG#5) |
| API metrics correctness | ⚠️ PARTIAL | Wrong status query (BUG#6) |
| Data referential integrity | ✅ PASS | All FK relations intact, 0 orphans |
| Unknown input handling | ⚠️ PARTIAL | Unknown job_type → stuck forever (BUG#7) |
| Auth and error handling | ✅ PASS | 400/401/405 all correct, request_id present |
| Duplicate prevention | ❌ FAIL | No dedup on resync (BUG#2) |
| Optimizer operational | ❌ NOT WIRED | Tests pass, never invoked (OBS#1) |
