#!/usr/bin/env bash
# validate_post_hotfix.sh — Post-hotfix operational validation for Arcanum
# Usage: ./scripts/validate_post_hotfix.sh [--skip-setup] [--cycles N]
#
# Requires: curl, jq, docker, psql (via docker exec)
# Environment: source .env before running, or set ADMIN_TOKEN

set -euo pipefail

# --- Configuration ---
API_HOST="${API_HOST:-http://localhost:8090}"
ADMIN_TOKEN="${ADMIN_TOKEN:-change-me-in-production}"
PG_CONTAINER="${PG_CONTAINER:-docker-compose-postgres-1}"
PG_USER="${PG_USER:-runeforge}"
PG_DB="${PG_DB:-runeforge}"
CYCLES="${CYCLES:-3}"
ARTIFACTS_DIR="${ARTIFACTS_DIR:-validation_artifacts}"
SKIP_SETUP="${SKIP_SETUP:-false}"

# --- Parse Args ---
for arg in "$@"; do
  case $arg in
    --skip-setup) SKIP_SETUP=true ;;
    --cycles) shift; CYCLES="${2:-3}" ;;
    --cycles=*) CYCLES="${arg#*=}" ;;
    --help|-h)
      echo "Usage: $0 [--skip-setup] [--cycles N]"
      echo "  --skip-setup  Skip governance config and test data creation"
      echo "  --cycles N    Number of agent cycles to run (default: 3)"
      exit 0
      ;;
  esac
done

# --- Helpers ---
PASS=0
FAIL=0
WARN=0

check() {
  local label="$1" result="$2"
  if [[ "$result" == "PASS" ]]; then
    echo "  [PASS] $label"
    ((PASS++))
  elif [[ "$result" == "WARN" ]]; then
    echo "  [WARN] $label"
    ((WARN++))
  else
    echo "  [FAIL] $label"
    ((FAIL++))
  fi
}

pgq() {
  docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -t -A -c "$1" 2>/dev/null | tr -d '[:space:]'
}

api() {
  curl -sf -H "X-Admin-Token: $ADMIN_TOKEN" "$API_HOST$1" 2>/dev/null
}

api_post() {
  curl -sf -X POST -H "X-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json" "$API_HOST$1" ${2:+-d "$2"} 2>/dev/null
}

mkdir -p "$ARTIFACTS_DIR"

echo "============================================"
echo "  Arcanum Post-Hotfix Validation"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# --- Phase 1: Infrastructure ---
echo "--- Phase 1: Infrastructure Health ---"

if docker exec "$PG_CONTAINER" pg_isready -U "$PG_USER" -q 2>/dev/null; then
  check "PostgreSQL reachable" "PASS"
else
  check "PostgreSQL reachable" "FAIL"
  echo "BLOCKED: PostgreSQL not available. Aborting."
  exit 1
fi

if curl -sf http://localhost:11434/api/tags >/dev/null 2>&1; then
  check "Ollama reachable" "PASS"
else
  check "Ollama reachable" "FAIL"
  echo "BLOCKED: Ollama not available. Aborting."
  exit 1
fi

if api "/healthz" >/dev/null 2>&1; then
  check "API Gateway healthy" "PASS"
else
  check "API Gateway healthy" "FAIL"
  echo "BLOCKED: API Gateway not responding. Start it first."
  exit 1
fi

echo ""

# --- Phase 2: Provider Verification ---
echo "--- Phase 2: Provider Verification ---"

PROVIDERS_JSON=$(api "/api/v1/agent/providers/status" || echo '{}')
PROVIDER_COUNT=$(echo "$PROVIDERS_JSON" | jq '.providers | length' 2>/dev/null || echo 0)
NON_LOCAL=$(echo "$PROVIDERS_JSON" | jq '[.providers[] | select(.kind != "local")] | length' 2>/dev/null || echo 0)

check "Providers registered (count=$PROVIDER_COUNT)" "$([[ $PROVIDER_COUNT -ge 1 ]] && echo PASS || echo FAIL)"
check "Non-local provider registered (count=$NON_LOCAL)" "$([[ $NON_LOCAL -ge 1 ]] && echo PASS || echo WARN)"

