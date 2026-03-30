# Data Model

Runeforge uses PostgreSQL. All tables use UUIDs as primary keys and include `created_at` / `updated_at` timestamps unless otherwise noted.

---

## 1. source_connections

Represents an integration with an upstream task source (e.g., Google Tasks workspace).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | Unique identifier |
| `name` | TEXT | NOT NULL | Human-friendly label, e.g. "Work Google Tasks" |
| `provider` | TEXT | NOT NULL | Provider slug, e.g. `google_tasks` |
| `config` | JSONB | NOT NULL | Provider-specific config (API keys, list IDs). Stored encrypted at rest in production. |
| `enabled` | BOOLEAN | NOT NULL DEFAULT true | When false, source-sync skips this connection |
| `last_synced_at` | TIMESTAMPTZ | NULLABLE | Timestamp of last successful sync poll |
| `created_at` | TIMESTAMPTZ | NOT NULL | |
| `updated_at` | TIMESTAMPTZ | NOT NULL | |

**Relationships:** One-to-many with `source_tasks`.

**Example:**
```json
{
  "id": "01920a1b-0000-7000-8000-000000000001",
  "name": "Personal Google Tasks",
  "provider": "google_tasks",
  "config": {"list_id": "MDEyMzQ1Njc4OQ"},
  "enabled": true,
  "last_synced_at": "2024-01-15T10:30:00Z"
}
```

---

## 2. source_tasks

Represents a single task mirrored from a source connection. This is a live mirror; it reflects the current known state of the external task.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | Internal identifier |
| `source_connection_id` | UUID | FK → source_connections | Which connection this task belongs to |
| `external_id` | TEXT | NOT NULL | The ID assigned by the upstream source |
| `title` | TEXT | NOT NULL | Task title as fetched from source |
| `description` | TEXT | NULLABLE | Task description/notes |
| `raw_payload` | JSONB | NOT NULL | Full raw API response for this task |
| `content_hash` | TEXT | NOT NULL | SHA-256 of the normalized content; used for change detection |
| `status` | TEXT | NOT NULL | Lifecycle status: `active`, `completed`, `deleted` |
| `priority` | INT | NOT NULL DEFAULT 0 | Priority integer from source (0 = none) |
| `due_at` | TIMESTAMPTZ | NULLABLE | Due date from source |
| `created_at` | TIMESTAMPTZ | NOT NULL | |
| `updated_at` | TIMESTAMPTZ | NOT NULL | Updated whenever source data changes |

**Unique constraint:** `(source_connection_id, external_id)`

**Relationships:** Many-to-one with `source_connections`. One-to-many with `source_task_snapshots` and `processing_jobs`.

---

## 3. source_task_snapshots

Append-only point-in-time snapshots of a source task. Created every time source-sync detects a content change. Used for full change history and rollback capability.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | |
| `source_task_id` | UUID | FK → source_tasks | Parent task |
| `snapshot_version` | INT | NOT NULL | Monotonically increasing version number per task |
| `content_hash` | TEXT | NOT NULL | Hash of content at snapshot time |
| `raw_payload` | JSONB | NOT NULL | Full raw payload at snapshot time |
| `snapshot_taken_at` | TIMESTAMPTZ | NOT NULL | When the snapshot was captured |

**No `updated_at`:** This table is strictly append-only. Rows are never updated.

**Unique constraint:** `(source_task_id, snapshot_version)`

---

## 4. processing_jobs

Represents a unit of work to be executed by the worker service. Created by the orchestrator when a task is detected or changed.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | |
| `source_task_id` | UUID | FK → source_tasks | The task this job processes |
| `job_type` | TEXT | NOT NULL | E.g. `title_rewrite`, `description_rewrite`, `routing`, `decomposition`, `priority` |
| `status` | TEXT | NOT NULL | `queued`, `leased`, `running`, `succeeded`, `failed`, `retry_scheduled`, `dead_letter` |
| `priority` | INT | NOT NULL DEFAULT 0 | Higher value = processed first |
| `dedupe_key` | TEXT | NULLABLE | Used to prevent duplicate jobs for the same task+type |
| `attempt_count` | INT | NOT NULL DEFAULT 0 | Number of times this job has been attempted |
| `max_attempts` | INT | NOT NULL DEFAULT 3 | Maximum attempts before dead-lettering |
| `payload` | JSONB | NOT NULL | Job-specific input data |
| `leased_at` | TIMESTAMPTZ | NULLABLE | When the worker took the lease |
| `lease_expiry` | TIMESTAMPTZ | NULLABLE | Lease expiry; jobs with expired leases are re-queued |
| `scheduled_at` | TIMESTAMPTZ | NULLABLE | For retry scheduling; NULL means immediately eligible |
| `created_at` | TIMESTAMPTZ | NOT NULL | |
| `updated_at` | TIMESTAMPTZ | NOT NULL | |

