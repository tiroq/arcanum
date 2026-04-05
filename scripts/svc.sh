#!/usr/bin/env bash
# svc.sh — Arcanum service manager
#
# Usage:
#   ./scripts/svc.sh list
#   ./scripts/svc.sh verify
#   ./scripts/svc.sh build   [service|all]
#   ./scripts/svc.sh start   [service|all]
#   ./scripts/svc.sh stop    [service|all]
#   ./scripts/svc.sh restart [service|all]
#   ./scripts/svc.sh status  [service|all]
#   ./scripts/svc.sh health  [service|all]
#   ./scripts/svc.sh logs    <service> [lines]
#   ./scripts/svc.sh tail    <service>
#   ./scripts/svc.sh ps
#   ./scripts/svc.sh infra   <start|stop|status|logs> [service]
#   ./scripts/svc.sh nats    <health|jsz|logs>

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOGDIR="$ROOT/logs"
PIDDIR="$ROOT/.arcanum/pids"
BINDIR="$ROOT/bin"
ENV_FILE="$ROOT/.env"
COMPOSE_FILE="$ROOT/deploy/docker-compose/docker-compose.yml"

mkdir -p "$LOGDIR" "$PIDDIR" "$BINDIR"

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

declare -A KNOWN_PORTS=(
  [api-gateway]=8080
  [source-sync]=8081
  [orchestrator]=8082
  [worker]=8083
  [writeback]=8084
  [notification]=8085
)

declare -A KNOWN_HEALTH=(
  [api-gateway]=/health
)

declare -A KNOWN_READY=(
  [api-gateway]=/ready
)

declare -a ALL_SERVICES=()
declare -a COMPOSE_SERVICES=()
declare -a INFRA_SERVICES=(postgres nats)
declare -A SVC_PORT=()
declare -A SVC_HEALTH=()
declare -A SVC_READY=()
declare -A SVC_PKG=()
declare -A SVC_DOCKERFILE=()

load_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    warn ".env not found at $ENV_FILE — using current environment"
    return
  fi

  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    [[ "$value" == *'|'* || "$value" == *'$('* ]] && continue
    export "$key=$value"
  done < <(grep -v '^#' "$ENV_FILE" | grep -v '^[[:space:]]*$')
}

discover_compose_services() {
  COMPOSE_SERVICES=()
  [[ -f "$COMPOSE_FILE" ]] || return

  while IFS= read -r svc; do
    COMPOSE_SERVICES+=("$svc")
  done < <(
    awk '
      /^services:/ { in_services=1; next }
      in_services && /^[^[:space:]]/ { in_services=0 }
      in_services && /^  [A-Za-z0-9_-]+:/ {
        name=$1
        sub(":", "", name)
        print name
      }
    ' "$COMPOSE_FILE"
  )
}