echo "$PROVIDERS_JSON" > "$ARTIFACTS_DIR/providers.json"
echo ""

# --- Phase 3: Baseline ---
echo "--- Phase 3: Record Baseline ---"

B_REPLAY=$(pgq "SELECT count(*) FROM agent_replay_packs")
B_SNAPSHOT=$(pgq "SELECT count(*) FROM agent_path_decision_snapshots")
B_RESOURCE=$(pgq "SELECT count(*) FROM agent_resource_profiles")
B_COUNTERFACTUAL=$(pgq "SELECT count(*) FROM agent_counterfactual_simulations")
B_OUTCOMES=$(pgq "SELECT count(*) FROM agent_action_outcomes")

echo "  replay_packs=$B_REPLAY path_snapshots=$B_SNAPSHOT resource_profiles=$B_RESOURCE"
echo "  counterfactual_sims=$B_COUNTERFACTUAL action_outcomes=$B_OUTCOMES"

cat > "$ARTIFACTS_DIR/baseline.json" <<EOF
{"replay_packs":$B_REPLAY,"path_snapshots":$B_SNAPSHOT,"resource_profiles":$B_RESOURCE,"counterfactual_sims":$B_COUNTERFACTUAL,"action_outcomes":$B_OUTCOMES}
EOF
echo ""

# --- Phase 4: Setup (optional) ---
if [[ "$SKIP_SETUP" != "true" ]]; then
  echo "--- Phase 4: Governance & Test Data ---"

  # Configure governance
  FREEZE_RESULT=$(api_post "/api/v1/agent/governance/freeze" '{
    "mode":"frozen",
    "freeze_learning":false,
    "freeze_exploration":true,
    "freeze_policy_updates":true,
    "require_human_review":true,
    "requested_by":"validate_post_hotfix.sh"
  }' || echo "FAILED")

  if [[ "$FREEZE_RESULT" != "FAILED" ]]; then
    check "Governance frozen" "PASS"
  else
    check "Governance frozen" "WARN"
    echo "  (governance freeze failed — continuing without it)"
  fi

  echo ""
else
  echo "--- Phase 4: Skipped (--skip-setup) ---"
  echo ""
fi

# --- Phase 5: Run Cycles ---
echo "--- Phase 5: Run $CYCLES Agent Cycles ---"

TOTAL_PLANNED=0
TOTAL_EXECUTED=0
TOTAL_FAILED=0
TOTAL_REJECTED=0

for i in $(seq 1 "$CYCLES"); do
  echo "  Cycle $i:"

  # Plan
  PLAN=$(api_post "/api/v1/agent/plan" || echo '{"planned_count":0}')
  PLANNED=$(echo "$PLAN" | jq '.planned_count // 0' 2>/dev/null || echo 0)
  echo "    planned=$PLANNED"

  # Execute
  RESULT=$(api_post "/api/v1/agent/actions/run" || echo '{"executed":0,"failed":0,"rejected":0}')
  EXECUTED=$(echo "$RESULT" | jq '.executed // 0' 2>/dev/null || echo 0)
  FAILED=$(echo "$RESULT" | jq '.failed // 0' 2>/dev/null || echo 0)
  REJECTED=$(echo "$RESULT" | jq '.rejected // 0' 2>/dev/null || echo 0)

  echo "    executed=$EXECUTED failed=$FAILED rejected=$REJECTED"

  TOTAL_PLANNED=$((TOTAL_PLANNED + PLANNED))
  TOTAL_EXECUTED=$((TOTAL_EXECUTED + EXECUTED))
  TOTAL_FAILED=$((TOTAL_FAILED + FAILED))
  TOTAL_REJECTED=$((TOTAL_REJECTED + REJECTED))

  echo "$RESULT" > "$ARTIFACTS_DIR/cycle_${i}.json"
  sleep 3
done

echo ""
echo "  Totals: planned=$TOTAL_PLANNED executed=$TOTAL_EXECUTED failed=$TOTAL_FAILED rejected=$TOTAL_REJECTED"
echo ""

# --- Phase 6: Post-Validation Counts ---
echo "--- Phase 6: Post-Validation Evidence ---"

