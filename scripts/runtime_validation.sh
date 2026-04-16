#!/usr/bin/env bash
# Arcanum Runtime Seeding & Validation Script
# Seeds minimal data and validates the full closed-loop.
set -euo pipefail

API="${API_GATEWAY_URL:-http://localhost:8090}"
TOKEN="${ADMIN_TOKEN:-change-me-in-production}"

H=(-H "X-Admin-Token: $TOKEN" -H "Content-Type: application/json")
HC=(-H "X-Admin-Token: $TOKEN")

ok() { echo "  ✅ $1"; }
fail() { echo "  ❌ $1"; }
info() { echo "  ℹ️  $1"; }
step() { echo ""; echo "=== $1 ==="; }

step "1. Health & Readiness"
if curl -sf "$API/health" > /dev/null; then ok "Health OK"; else fail "Health FAILED"; exit 1; fi
if curl -sf "$API/ready" > /dev/null; then ok "Readiness OK"; else fail "Readiness FAILED"; exit 1; fi

step "2. Autonomy State"
STATE=$(curl -sf "${HC[@]}" "$API/api/v1/agent/autonomy/state")
MODE=$(echo "$STATE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('mode','unknown'))")
RUNNING=$(echo "$STATE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('running',False))")
info "Mode: $MODE, Running: $RUNNING"
if [ "$RUNNING" = "True" ]; then ok "Autonomy running"; else fail "Autonomy not running"; fi

step "3. System Vector"
VEC=$(curl -sf "${HC[@]}" "$API/api/v1/agent/vector")
info "Current vector: $(echo "$VEC" | python3 -c "import sys,json; v=json.load(sys.stdin); print(f'income={v[\"income_priority\"]}, risk={v[\"risk_tolerance\"]}, review={v[\"human_review_strictness\"]}')")"
ok "Vector accessible"

step "4. Goals"
GOALS=$(curl -sf "${HC[@]}" "$API/api/v1/agent/goals")
GOAL_COUNT=$(echo "$GOALS" | python3 -c "import sys,json; d=json.load(sys.stdin); g=d.get('goals',d) if isinstance(d,dict) else d; print(len(g) if g else 0)")
info "Goals count: $GOAL_COUNT"

step "5. Create Goal Plan + Decompose"
PLAN=$(curl -sf -X POST "${H[@]}" -d '{"goal_id":"monthly_income_growth","horizon":"monthly","strategy":"exploit_success_path"}' "$API/api/v1/agent/goals/plan" 2>/dev/null || echo "{}")
PLAN_ID=$(echo "$PLAN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('plan_id','none'))" 2>/dev/null || echo "none")
if [ "$PLAN_ID" != "none" ] && [ "$PLAN_ID" != "" ]; then
  ok "Plan created: $PLAN_ID"
else
  info "Plan creation: $(echo "$PLAN" | head -c 200)"
fi

SUBGOALS=$(curl -sf "${HC[@]}" "$API/api/v1/agent/goals/subgoals" 2>/dev/null || echo "[]")
SG_COUNT=$(echo "$SUBGOALS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")
info "Subgoals: $SG_COUNT"