discover_services() {
  ALL_SERVICES=()
  SVC_PORT=()
  SVC_HEALTH=()
  SVC_READY=()
  SVC_PKG=()
  SVC_DOCKERFILE=()

  local -a discovered=()
  while IFS= read -r svc; do
    discovered+=("$svc")
  done < <(
    find "$ROOT/cmd" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort
  )

  if [[ ${#discovered[@]} -eq 0 ]]; then
    err "No services discovered under $ROOT/cmd"
    exit 1
  fi

  local next_port=8080
  local svc
  for svc in "${discovered[@]}"; do
    ALL_SERVICES+=("$svc")
    SVC_PKG["$svc"]="./cmd/$svc"
    SVC_DOCKERFILE["$svc"]="$ROOT/deploy/docker/Dockerfile.$svc"
    SVC_HEALTH["$svc"]="${KNOWN_HEALTH[$svc]:-/healthz}"
    SVC_READY["$svc"]="${KNOWN_READY[$svc]:-/readyz}"

    if [[ -n "${KNOWN_PORTS[$svc]:-}" ]]; then
      SVC_PORT["$svc"]="${KNOWN_PORTS[$svc]}"
      if (( KNOWN_PORTS[$svc] >= next_port )); then
        next_port=$((KNOWN_PORTS[$svc] + 1))
      fi
    else
      SVC_PORT["$svc"]="$next_port"
      next_port=$((next_port + 1))
    fi
  done
}

compose_has_service() {
  local target="$1"
  local svc
  for svc in "${COMPOSE_SERVICES[@]}"; do
    [[ "$svc" == "$target" ]] && return 0
  done
  return 1
}

pid_file() { echo "$PIDDIR/$1.pid"; }
save_pid() { echo "$2" > "$(pid_file "$1")"; }

read_pid() {
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

service_port_env_name() {
  local svc="$1"
  svc="${svc^^}"
  svc="${svc//-/_}"
  echo "SVC_PORT_${svc}"
}

service_port() {
  local svc="$1"
  local env_name
  env_name="$(service_port_env_name "$svc")"
  echo "${!env_name:-${SVC_PORT[$svc]}}"
}

service_health_url() {
  local svc="$1"
  echo "http://localhost:$(service_port "$svc")${SVC_HEALTH[$svc]}"
}

service_ready_url() {
  local svc="$1"
  echo "http://localhost:$(service_port "$svc")${SVC_READY[$svc]}"
}

binary_path() {
  echo "$BINDIR/$1"
}

build_service() {
  local svc="$1"
  local out
  out="$(binary_path "$svc")"
  info "Building $svc -> $out"
  (cd "$ROOT" && go build -trimpath -o "$out" "${SVC_PKG[$svc]}")
  ok "$svc built"
}

do_start() {
  local svc="$1"
  if is_running "$svc"; then
    warn "$svc already running (PID $(read_pid "$svc"))"
    return
  fi

  local port log bin pid
  port="$(service_port "$svc")"
  log="$LOGDIR/${svc}.log"
  bin="$(binary_path "$svc")"

  if [[ ! -x "$bin" ]]; then
    info "Binary for $svc not found; building first"
    build_service "$svc"
  fi

  info "Starting $svc on port $port"
  (
    cd "$ROOT"
    HTTP_PORT="$port" "$bin"
  ) >> "$log" 2>&1 &
  pid=$!
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

  local pid waited=0
  pid="$(read_pid "$svc")"
  info "Stopping $svc (PID $pid)"
  kill -TERM "$pid" 2>/dev/null || true

  while kill -0 "$pid" 2>/dev/null && (( waited < 10 )); do
    sleep 1
    waited=$((waited + 1))
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
  local pid state h_result r_result
  pid="$(read_pid "$svc")"

  if is_running "$svc"; then
    state="${GREEN}running${RESET}  PID=${pid}"
    h_result="$(curl -sf --max-time 2 "$(service_health_url "$svc")" 2>/dev/null || echo "unreachable")"
    r_result="$(curl -sf --max-time 2 "$(service_ready_url "$svc")" 2>/dev/null || echo "unreachable")"
  else
    state="${RED}stopped${RESET}"
    h_result="-"
    r_result="-"
    [[ -n "$pid" ]] && clear_pid "$svc"
  fi

  printf "  %-16s %b  port=%-5s  health=%-12s  ready=%s\n" \
    "$svc" "$state" "$(service_port "$svc")" "$h_result" "$r_result"
}

do_health_one() {
  local svc="$1"
  local h r h_col r_col
  h=$(curl -sf --max-time 3 "$(service_health_url "$svc")" >/dev/null && echo "ok" || echo "fail")
  r=$(curl -sf --max-time 3 "$(service_ready_url "$svc")" >/dev/null && echo "ok" || echo "fail")

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
  info "Tailing $log (Ctrl+C to stop)"
  tail -f "$log"
}

do_ps() {
  section "=== Arcanum processes ==="
  printf "  %-16s %-10s %s\n" "SERVICE" "PID" "STATE"
  printf "  %-16s %-10s %s\n" "-------" "---" "-----"

  local svc pid state
  for svc in "${ALL_SERVICES[@]}"; do
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

do_list() {
  section "=== Discovered services ==="
  printf "  %-16s %-8s %-10s %-8s %s\n" "SERVICE" "PORT" "COMPOSE" "DOCKER" "PACKAGE"
  printf "  %-16s %-8s %-10s %-8s %s\n" "-------" "----" "-------" "------" "-------"

  local svc compose_state docker_state
  for svc in "${ALL_SERVICES[@]}"; do
    compose_state="no"
    docker_state="no"
    compose_has_service "$svc" && compose_state="yes"
    [[ -f "${SVC_DOCKERFILE[$svc]}" ]] && docker_state="yes"

    printf "  %-16s %-8s %-10s %-8s %s\n" \
      "$svc" "$(service_port "$svc")" "$compose_state" "$docker_state" "${SVC_PKG[$svc]}"
  done
}

do_verify() {
  section "=== Verifying service coverage ==="
  local failures=0 svc

  for svc in "${ALL_SERVICES[@]}"; do
    if [[ ! -d "$ROOT/${SVC_PKG[$svc]#./}" ]]; then
      err "$svc missing package directory ${SVC_PKG[$svc]}"
      failures=$((failures + 1))
    fi

    if [[ ! -f "${SVC_DOCKERFILE[$svc]}" ]]; then
      err "$svc missing Dockerfile ${SVC_DOCKERFILE[$svc]}"
      failures=$((failures + 1))
    fi

    if ! compose_has_service "$svc"; then
      warn "$svc not present in compose services"
    fi
  done

  for svc in "${COMPOSE_SERVICES[@]}"; do
    if [[ "$svc" == "admin-web" || "$svc" == "postgres" || "$svc" == "nats" ]]; then
      continue
    fi
    if [[ -z "${SVC_PKG[$svc]:-}" ]]; then
      warn "$svc exists in compose but has no matching cmd/$svc entry"
    fi
  done

  if (( failures > 0 )); then
    err "Verification failed with $failures hard error(s)"
    exit 1
  fi

  ok "Verification passed for ${#ALL_SERVICES[@]} discovered service(s)"
}

resolve_targets() {
  local target="${1:-all}"
  if [[ "$target" == "all" ]]; then
    echo "${ALL_SERVICES[*]}"
    return
  fi

  if [[ -n "${SVC_PKG[$target]:-}" ]]; then
    echo "$target"
    return
  fi

  err "Unknown service: $target"
  err "Valid names: ${ALL_SERVICES[*]}, all"
  exit 1
}

compose_cmd() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

do_infra() {
  local action="${1:-}"
  local target="${2:-}"

  [[ -f "$COMPOSE_FILE" ]] || {
    err "Compose file not found: $COMPOSE_FILE"
    exit 1
  }

  case "$action" in
    start|up)
      section "=== Starting infra ==="
      compose_cmd up -d "${INFRA_SERVICES[@]}"
      ;;
    stop|down)
      section "=== Stopping infra ==="
      compose_cmd stop "${INFRA_SERVICES[@]}"
      ;;
    status)
      section "=== Infra status ==="
      compose_cmd ps "${INFRA_SERVICES[@]}"
      ;;
    logs)
      if [[ -n "$target" ]]; then
        compose_cmd logs -f --tail=100 "$target"
      else
        compose_cmd logs -f --tail=100 "${INFRA_SERVICES[@]}"
      fi
      ;;
    *)
      err "infra requires one of: start, stop, status, logs"
      exit 1
      ;;
  esac
}