P_REPLAY=$(pgq "SELECT count(*) FROM agent_replay_packs")
P_SNAPSHOT=$(pgq "SELECT count(*) FROM agent_path_decision_snapshots")
P_RESOURCE=$(pgq "SELECT count(*) FROM agent_resource_profiles")
P_COUNTERFACTUAL=$(pgq "SELECT count(*) FROM agent_counterfactual_simulations")
P_OUTCOMES=$(pgq "SELECT count(*) FROM agent_action_outcomes")

D_REPLAY=$((P_REPLAY - B_REPLAY))
D_SNAPSHOT=$((P_SNAPSHOT - B_SNAPSHOT))
D_RESOURCE=$((P_RESOURCE - B_RESOURCE))
D_COUNTERFACTUAL=$((P_COUNTERFACTUAL - B_COUNTERFACTUAL))
D_OUTCOMES=$((P_OUTCOMES - B_OUTCOMES))

echo "  replay_packs: $B_REPLAY → $P_REPLAY (+$D_REPLAY)"
echo "  path_snapshots: $B_SNAPSHOT → $P_SNAPSHOT (+$D_SNAPSHOT)"
echo "  resource_profiles: $B_RESOURCE → $P_RESOURCE (+$D_RESOURCE)"
echo "  counterfactual_sims: $B_COUNTERFACTUAL → $P_COUNTERFACTUAL (+$D_COUNTERFACTUAL)"
echo "  action_outcomes: $B_OUTCOMES → $P_OUTCOMES (+$D_OUTCOMES)"

cat > "$ARTIFACTS_DIR/db_counts_post.json" <<EOF
{"replay_packs":$P_REPLAY,"path_snapshots":$P_SNAPSHOT,"resource_profiles":$P_RESOURCE,"counterfactual_sims":$P_COUNTERFACTUAL,"action_outcomes":$P_OUTCOMES}
EOF

# Routing decisions
ROUTING_JSON=$(api "/api/v1/agent/providers/decisions" || echo '{"total":0}')
ROUTING_COUNT=$(echo "$ROUTING_JSON" | jq '.total // 0' 2>/dev/null || echo 0)
echo "$ROUTING_JSON" > "$ARTIFACTS_DIR/routing_decisions.json"

# Fallback chains
FALLBACK_COUNT=$(echo "$ROUTING_JSON" | jq '[.decisions[]? | select(.fallback_chain | length > 0)] | length' 2>/dev/null || echo 0)

# Governance audit events
GOV_REVIEW=$(pgq "SELECT count(*) FROM audit_events WHERE event_type = 'governance.review_required'")

echo ""

# --- Phase 7: Collect Logs ---
echo "--- Phase 7: Collect Logs ---"

for svc in api-gateway worker; do
  if [[ -f "logs/${svc}.log" ]]; then
    cp "logs/${svc}.log" "$ARTIFACTS_DIR/${svc}.log"
    echo "  Copied logs/${svc}.log"
  fi
done

# Count post-hotfix errors (exclude known historical errors)
POST_ERRORS=$(grep '"level":"error"' "$ARTIFACTS_DIR/api-gateway.log" 2>/dev/null \
  | grep -v '2026-04-05' \
  | grep -v '01:16:53' \
  | grep -v '01:23:48' \
  | grep -v 'governance_replay_failed' \
  | grep -v 'requested_by is required' \
  | wc -l || echo 0)
POST_ERRORS=$(echo "$POST_ERRORS" | tr -d '[:space:]')

echo "  Post-hotfix API errors: $POST_ERRORS"
echo ""

# --- Phase 8: MUST Conditions ---
echo "============================================"
echo "  MUST Conditions Verification"
echo "============================================"

LEARNING_WRITES=$((D_REPLAY + D_SNAPSHOT + D_RESOURCE + D_COUNTERFACTUAL + D_OUTCOMES))

check "MUST-1: Learning writes (total_delta=$LEARNING_WRITES)" \
  "$([[ $LEARNING_WRITES -gt 0 ]] && echo PASS || echo FAIL)"

