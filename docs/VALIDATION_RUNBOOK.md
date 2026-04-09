# Post-Hotfix Validation Runbook

**Purpose:** Step-by-step procedure to validate that the Arcanum autonomous agent system is operational after the R-1/A-1/A-2 hotfixes.

**Expected duration:** 15-30 minutes  
**Prerequisites:** PostgreSQL, NATS, Ollama running; Go toolchain available

---

## 1. Preconditions

### Infrastructure Health

```bash
# Verify PostgreSQL
docker exec docker-compose-postgres-1 pg_isready -U runeforge
# Expected: accepting connections

# Verify NATS
docker exec docker-compose-nats-1 nats-server --help >/dev/null 2>&1 && echo "OK"
# Expected: OK (binary exists)

# Verify Ollama
curl -s http://localhost:11434/api/tags | head -1
# Expected: JSON with models list
```

### Build Services

```bash
cd /path/to/arcanum
scripts/svc.sh build all
# Expected: all binaries built to bin/
```

### Environment Variables

Ensure `.env` contains:
- `ADMIN_TOKEN` (e.g., `change-me-in-production`)
- `DATABASE_URL` or individual PG vars
- `NATS_URL`

For external provider validation, set:
```bash
export OPENROUTER_ENABLED=true
# OR
export OLLAMA_CLOUD_ENABLED=true
```

---

## 2. Start Services

```bash
source .env

# Stop any running services
scripts/svc.sh stop all

# Start with external provider enabled
export OPENROUTER_ENABLED=true
scripts/svc.sh start api-gateway
scripts/svc.sh start worker

# Wait for startup
sleep 5
```

### Verify Health

```bash
# API Gateway
curl -s http://localhost:8090/healthz
# Expected: {"status":"ok"}

# Worker
curl -s http://localhost:8083/healthz
# Expected: {"status":"ok"}
```

### Verify Providers

```bash
curl -s -H "X-Admin-Token: $ADMIN_TOKEN" http://localhost:8090/api/v1/agent/providers/status | jq '.providers[].name'
# Expected: "ollama" and "openrouter" (or "ollama-cloud")
```

---

## 3. Configure Governance

```bash
curl -s -X POST http://localhost:8090/api/v1/agent/governance/freeze \
  -H "X-Admin-Token: $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "frozen",
    "freeze_learning": false,
    "freeze_exploration": true,
    "freeze_policy_updates": true,
    "require_human_review": true,
    "requested_by": "post-hotfix-validation"
  }'
# Expected: 200 OK
```

---

## 4. Record Baseline

```bash
PGCMD="docker exec docker-compose-postgres-1 psql -U runeforge -d runeforge -t -A -c"

echo "=== BASELINE ==="
echo "replay_packs: $($PGCMD "SELECT count(*) FROM agent_replay_packs")"
echo "path_snapshots: $($PGCMD "SELECT count(*) FROM agent_path_decision_snapshots")"
echo "resource_profiles: $($PGCMD "SELECT count(*) FROM agent_resource_profiles")"
echo "counterfactual_sims: $($PGCMD "SELECT count(*) FROM agent_counterfactual_simulations")"
echo "action_outcomes: $($PGCMD "SELECT count(*) FROM agent_action_outcomes")"
echo "action_memory: $($PGCMD "SELECT count(*) FROM agent_action_memory")"
echo "calibration_records: $($PGCMD "SELECT count(*) FROM agent_calibration_records")"
```

Save these values for comparison.

---

## 5. Create Test Conditions

Manipulate job statistics to trigger goal evaluation:

```bash
# Trigger increase_reliability (failure_rate > 20%)
$PGCMD "UPDATE processing_jobs SET status='failed' WHERE status='succeeded' AND dedupe_key IS NULL LIMIT 20"

# Trigger reduce_retry_rate (retry_scheduled > 10)
$PGCMD "UPDATE processing_jobs SET status='retry_scheduled' WHERE status='succeeded' AND dedupe_key IS NULL LIMIT 12"

# Trigger investigate_failed_jobs (dead_letter_rate > 10%)
$PGCMD "UPDATE processing_jobs SET status='dead_letter' WHERE status='succeeded' AND dedupe_key IS NULL LIMIT 10"
```

Verify goals trigger:
```bash
curl -s -H "X-Admin-Token: $ADMIN_TOKEN" http://localhost:8090/api/v1/agent/goals | jq '.goals[].type'
# Expected: at least increase_reliability, reduce_retry_rate, investigate_failed_jobs
```

---

## 6. Run Validation Cycles

Run 3 agent cycles:

```bash
for i in 1 2 3; do
  echo "=== CYCLE $i ==="
  
  # Plan
  PLAN=$(curl -s -X POST http://localhost:8090/api/v1/agent/plan \
    -H "X-Admin-Token: $ADMIN_TOKEN")
  echo "$PLAN" | jq '{planned: .planned_count, strategies: [.strategies[].type]}'
  
  # Execute
  RESULT=$(curl -s -X POST http://localhost:8090/api/v1/agent/actions/run \
    -H "X-Admin-Token: $ADMIN_TOKEN")
  echo "$RESULT" | jq '{executed: .executed, failed: .failed, rejected: .rejected}'
  
  # Save
  echo "$RESULT" > "validation_artifacts/cycle_${i}.json"
  
  sleep 3
done
```

---

