# API Reference

Base URL: `http://localhost:8080` (development)

## Authentication

All endpoints (except `/health` and `/metrics`) require the `Admin-Token` header:

```
Admin-Token: <your-admin-token>
```

The token is configured via the `ADMIN_TOKEN` environment variable. Requests without a valid token receive `401 Unauthorized`.

---

## Health & Observability

### `GET /health`

Returns service health. No authentication required.

**Response 200:**
```json
{
  "status": "ok",
  "service": "api-gateway",
  "uptime_seconds": 3600
}
```

---

### `GET /metrics`

Prometheus metrics endpoint. No authentication required.

---

## Source Connections

### `GET /v1/source-connections`

List all source connections.

**Query params:** `page` (default 1), `per_page` (default 20, max 100)

**Response 200:**
```json
[
  {
    "id": "01920a1b-0000-7000-8000-000000000001",
    "name": "Work Google Tasks",
    "provider": "google_tasks",
    "enabled": true,
    "last_synced_at": "2024-01-15T10:30:00Z",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-15T10:30:00Z"
  }
]
```

**Example:**
```bash
curl -H "Admin-Token: change-me" http://localhost:8080/v1/source-connections
```

---

### `POST /v1/source-connections`

Create a new source connection.

**Request body:**
```json
{
  "name": "Work Google Tasks",
  "provider": "google_tasks",
  "config": {
    "list_id": "MDEyMzQ1Njc4OQ"
  }
}
```

**Response 201:** Created source connection object.

**Example:**
```bash
curl -X POST -H "Admin-Token: change-me" \
  -H "Content-Type: application/json" \
  -d '{"name":"Work Tasks","provider":"google_tasks","config":{"list_id":"abc"}}' \
  http://localhost:8080/v1/source-connections
```

---

### `GET /v1/source-connections/{id}`

Get a single source connection by ID.

**Response 200:** Source connection object.  
**Response 404:** `{"error": "not found"}`

---

### `PUT /v1/source-connections/{id}`

Update a source connection (name, config, enabled).

**Request body:** Partial update — include only fields to change.

**Response 200:** Updated source connection object.

---

### `DELETE /v1/source-connections/{id}`

Soft-delete (disable) a source connection.

**Response 204:** No content.

---

## Source Tasks

### `GET /v1/source-tasks`

List source tasks with optional filtering.

**Query params:**
- `page`, `per_page`
- `connection_id` — filter by source connection
- `status` — `active`, `completed`, `deleted`

**Response 200:**
```json
[
  {
    "id": "uuid",
    "source_connection_id": "uuid",
    "external_id": "google-task-id",
    "title": "Finalize Q4 report",
    "description": "Send to manager by Friday",
    "status": "active",
    "priority": 1,
    "due_at": "2024-01-19T00:00:00Z",
    "content_hash": "abc123...",
    "created_at": "...",
    "updated_at": "..."
  }
]
```

**Example:**
```bash
curl -H "Admin-Token: change-me" \
  "http://localhost:8080/v1/source-tasks?status=active&per_page=50"
```

---

### `GET /v1/source-tasks/{id}`

Get a single source task by ID.

---

### `GET /v1/source-tasks/{id}/snapshots`

List snapshots for a source task, ordered by version ascending.

**Response 200:**
```json
[
  {
    "id": "uuid",
    "source_task_id": "uuid",
    "snapshot_version": 1,
    "content_hash": "abc123",
    "snapshot_taken_at": "2024-01-15T10:00:00Z"
  }
]
```

---

## Processing Jobs

### `GET /v1/jobs`

List processing jobs.

**Query params:**
- `page`, `per_page`
- `status` — filter by job status
- `source_task_id` — filter by task

**Response 200:**
```json
[
  {
    "id": "uuid",
    "source_task_id": "uuid",
    "job_type": "title_rewrite",
    "status": "queued",
    "priority": 0,
    "attempt_count": 0,
    "max_attempts": 3,
    "created_at": "...",
    "updated_at": "..."
  }
]
```

---

### `GET /v1/jobs/{id}`

Get a single processing job.

---

### `GET /v1/jobs/{id}/runs`

List all processing runs for a job.

**Response 200:**
```json
[
  {
    "id": "uuid",
    "job_id": "uuid",
    "attempt_number": 1,
    "outcome": "failure",
    "started_at": "...",
    "finished_at": "...",
    "duration_ms": 1234,
    "error_message": "LLM timeout"
  }
]
```

---

## Suggestion Proposals

### `GET /v1/proposals`

List suggestion proposals.

**Query params:**
- `page`, `per_page`
- `approval_status` — `pending`, `approved`, `rejected`
- `source_task_id`

**Response 200:**
```json
[
  {
    "id": "uuid",
    "source_task_id": "uuid",
    "job_id": "uuid",
    "proposal_type": "title_rewrite",
    "approval_status": "pending",
    "human_review_required": true,
    "proposal_payload": {
      "original_title": "do the thing",
      "proposed_title": "Complete Q4 budget review"
    },
    "auto_approved": false,
    "created_at": "..."
  }
]
```

---

### `GET /v1/proposals/{id}`

Get a single proposal.

---

### `POST /v1/proposals/{id}/approve`

Approve a pending proposal. Publishes `runeforge.proposal.approved`.

**Response 200:** Updated proposal object.

**Example:**
```bash
curl -X POST -H "Admin-Token: change-me" \
  http://localhost:8080/v1/proposals/uuid/approve
```

---

### `POST /v1/proposals/{id}/reject`

Reject a pending proposal.

**Request body (optional):**
```json
{"reason": "Title is already clear enough"}
```

**Response 200:** Updated proposal object.

---

## Writeback Operations

### `GET /v1/writeback-operations`

List writeback operations.

**Query params:** `page`, `per_page`, `status`

**Response 200:**
```json
[
  {
    "id": "uuid",
    "proposal_id": "uuid",
    "source_task_id": "uuid",
    "operation_type": "update_title",
    "status": "completed",
    "verified": true,
    "executed_at": "...",
    "completed_at": "..."
  }
]
```

---

## Audit Events

### `GET /v1/audit-events`

List audit events, newest first.

**Query params:**
- `page`, `per_page`
- `entity_type` — filter by entity type
- `entity_id` — filter by specific entity
- `event_type` — filter by event type
- `since` — ISO 8601 timestamp lower bound

**Response 200:**
```json
[
  {
    "id": "uuid",
    "entity_type": "processing_job",
    "entity_id": "uuid",
    "event_type": "job.created",
    "actor_type": "system",
    "actor_id": "orchestrator",
    "payload": {},
    "occurred_at": "2024-01-15T10:30:00Z"
  }
]
```

---

## Error Responses

All error responses follow this format:

```json
{
  "error": "human-readable message",
  "request_id": "uuid"
}
```

| Status | Meaning |
|--------|---------|
| 400 | Bad request (invalid input) |
| 401 | Missing or invalid Admin-Token |
| 404 | Resource not found |
| 405 | Method not allowed |
| 409 | Conflict (e.g., duplicate connection) |
| 500 | Internal server error |