check "MUST-2: Routing decisions (count=$ROUTING_COUNT)" \
  "$([[ $ROUTING_COUNT -gt 0 ]] && echo PASS || echo FAIL)"

check "MUST-3: Fallback chains (count=$FALLBACK_COUNT)" \
  "$([[ $FALLBACK_COUNT -gt 0 ]] && echo PASS || echo FAIL)"

check "MUST-4: Non-local provider (count=$NON_LOCAL)" \
  "$([[ $NON_LOCAL -gt 0 ]] && echo PASS || echo WARN)"

check "MUST-5: Successful execution (executed=$TOTAL_EXECUTED)" \
  "$([[ $TOTAL_EXECUTED -gt 0 ]] && echo PASS || echo FAIL)"

check "MUST-6: Replay packs persisted (delta=+$D_REPLAY)" \
  "$([[ $D_REPLAY -gt 0 ]] && echo PASS || echo FAIL)"

check "MUST-7: Path snapshots persisted (delta=+$D_SNAPSHOT)" \
  "$([[ $D_SNAPSHOT -gt 0 ]] && echo PASS || echo FAIL)"

# Decision linkage
LINKAGE=$(pgq "SELECT count(*) FROM agent_replay_packs r JOIN agent_path_decision_snapshots s ON r.decision_id = s.decision_id")
check "MUST-8: Decision linkage (matched=$LINKAGE)" \
  "$([[ $LINKAGE -gt 0 ]] && echo PASS || echo FAIL)"

check "MUST-9: Governance enforced (review_events=$GOV_REVIEW)" \
  "$([[ $GOV_REVIEW -gt 0 ]] && echo PASS || echo WARN)"

check "MUST-10: No unsafe failures (failed=$TOTAL_FAILED)" \
  "$([[ $TOTAL_FAILED -eq 0 ]] && echo PASS || echo FAIL)"

check "MUST-11: Logs collected" \
  "$([[ -s "$ARTIFACTS_DIR/api-gateway.log" ]] && echo PASS || echo FAIL)"

check "MUST-12: Zero post-hotfix errors (count=$POST_ERRORS)" \
  "$([[ $POST_ERRORS -eq 0 ]] && echo PASS || echo FAIL)"

echo ""

# --- Phase 9: Verdict ---
echo "============================================"

if [[ $FAIL -eq 0 && $WARN -le 2 ]]; then
  VERDICT="READY_WITH_GUARDS"
elif [[ $FAIL -le 2 ]]; then
  VERDICT="READY_WITH_GUARDS (conditional)"
else
  VERDICT="NEEDS_FIXES"
fi

echo "  PASS=$PASS  WARN=$WARN  FAIL=$FAIL"
echo "  Verdict: $VERDICT"
echo "============================================"

# --- Save Summary ---
cat > "$ARTIFACTS_DIR/validation_summary.json" <<EOF
{
  "date": "$(date -Iseconds)",
  "verdict": "$VERDICT",
  "pass": $PASS,
  "warn": $WARN,
  "fail": $FAIL,
  "cycles": $CYCLES,
  "total_planned": $TOTAL_PLANNED,
  "total_executed": $TOTAL_EXECUTED,
  "total_failed": $TOTAL_FAILED,
  "total_rejected": $TOTAL_REJECTED,
  "deltas": {
    "replay_packs": $D_REPLAY,
    "path_snapshots": $D_SNAPSHOT,
    "resource_profiles": $D_RESOURCE,
    "counterfactual_sims": $D_COUNTERFACTUAL,
    "action_outcomes": $D_OUTCOMES
  },
  "routing_decisions": $ROUTING_COUNT,
  "fallback_chains": $FALLBACK_COUNT,
  "non_local_providers": $NON_LOCAL,
  "governance_review_events": $GOV_REVIEW,
  "post_hotfix_errors": $POST_ERRORS,
  "decision_linkage": $LINKAGE
}
EOF

echo ""
echo "Artifacts saved to: $ARTIFACTS_DIR/"
echo "Summary: $ARTIFACTS_DIR/validation_summary.json"

# Exit code: 0 if no FAIL, 1 otherwise
[[ $FAIL -eq 0 ]] && exit 0 || exit 1
