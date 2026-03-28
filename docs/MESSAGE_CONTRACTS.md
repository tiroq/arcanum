# Message Contracts

All inter-service communication uses NATS JetStream. Subject strings are defined as constants in `internal/contracts/subjects/subjects.go`. Event payload schemas are defined in `internal/contracts/events/events.go`.

**Hardcoding subject strings outside of `subjects.go` is forbidden** — the enforcement test at `internal/contracts/enforcement_test.go` verifies this.

All events carry a `"version": "v1"` field for future schema evolution.

---

## Stream Configuration

| Stream name | Subjects | Retention |
|---|---|---|
| `RUNEFORGE` | `runeforge.>` | `LimitsPolicy` with `MaxAge = 7d` |

---

## Subjects and Payloads

### `runeforge.source.task.detected`

Published by: `source-sync`  
Consumed by: `orchestrator`

Fired when a task is seen for the first time (new) or when it has been absent for a full poll cycle (re-appeared).

```json
{
  "version": "v1",
  "source_task_id": "uuid",
  "source_connection_id": "uuid",
  "external_id": "google-tasks-task-id",
  "change_type": "new | reappeared",
  "detected_at": "2024-01-15T10:30:00Z"
}
```

---

### `runeforge.source.task.changed`

Published by: `source-sync`  
Consumed by: `orchestrator`

Fired when an existing task's `content_hash` changes between polls.

```json
{
  "version": "v1",
  "source_task_id": "uuid",
  "previous_hash": "sha256-of-previous-content",
  "new_hash": "sha256-of-new-content",
  "changed_at": "2024-01-15T10:31:00Z"
}
```

---

### `runeforge.job.created`

Published by: `source-sync`  
Consumed by: `worker`

Emitted by `source-sync` immediately after enqueueing a DB job, signaling that a processing job is ready to be leased.

```json
{
  "version": "v1",
  "job_id": "uuid",
  "source_task_id": "uuid",
  "job_type": "title_rewrite | description_rewrite | routing | decomposition | priority",
  "priority": 0,
  "dedupe_key": "source_task_id:job_type"
}
```

---

### `runeforge.job.retry`

Published by: `orchestrator`  
Consumed by: `orchestrator` (self, to re-enqueue after backoff)

```json
{
  "version": "v1",
  "job_id": "uuid",
  "attempt_count": 2,
  "reason": "LLM timeout",
  "retry_at": "2024-01-15T10:35:00Z"
}
```

---

### `runeforge.job.dead`

Published by: `orchestrator`  
Consumed by: `notification` (optional alerting)

Fired when a job exceeds `max_attempts`.

```json
{
  "version": "v1",
  "job_id": "uuid",
  "reason": "exceeded max attempts: LLM timeout",
  "dead_at": "2024-01-15T11:00:00Z"
}
```

---

### `runeforge.proposal.created`

Published by: `worker`  
Consumed by: `orchestrator` (auto-approve check), `notification` (human review alert)

```json
{
  "version": "v1",
  "proposal_id": "uuid",
  "source_task_id": "uuid",
  "proposal_type": "title_rewrite",
  "human_review_required": true
}
```

---

### `runeforge.proposal.approved`

Published by: `api-gateway` (on human approval) or `orchestrator` (on auto-approve)  
Consumed by: `writeback`

```json
{
  "version": "v1",
  "proposal_id": "uuid",
  "approved_by": "user:abc123 | system",
  "auto_approved": false,
  "approved_at": "2024-01-15T12:00:00Z"
}
```

---

### `runeforge.writeback.requested`

Published by: `writeback` (internal, after creating the WritebackOperation row)  
Consumed by: `writeback` (self, for retry/deduplication)

```json
{
  "version": "v1",
  "writeback_id": "uuid",
  "proposal_id": "uuid",
  "source_task_id": "uuid",
  "operation_type": "update_title | update_description | update_list"
}
```

---

### `runeforge.writeback.completed`

Published by: `writeback`  
Consumed by: `source-sync` (optional: trigger re-sync to verify), `notification`

```json
{
  "version": "v1",
  "writeback_id": "uuid",
  "verified": true,
  "completed_at": "2024-01-15T12:01:00Z"
}
```

---

### `runeforge.writeback.failed`

Published by: `writeback`  
Consumed by: `notification`, `orchestrator` (may trigger re-queue)

```json
{
  "version": "v1",
  "writeback_id": "uuid",
  "error_code": "RATE_LIMITED | UNAUTHORIZED | NOT_FOUND | UNKNOWN",
  "error_message": "Google Tasks API returned 429",
  "failed_at": "2024-01-15T12:01:00Z"
}
```

---

### `runeforge.notify.requested`

Published by: any service  
Consumed by: `notification`

Generic notification envelope. The notification service routes based on `notification_type`.

```json
{
  "version": "v1",
  "notification_type": "proposal.pending_review | job.dead | writeback.failed",
  "recipient": "admin@example.com | webhook:https://...",
  "payload_json": "{\"proposal_id\": \"...\", ...}"
}
```

---

## Schema Evolution

- All events carry `"version": "v1"`.
- Adding optional fields is backward-compatible; consumers must tolerate unknown fields.
- Removing or renaming fields requires a new version (`v2`) and a transition period.
- Subject string changes require updating `subjects.go` and all consumers simultaneously (rare).