do_nats() {
  local action="${1:-}"
  case "$action" in
    health)
      curl -fsS http://localhost:8222/healthz
      echo
      ;;
    jsz)
      curl -fsS http://localhost:8222/jsz
      echo
      ;;
    logs)
      do_infra logs nats
      ;;
    *)
      err "nats requires one of: health, jsz, logs"
      exit 1
      ;;
  esac
}

usage() {
  cat <<EOF
${BOLD}Arcanum service manager${RESET}

Usage: $(basename "$0") <command> [service] [options]

Commands:
  list                      Show discovered services and metadata
  verify                    Check discovered services against cmd/, Dockerfiles, and compose
  build   [service|all]     Build service binaries into ./bin
  start   [service|all]     Start service(s) locally from ./bin (auto-build on first run)
  stop    [service|all]     Stop service(s) gracefully (SIGKILL fallback)
  restart [service|all]     Stop then start service(s)
  status  [service|all]     Show running state + health/readiness
  health  [service|all]     Probe health and readiness endpoints only
  logs    <service> [lines] Print last N lines of service log (default: 50)
  tail    <service>         Follow service log in real time
  ps                        List tracked PIDs
  infra   <cmd> [service]   Manage docker infra: start, stop, status, logs
  nats    <cmd>             NATS helpers: health, jsz, logs

Services are discovered from ./cmd automatically.
Ports default to known service ports; new services get the next free port from 8080 upward.
Port overrides can be set per service via env vars like SVC_PORT_API_GATEWAY=9090.

Examples:
  $(basename "$0") list
  $(basename "$0") verify
  $(basename "$0") build all
  $(basename "$0") build worker
  $(basename "$0") start
  $(basename "$0") restart orchestrator
  $(basename "$0") status
  $(basename "$0") logs worker 100
  $(basename "$0") infra start
  $(basename "$0") nats jsz
EOF
}

