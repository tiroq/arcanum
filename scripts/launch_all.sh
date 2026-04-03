#!/usr/bin/env bash
# Launch all implemented Arcanum services for observation
# Each service gets its own port and log file
set -euo pipefail

cd "$(dirname "$0")/.."
LOGDIR="$PWD/logs"
mkdir -p "$LOGDIR"

# Load .env (skip lines with pipes/subshells that break sourcing)
set -a
source <(grep -v '^#' .env | grep -v '^\s*$' | grep -v '|' | grep -v '\$(' )
set +a

echo "Starting all Arcanum services..."

# API Gateway
HTTP_PORT=8090 go run ./cmd/api-gateway/ > "$LOGDIR/api-gateway.log" 2>&1 &
echo "api-gateway PID=$! port=8090"

# Orchestrator
HTTP_PORT=8091 go run ./cmd/orchestrator/ > "$LOGDIR/orchestrator.log" 2>&1 &
echo "orchestrator PID=$! port=8091"

# Worker
HTTP_PORT=8092 go run ./cmd/worker/ > "$LOGDIR/worker.log" 2>&1 &
echo "worker PID=$! port=8092"

# Notification (Telegram)
HTTP_PORT=8093 go run ./cmd/notification/ > "$LOGDIR/notification.log" 2>&1 &
echo "notification PID=$! port=8093"

echo ""
echo "All services started. Logs in $LOGDIR/"
echo "Waiting 5s for startup..."
sleep 5

echo ""
echo "=== Health Checks ==="
for port in 8090 8091 8092 8093; do
    name=""
    case $port in
        8090) name="api-gateway" ;;
        8091) name="orchestrator" ;;
        8092) name="worker" ;;
        8093) name="notification" ;;
    esac
    result=$(curl -sf "http://localhost:$port/health" 2>&1 || echo "FAILED")
    echo "$name ($port): $result"
done

echo ""
echo "=== Readiness Checks ==="
for port in 8090 8091 8092 8093; do
    name=""
    case $port in
        8090) name="api-gateway" ;;
        8091) name="orchestrator" ;;
        8092) name="worker" ;;
        8093) name="notification" ;;
    esac
    result=$(curl -sf "http://localhost:$port/ready" 2>&1 || echo "FAILED")
    echo "$name ($port): $result"
done

echo ""
echo "Services running. Press Ctrl+C to stop all."
wait