## 7. MUST Verification Checklist

After running cycles, verify each condition:

### MUST 1: Learning Writes

```bash
PGCMD="docker exec docker-compose-postgres-1 psql -U runeforge -d runeforge -t -A -c"
echo "replay_packs: $($PGCMD "SELECT count(*) FROM agent_replay_packs")"
echo "path_snapshots: $($PGCMD "SELECT count(*) FROM agent_path_decision_snapshots")"
echo "resource_profiles: $($PGCMD "SELECT count(*) FROM agent_resource_profiles")"
echo "action_outcomes: $($PGCMD "SELECT count(*) FROM agent_action_outcomes")"
# PASS if any table has grown from baseline
```

### MUST 2: Routing Decision

```bash
curl -s -H "X-Admin-Token: $ADMIN_TOKEN" http://localhost:8090/api/v1/agent/providers/decisions | jq '.total'
# PASS if > 0
```

### MUST 3: Fallback Chain

```bash
curl -s -H "X-Admin-Token: $ADMIN_TOKEN" http://localhost:8090/api/v1/agent/providers/decisions | jq '.decisions[0].fallback_chain'
# PASS if array is non-empty
```

### MUST 4: Non-Local Provider

```bash
curl -s -H "X-Admin-Token: $ADMIN_TOKEN" http://localhost:8090/api/v1/agent/providers/status | jq '[.providers[] | select(.kind != "local")] | length'
# PASS if > 0
```

### MUST 5: Successful Execution

```bash
# Check cycle results
cat validation_artifacts/cycle_*.json | jq -s '[.[].executed] | add'
# PASS if > 0
```

### MUST 6: Replay Packs Persisted (A-1)

```bash
$PGCMD "SELECT count(*) FROM agent_replay_packs"
# PASS if > baseline
```

### MUST 7: Path Snapshots Persisted (A-2)

```bash
$PGCMD "SELECT count(*) FROM agent_path_decision_snapshots"
# PASS if > baseline
```

### MUST 8: Decision Linkage

```bash
$PGCMD "SELECT count(*) FROM agent_replay_packs r JOIN agent_path_decision_snapshots s ON r.decision_id = s.decision_id"
# PASS if > 0 (matching decision_ids exist)
```

### MUST 9: Governance Enforced

```bash
$PGCMD "SELECT count(*) FROM audit_events WHERE event_type = 'governance.review_required'"
# PASS if > 0
```

### MUST 10: No Unsafe Failures

```bash
cat validation_artifacts/cycle_*.json | jq -s '[.[].failed] | add'
# PASS if == 0
```

### MUST 11: Logs Collected

```bash
ls -la validation_artifacts/api-gateway.log validation_artifacts/worker.log
# PASS if both files exist and are non-empty
```

### MUST 12: Zero Post-Hotfix Errors

```bash
# Check for 401 auth errors (R-1 regression)
grep '"status 401"' logs/api-gateway.log | grep -v '01:23:48' | wc -l
# PASS if == 0

# Check for post-cycle execution errors
grep '"action_execute_failed"' logs/api-gateway.log | tail -5
# PASS if no entries from current session
```

---

## 8. Common Failure Modes

| Symptom | Cause | Fix |
|---------|-------|-----|
| 401 errors on `retry_job` | Auth header regression (R-1) | Check `executor.go:122` uses `X-Admin-Token` |
| replay_packs count unchanged | Early return regression (A-1) | Check `planner_adapter.go` has no early return before replay recording |
| path_snapshots count unchanged | Early return regression (A-2) | Same as A-1 |
| No routing decisions | Provider not registered | Verify `OPENROUTER_ENABLED=true` exported before starting services |
| Empty fallback chain | Only one provider registered | Need ≥2 providers for fallback |
| No goals triggered | Job statistics don't cross thresholds | Verify failure_rate>20%, retry_scheduled>10, dead_letter_rate>10% |
| `governance_freeze_failed` | Missing `requested_by` field | Include `requested_by` in freeze request body |
| `governance_freeze_failed` — table missing | Migrations not applied | Run `scripts/migrate.sh up` |
| `action_memory_update_failed` warnings | Cold-start, no prior memory | Expected — fail-open by design, self-heals |

---

## 9. Pass/Fail Decision Rules

### PASS → `READY_WITH_GUARDS`

All of:
- ≥10/12 MUST conditions pass
- Zero 401 auth errors during validation cycles
- replay_packs and path_snapshots both grew from baseline
- ≥1 routing decision with non-empty fallback chain
- 0 unsafe failures (rejected actions are OK)

### CONDITIONAL PASS → `READY_WITH_GUARDS` (with caveats)

- 8-9/12 MUST conditions pass
- Failures are in non-critical areas (e.g., external provider never primary, zero-delta learning tables)
- Core pipeline (execute → record → persist) works

### FAIL → `NEEDS_FIXES`

Any of:
- 401 auth errors during validation cycles (R-1 regression)
- replay_packs or path_snapshots did not grow (A-1/A-2 regression)
- Actions planned but all fail to execute
- Zero routing decisions despite multiple providers registered
- Governance not enforced (no `review_required` events)

### BLOCKED → `BLOCKED`

Any of:
- Services fail to start
- PostgreSQL or NATS unavailable
- Ollama unreachable (no LLM inference possible)
- Build fails
- Migrations not applied (missing tables)