discover_services
discover_compose_services

CMD="${1:-}"
if [[ -z "$CMD" ]]; then
  usage
  exit 0
fi
shift

case "$CMD" in
  list|ls)
    do_list
    ;;

  verify)
    do_verify
    ;;

  build)
    cd "$ROOT"
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Building services ==="
    for svc in "${targets[@]}"; do
      build_service "$svc"
    done
    ;;

  start)
    load_env
    cd "$ROOT"
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Starting services ==="
    for svc in "${targets[@]}"; do
      do_start "$svc"
    done
    info "Waiting 4 s for startup"
    sleep 4
    section "=== Status ==="
    for svc in "${targets[@]}"; do
      do_status_one "$svc"
    done
    ;;

  stop)
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Stopping services ==="
    for svc in "${targets[@]}"; do
      do_stop "$svc"
    done
    ;;

  restart)
    load_env
    cd "$ROOT"
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Restarting services ==="
    for svc in "${targets[@]}"; do
      do_restart "$svc"
    done
    info "Waiting 4 s for startup"
    sleep 4
    section "=== Status ==="
    for svc in "${targets[@]}"; do
      do_status_one "$svc"
    done
    ;;

  status)
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Service status ==="
    for svc in "${targets[@]}"; do
      do_status_one "$svc"
    done
    ;;

  health)
    IFS=' ' read -r -a targets <<< "$(resolve_targets "${1:-all}")"
    section "=== Health checks ==="
    for svc in "${targets[@]}"; do
      do_health_one "$svc"
    done
    ;;

  logs)
    SVC="${1:-}"
    LINES="${2:-50}"
    [[ -z "$SVC" ]] && { err "logs requires a service name"; usage; exit 1; }
    resolve_targets "$SVC" >/dev/null
    do_logs "$SVC" "$LINES"
    ;;

  tail)
    SVC="${1:-}"
    [[ -z "$SVC" ]] && { err "tail requires a service name"; usage; exit 1; }
    resolve_targets "$SVC" >/dev/null
    do_tail "$SVC"
    ;;

  ps)
    do_ps
    ;;

  infra)
    do_infra "${1:-}" "${2:-}"
    ;;

  nats)
    do_nats "${1:-}"
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
