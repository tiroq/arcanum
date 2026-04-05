#!/usr/bin/env bash
# svc.sh — Arcanum service manager
#
# Usage:
#   ./scripts/svc.sh start   [service|all]
#   ./scripts/svc.sh stop    [service|all]
#   ./scripts/svc.sh restart [service|all]
#   ./scripts/svc.sh status  [service|all]
#   ./scripts/svc.sh logs    <service> [lines]
#   ./scripts/svc.sh tail    <service>
#   ./scripts/svc.sh health  [service|all]
#   ./scripts/svc.sh ps
#
# Services: api-gateway, orchestrator, worker, notification, source-sync, writeback
# Default target when omitted: all

set -euo pipefail

# ── Paths ────────────────────────────────────────────────────────────────────

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOGDIR="$ROOT/logs"
PIDDIR="$ROOT/.arcanum/pids"
ENV_FILE="$ROOT/.env"

mkdir -p "$LOGDIR" "$PIDDIR"

# ── Colour helpers ────────────────────────────────────────────────────────────

if [[ -t 1 ]]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
  CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; CYAN=''; BOLD=''; RESET=''
fi

info()    { echo -e "${CYAN}${BOLD}[svc]${RESET} $*"; }
ok()      { echo -e "${GREEN}✔${RESET}  $*"; }
warn()    { echo -e "${YELLOW}⚠${RESET}  $*"; }
err()     { echo -e "${RED}✘${RESET}  $*" >&2; }
section() { echo -e "\n${BOLD}$*${RESET}"; }

# ── Service definitions ───────────────────────────────────────────────────────
# Format: NAME PORT HEALTH_PATH READY_PATH PKG

declare -A SVC_PORT SVC_HEALTH SVC_READY SVC_PKG

SVC_PORT=(
  [api-gateway]=8090
  [orchestrator]=8091
  [worker]=8092
  [notification]=8093
  [source-sync]=8094
  [writeback]=8095
)

SVC_HEALTH=(
  [api-gateway]=/health
  [orchestrator]=/healthz
  [worker]=/healthz
  [notification]=/healthz
  [source-sync]=/healthz
  [writeback]=/healthz
)

SVC_READY=(
  [api-gateway]=/ready
  [orchestrator]=/readyz
  [worker]=/readyz
  [notification]=/readyz
  [source-sync]=/readyz
  [writeback]=/readyz
)

SVC_PKG=(
  [api-gateway]=./cmd/api-gateway
  [orchestrator]=./cmd/orchestrator
  [worker]=./cmd/worker
  [notification]=./cmd/notification
  [source-sync]=./cmd/source-sync
  [writeback]=./cmd/writeback
)

ALL_SERVICES=(api-gateway orchestrator worker notification source-sync writeback)

# ── .env loader ───────────────────────────────────────────────────────────────

load_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    warn ".env not found at $ENV_FILE — using current environment"
    return
  fi
  # Skip comments, blanks, and lines with shell-metacharacter expansions
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    [[ "$value" == *'|'* || "$value" == *'$('* ]] && continue
    export "$key=$value"
  done < <(grep -v '^#' "$ENV_FILE" | grep -v '^\s*$')
}

# ── PID tracking ──────────────────────────────────────────────────────────────

pid_file() { echo "$PIDDIR/$1.pid"; }

save_pid()  { echo "$2" > "$(pid_file "$1")"; }

read_pid()  {
  local f
  f="$(pid_file "$1")"
  [[ -f "$f" ]] && cat "$f" || echo ""
}

clear_pid() { rm -f "$(pid_file "$1")"; }