**Status transitions:** See [Architecture - Job Lifecycle](ARCHITECTURE.md).

---

## 5. processing_runs

Records the result of a single execution attempt of a `ProcessingJob`. One row per attempt.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | |
| `job_id` | UUID | FK → processing_jobs | Parent job |
| `attempt_number` | INT | NOT NULL | 1-indexed attempt number |
| `outcome` | TEXT | NOT NULL | `success`, `failure`, `error` |
| `started_at` | TIMESTAMPTZ | NOT NULL | When the worker started this run |
| `finished_at` | TIMESTAMPTZ | NULLABLE | When the run ended (NULL if still running) |
| `duration_ms` | BIGINT | NULLABLE | Wall-clock duration in milliseconds |
| `error_message` | TEXT | NULLABLE | Error description on failure |
| `result_payload` | JSONB | NOT NULL | Raw LLM response or processor output |
| `worker_id` | TEXT | NULLABLE | Hostname or pod name of the worker that ran this |

**Immutable:** Rows are never updated after `finished_at` is set.

---

## 6. suggestion_proposals

An AI-generated suggestion awaiting human or automatic approval. Created by the worker after a successful processing run.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | |
| `source_task_id` | UUID | FK → source_tasks | Task this proposal modifies |
| `job_id` | UUID | FK → processing_jobs | Job that produced this proposal |
| `proposal_type` | TEXT | NOT NULL | Mirrors `job_type`: `title_rewrite`, `routing`, etc. |
| `approval_status` | TEXT | NOT NULL | `pending`, `approved`, `rejected` |
| `human_review_required` | BOOLEAN | NOT NULL | When true, auto-approve is bypassed |
| `proposal_payload` | JSONB | NOT NULL | The proposed change (original + proposed values) |
| `approved_by` | TEXT | NULLABLE | Actor ID who approved/rejected (user ID or "system") |
| `auto_approved` | BOOLEAN | NOT NULL DEFAULT false | True if approved without human interaction |
| `reviewed_at` | TIMESTAMPTZ | NULLABLE | When the proposal was reviewed |
| `created_at` | TIMESTAMPTZ | NOT NULL | |
| `updated_at` | TIMESTAMPTZ | NOT NULL | |

**Relationships:** One-to-one with `writeback_operations` (created only after approval).

---

## 7. writeback_operations

Represents the execution of an approved proposal back to the source system.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | |
| `proposal_id` | UUID | FK → suggestion_proposals | Parent proposal |
| `source_task_id` | UUID | FK → source_tasks | Task being modified |
| `operation_type` | TEXT | NOT NULL | What is being changed, e.g. `update_title` |
| `status` | TEXT | NOT NULL | `pending`, `executing`, `completed`, `failed`, `verified` |
| `request_payload` | JSONB | NOT NULL | The payload sent to the source API |
| `response_payload` | JSONB | NULLABLE | The raw response from the source API |
| `verified` | BOOLEAN | NOT NULL DEFAULT false | True if post-write read confirmed the change |
| `error_code` | TEXT | NULLABLE | Error code on failure |
| `error_message` | TEXT | NULLABLE | Error detail on failure |
| `executed_at` | TIMESTAMPTZ | NULLABLE | When the write was attempted |
| `completed_at` | TIMESTAMPTZ | NULLABLE | When the operation reached a terminal state |
| `created_at` | TIMESTAMPTZ | NOT NULL | |
| `updated_at` | TIMESTAMPTZ | NOT NULL | |

---

## 8. audit_events

Append-only log of all significant platform events. Never updated or deleted. Used for compliance and debugging.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PK | |
| `entity_type` | TEXT | NOT NULL | E.g. `source_connection`, `processing_job`, `suggestion_proposal` |
| `entity_id` | UUID | NOT NULL | ID of the entity involved |
| `event_type` | TEXT | NOT NULL | E.g. `job.created`, `proposal.approved`, `writeback.completed` |
| `actor_type` | TEXT | NOT NULL | `system` or `user` |
| `actor_id` | TEXT | NOT NULL | System service name or user ID |
| `payload` | JSONB | NOT NULL | Full event data snapshot |
| `occurred_at` | TIMESTAMPTZ | NOT NULL | When the event occurred (not inserted_at) |

**No `updated_at`:** Rows are immutable after insert.

**Partitioning consideration:** In high-volume deployments, partition by `occurred_at` monthly.

---

## Entity Relationship Summary

```
source_connections (1)
  └── source_tasks (N)
        ├── source_task_snapshots (N)   [append-only]
        └── processing_jobs (N)
              ├── processing_runs (N)   [append-only per attempt]
              └── suggestion_proposals (N)
                    └── writeback_operations (1)

audit_events                            [append-only, references any entity]
```
