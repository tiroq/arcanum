# Runbook

Operational procedures for the Runeforge platform.

---

## Health Checks

### Check all service health

```bash
make health
# Or individually:
curl http://localhost:8080/health  # api-gateway
curl http://localhost:8081/health  # source-sync
curl http://localhost:8082/health  # orchestrator
curl http://localhost:8083/health  # worker
curl http://localhost:8084/health  # writeback
curl http://localhost:8085/health  # notification
```

Expected response:
```json
{"status": "ok", "service": "api-gateway"}
```

### Check NATS

```bash
# JetStream stats
curl http://localhost:8222/jsz

# Server health
curl http://localhost:8222/healthz
```

### Check PostgreSQL

```bash
make db-shell
# In psql:
SELECT count(*) FROM processing_jobs WHERE status = 'queued';
SELECT count(*) FROM processing_jobs WHERE status = 'dead_letter';
SELECT count(*) FROM suggestion_proposals WHERE approval_status = 'pending';
```

---

## Common Failure Modes

### Jobs stuck in `leased` status

**Symptom:** Jobs remain in `leased` status long after the lease expiry time.

**Cause:** Worker crashed mid-execution without releasing the lease.

**Resolution:**
The orchestrator automatically re-queues jobs whose `lease_expiry < NOW()`. If the orchestrator is also down, manually reset:

```sql
UPDATE processing_jobs
SET status = 'queued', leased_at = NULL, lease_expiry = NULL
WHERE status = 'leased' AND lease_expiry < NOW();
```

---

### Jobs accumulating in `queued` status

**Symptom:** `queued` job count grows but `succeeded` count does not.

**Causes:**
1. Worker service is down.
2. LLM provider is unreachable or returning errors.
3. `PROMPTS_PATH` is misconfigured.

**Diagnosis:**
```bash
# Check worker logs
docker compose -f deploy/docker-compose/docker-compose.yml logs worker --tail=50

# Check processing_runs for errors
SELECT error_message, count(*) 
FROM processing_runs 
WHERE outcome = 'failure' 
GROUP BY error_message 
ORDER BY count DESC 
LIMIT 10;
```

**Resolution:** Fix the root cause (restart worker, fix API key, fix PROMPTS_PATH), then jobs will self-recover.

---

### LLM provider timeout / rate limiting

**Symptom:** Processing runs show `LLM timeout` or `429` errors.

**Resolution:**
1. Check provider status page.
2. Adjust `OPENAI_TIMEOUT_SECONDS` or switch to a different provider via config.
3. Jobs will retry automatically up to `RETRY_MAX_ATTEMPTS`.
4. If all retries exhausted, jobs move to `dead_letter` — see [Dead-letter Queue Management](#dead-letter-queue-management).

---

### Writeback operations failing

**Symptom:** `writeback_operations` rows have `status = 'failed'`.

**Causes:**
1. Source API credentials expired (e.g., Google OAuth token).
2. Task was deleted in the source system (`NOT_FOUND`).
3. Rate limiting from source API.

**Diagnosis:**
```sql
SELECT error_code, error_message, count(*)
FROM writeback_operations
WHERE status = 'failed'
GROUP BY error_code, error_message
ORDER BY count DESC;
```

**Resolution:**
- For credential expiry: refresh the OAuth token / rotate the API key in the source connection config.
- For `NOT_FOUND`: the proposal is stale; reject it manually via the API.
- For rate limiting: the writeback service should add exponential backoff on retry (future enhancement).

---

### Source sync falling behind

**Symptom:** `last_synced_at` on source connections is stale.

**Cause:** source-sync service is down or the source API is unreachable.

**Resolution:**
1. Check source-sync logs.
2. Verify source API credentials are valid.
3. Restart source-sync after fixing the root cause.

Tasks will be re-synced on the next poll. Change detection is hash-based, so no duplicates are created.

---

## Debugging Job Failures

### Full failure trace for a job

```sql
-- Find the job
SELECT id, job_type, status, attempt_count, max_attempts
FROM processing_jobs
WHERE source_task_id = '<uuid>';

-- Find all runs for the job
SELECT attempt_number, outcome, duration_ms, error_message, started_at
FROM processing_runs
WHERE job_id = '<uuid>'
ORDER BY attempt_number;

-- Find the source task
SELECT title, description, content_hash
FROM source_tasks
WHERE id = '<source_task_id>';
```

### Recent audit events for an entity

```sql
SELECT event_type, actor_id, payload, occurred_at
FROM audit_events
WHERE entity_id = '<uuid>'
ORDER BY occurred_at DESC
LIMIT 20;
```

---

## Dead-letter Queue Management

Jobs in `dead_letter` status have exhausted all retry attempts. They are not automatically retried.

### List dead-letter jobs

```sql
SELECT j.id, j.job_type, j.attempt_count, j.updated_at,
       t.title AS task_title,
       r.error_message AS last_error
FROM processing_jobs j
JOIN source_tasks t ON t.id = j.source_task_id
LEFT JOIN processing_runs r ON r.job_id = j.id AND r.attempt_number = j.attempt_count
WHERE j.status = 'dead_letter'
ORDER BY j.updated_at DESC;
```

### Re-queue a dead-letter job

After fixing the underlying issue (e.g., bad prompt template, LLM outage), re-queue a job:

```sql
UPDATE processing_jobs
SET status = 'queued',
    attempt_count = 0,
    leased_at = NULL,
    lease_expiry = NULL,
    scheduled_at = NULL,
    updated_at = NOW()
WHERE id = '<uuid>'
  AND status = 'dead_letter';
```

### Re-queue all dead-letter jobs

```sql
UPDATE processing_jobs
SET status = 'queued',
    attempt_count = 0,
    leased_at = NULL,
    lease_expiry = NULL,
    scheduled_at = NULL,
    updated_at = NOW()
WHERE status = 'dead_letter';
```

---

## Database Maintenance

### Check table sizes

```sql
SELECT relname AS table,
       pg_size_pretty(pg_total_relation_size(oid)) AS total_size
FROM pg_class
WHERE relkind = 'r'
  AND relnamespace = 'public'::regnamespace
ORDER BY pg_total_relation_size(oid) DESC;
```

### Archive old audit events

`audit_events` grows unboundedly. Archive events older than 90 days to cold storage:

```sql
-- Move to archive table (create first if needed)
INSERT INTO audit_events_archive
SELECT * FROM audit_events WHERE occurred_at < NOW() - INTERVAL '90 days';

DELETE FROM audit_events WHERE occurred_at < NOW() - INTERVAL '90 days';
```

### Vacuum

```sql
VACUUM ANALYZE processing_jobs;
VACUUM ANALYZE audit_events;
```

---

## Restarting Services

### Rolling restart (Docker Compose)

```bash
docker compose -f deploy/docker-compose/docker-compose.yml restart worker
docker compose -f deploy/docker-compose/docker-compose.yml restart orchestrator
```

### Full restart

```bash
make docker-down && make docker-up
```

Infrastructure (postgres, nats) data is preserved in named volumes.

### Wipe and start fresh (development only)

```bash
docker compose -f deploy/docker-compose/docker-compose.yml down -v
make docker-up
make migrate-up
```