is_running() {
  local pid
  pid="$(read_pid "$1")"
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

# ── Core operations ───────────────────────────────────────────────────────────

do_start() {
  local svc="$1"
  if is_running "$svc"; then
    warn "$svc already running (PID $(read_pid "$svc"))"
    return
  fi
  local port="${SVC_PORT[$svc]}"
  local log="$LOGDIR/${svc}.log"
  info "Starting $svc on port $port …"
  HTTP_PORT="$port" go run "${SVC_PKG[$svc]}" >> "$log" 2>&1 &
  local pid=$!
  save_pid "$svc" "$pid"
  ok "$svc started  PID=$pid  log=$log"
}

do_stop() {
  local svc="$1"
  if ! is_running "$svc"; then
    warn "$svc is not running"
    clear_pid "$svc"
    return
  fi
  local pid
  pid="$(read_pid "$svc")"
  info "Stopping $svc (PID $pid) …"
  # Send SIGTERM and wait up to 10 s for graceful shutdown
  kill -TERM "$pid" 2>/dev/null || true
  local waited=0
  while kill -0 "$pid" 2>/dev/null && (( waited < 10 )); do
    sleep 1; (( waited++ ))
  done
  if kill -0 "$pid" 2>/dev/null; then
    warn "$svc did not stop gracefully; sending SIGKILL"
    kill -KILL "$pid" 2>/dev/null || true
  fi
  clear_pid "$svc"
  ok "$svc stopped"
}

do_restart() {
  do_stop "$1"
  sleep 1
  do_start "$1"
}

do_status_one() {
  local svc="$1"
  local pid port url_h url_r h_result r_result state
  pid="$(read_pid "$svc")"
  port="${SVC_PORT[$svc]}"
  url_h="http://localhost:${port}${SVC_HEALTH[$svc]}"
  url_r="http://localhost:${port}${SVC_READY[$svc]}"

  if is_running "$svc"; then
    state="${GREEN}running${RESET}  PID=${pid}"
    h_result=$(curl -sf --max-time 2 "$url_h" 2>/dev/null || echo "unreachable")
    r_result=$(curl -sf --max-time 2 "$url_r" 2>/dev/null || echo "unreachable")
  else
    state="${RED}stopped${RESET}"
    h_result="-"
    r_result="-"
    [[ -n "$pid" ]] && clear_pid "$svc"
  fi

  printf "  %-16s %b  port=%-5s  health=%-12s  ready=%s\n" \
    "$svc" "$state" "$port" "$h_result" "$r_result"
}

do_health_one() {
  local svc="$1"
  local port url_h url_r
  port="${SVC_PORT[$svc]}"
  url_h="http://localhost:${port}${SVC_HEALTH[$svc]}"
  url_r="http://localhost:${port}${SVC_READY[$svc]}"

  local h r
  h=$(curl -sf --max-time 3 "$url_h" 2>/dev/null && echo "ok" || echo "fail")
  r=$(curl -sf --max-time 3 "$url_r" 2>/dev/null && echo "ok" || echo "fail")

  local h_col r_col
  [[ "$h" == "ok" ]] && h_col="${GREEN}ok${RESET}" || h_col="${RED}fail${RESET}"
  [[ "$r" == "ok" ]] && r_col="${GREEN}ok${RESET}" || r_col="${RED}fail${RESET}"

  printf "  %-16s health=%b   ready=%b\n" "$svc" "$h_col" "$r_col"
}

do_logs() {
  local svc="$1" lines="${2:-50}"
  local log="$LOGDIR/${svc}.log"
  if [[ ! -f "$log" ]]; then
    err "No log file found: $log"
    exit 1
  fi
  section "=== Last $lines lines of $svc ==="
  tail -n "$lines" "$log"
}

do_tail() {
  local svc="$1"
  local log="$LOGDIR/${svc}.log"
  if [[ ! -f "$log" ]]; then
    err "No log file found: $log"
    exit 1
  fi
  info "Tailing $log  (Ctrl+C to stop)"
  tail -f "$log"
}

do_ps() {
  section "=== Arcanum processes ==="
  printf "  %-16s %-10s %s\n" "SERVICE" "PID" "STATE"
  printf "  %-16s %-10s %s\n" "-------" "---" "-----"
  for svc in "${ALL_SERVICES[@]}"; do
    local pid state
    pid="$(read_pid "$svc")"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      state="${GREEN}running${RESET}"
    else
      state="${RED}stopped${RESET}"
      pid="-"
    fi
    printf "  %-16s %-10s %b\n" "$svc" "$pid" "$state"
  done
}

# ── Resolve target list ───────────────────────────────────────────────────────

resolve_targets() {
  local target="${1:-all}"
  if [[ "$target" == "all" ]]; then
    echo "${ALL_SERVICES[@]}"
  elif [[ -n "${SVC_PORT[$target]+_}" ]]; then
    echo "$target"
  else
    err "Unknown service: $target"
    err "Valid names: ${ALL_SERVICES[*]}, all"
    exit 1
  fi
}

# ── Usage ─────────────────────────────────────────────────────────────────────

usage() {
  cat <<EOF
${BOLD}Arcanum service manager${RESET}

Usage: $(basename "$0") <command> [service] [options]

Commands:
  start   [service|all]       Start service(s)
  stop    [service|all]       Stop service(s) gracefully (SIGKILL fallback)
  restart [service|all]       Stop then start service(s)
  status  [service|all]       Show running state + health/readiness
  health  [service|all]       Probe health and readiness endpoints only
  logs    <service> [lines]   Print last N lines of service log (default: 50)
  tail    <service>           Follow service log in real time
  ps                          List all tracked PIDs

Services: ${ALL_SERVICES[*]}
          all  (default when omitted)

Examples:
  $(basename "$0") start
  $(basename "$0") start worker
  $(basename "$0") stop orchestrator
  $(basename "$0") restart all
  $(basename "$0") status
  $(basename "$0") logs worker 100
  $(basename "$0") tail api-gateway
  $(basename "$0") health
EOF
}

# ── Entry point ───────────────────────────────────────────────────────────────

CMD="${1:-}"
if [[ -z "$CMD" ]]; then usage; exit 0; fi
shift

case "$CMD" in
  start)
    load_env
    cd "$ROOT"
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Starting services ==="
    for svc in "${targets[@]}"; do do_start "$svc"; done
    info "Waiting 4 s for startup …"
    sleep 4
    section "=== Status ==="
    for svc in "${targets[@]}"; do do_status_one "$svc"; done
    ;;

  stop)
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Stopping services ==="
    for svc in "${targets[@]}"; do do_stop "$svc"; done
    ;;

  restart)
    load_env
    cd "$ROOT"
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Restarting services ==="
    for svc in "${targets[@]}"; do do_restart "$svc"; done
    info "Waiting 4 s for startup …"
    sleep 4
    section "=== Status ==="
    for svc in "${targets[@]}"; do do_status_one "$svc"; done
    ;;

  status)
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Service status ==="
    for svc in "${targets[@]}"; do do_status_one "$svc"; done
    ;;

  health)
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Health checks ==="
    for svc in "${targets[@]}"; do do_health_one "$svc"; done
    ;;

  logs)
    SVC="${1:-}"
    LINES="${2:-50}"
    [[ -z "$SVC" ]] && { err "logs requires a service name"; usage; exit 1; }
    resolve_targets "$SVC" > /dev/null  # validate
    do_logs "$SVC" "$LINES"
    ;;

  tail)
    SVC="${1:-}"
    [[ -z "$SVC" ]] && { err "tail requires a service name"; usage; exit 1; }
    resolve_targets "$SVC" > /dev/null  # validate
    do_tail "$SVC"
    ;;

  ps)
    do_ps
    ;;

  help|--help|-h)
    usage
    ;;

  *)
    err "Unknown command: $CMD"
    usage
    exit 1
    ;;
esac