step "6. Create & Prioritize Task"
TASK=$(curl -sf -X POST "${H[@]}" -d '{
  "source": "manual",
  "goal": "monthly_income_growth: find new consulting lead",
  "urgency": 0.8,
  "expected_value": 500.0,
  "risk_level": 0.2,
  "strategy_type": "exploit_success_path"
}' "$API/api/v1/agent/tasks" 2>/dev/null || echo "{}")
TASK_ID=$(echo "$TASK" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id','none'))" 2>/dev/null || echo "none")
if [ "$TASK_ID" != "none" ] && [ "$TASK_ID" != "" ]; then
  ok "Task created: $TASK_ID"
else
  info "Task creation: $(echo "$TASK" | head -c 200)"
fi

# Recompute priorities
RECOMP=$(curl -sf -X POST "${HC[@]}" "$API/api/v1/agent/tasks/recompute" 2>/dev/null || echo "{}")
info "Recompute: $(echo "$RECOMP" | head -c 200)"

# Show queue
QUEUE=$(curl -sf "${HC[@]}" "$API/api/v1/agent/tasks/queue" 2>/dev/null || echo "[]")
Q_COUNT=$(echo "$QUEUE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")
info "Queue size: $Q_COUNT"

step "7. Dispatch Task"
DISPATCH=$(curl -sf -X POST "${HC[@]}" "$API/api/v1/agent/tasks/dispatch" 2>/dev/null || echo "{}")
info "Dispatch: $(echo "$DISPATCH" | head -c 300)"

step "8. Create Execution Task"
EXEC_TASK=$(curl -sf -X POST "${H[@]}" -d '{"opportunity_id":"seed-opp-001","goal":"monthly_income_growth: find new consulting lead"}' "$API/api/v1/agent/execution/tasks" 2>/dev/null || echo "{}")
EXEC_ID=$(echo "$EXEC_TASK" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id','none'))" 2>/dev/null || echo "none")
if [ "$EXEC_ID" != "none" ] && [ "$EXEC_ID" != "" ]; then
  ok "Execution task created: $EXEC_ID"
  
  # Run bounded execution
  EXEC_RUN=$(curl -sf -X POST "${HC[@]}" "$API/api/v1/agent/execution/run/$EXEC_ID" 2>/dev/null || echo "{}")
  info "Execution run: $(echo "$EXEC_RUN" | head -c 300)"
else
  info "Execution task: $(echo "$EXEC_TASK" | head -c 200)"
fi

step "9. Trigger Actuation"
ACT_RUN=$(curl -sf -X POST "${HC[@]}" "$API/api/v1/agent/actuation/run" 2>/dev/null || echo "{}")
info "Actuation: $(echo "$ACT_RUN" | head -c 300)"

DECISIONS=$(curl -sf "${HC[@]}" "$API/api/v1/agent/actuation/decisions" 2>/dev/null || echo "[]")
DEC_COUNT=$(echo "$DECISIONS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")
info "Actuation decisions: $DEC_COUNT"

step "10. Objective Function"
OBJ=$(curl -sf "${HC[@]}" "$API/api/v1/agent/objective/summary" 2>/dev/null || echo "{}")
NET_UTIL=$(echo "$OBJ" | python3 -c "import sys,json; print(json.load(sys.stdin).get('net_utility', 'N/A'))" 2>/dev/null || echo "N/A")
info "Net utility: $NET_UTIL"

step "11. Trigger Replanning"
REPLAN=$(curl -sf -X POST "${H[@]}" -d '{"goal_id":"monthly_income_growth"}' "$API/api/v1/agent/goals/replan" 2>/dev/null || echo "{}")
info "Replan: $(echo "$REPLAN" | head -c 200)"

step "12. System Vector Change (behaviour adaptation)"
VEC2=$(curl -sf -X POST "${H[@]}" -d '{"income_priority":0.95,"family_safety_priority":1.0,"infra_priority":0.2,"automation_priority":0.6,"exploration_level":0.5,"risk_tolerance":0.5,"human_review_strictness":0.6}' "$API/api/v1/agent/vector/set" 2>/dev/null || echo "{}")
info "Vector updated: $(echo "$VEC2" | head -c 300)"

step "13. Autonomy Reports"
REPORTS=$(curl -sf "${HC[@]}" "$API/api/v1/agent/autonomy/reports" 2>/dev/null || echo "[]")
REP_COUNT=$(echo "$REPORTS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo "0")
info "Reports available: $REP_COUNT"

step "14. Final State"
FINAL=$(curl -sf "${HC[@]}" "$API/api/v1/agent/autonomy/state")
echo "$FINAL" | python3 -c "
import sys, json
s = json.load(sys.stdin)
print(f\"  Mode: {s['mode']}\")
print(f\"  Running: {s['running']}\")  
print(f\"  Cycles: {s['cycles_run']}\")
print(f\"  Tasks created from actuation: {s['tasks_created_from_actuation']}\")
print(f\"  Execution completed: {s['execution_completed']}\")
print(f\"  Execution failed: {s['execution_failed']}\")
print(f\"  Feedback recorded: {s['feedback_recorded']}\")
print(f\"  Downgraded: {s['downgraded']}\")
" 2>/dev/null || info "Could not parse final state"

echo ""
echo "=== VALIDATION COMPLETE ==="
