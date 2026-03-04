#!/usr/bin/env bash
# Health check for OpenClaw monitoring stack
set -uo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

PASS=0
FAIL=0

check() {
    local name="$1"
    shift
    if "$@" > /dev/null 2>&1; then
        printf "  ${GREEN}OK${NC}   %s\n" "$name"
        PASS=$((PASS + 1))
    else
        printf "  ${RED}FAIL${NC} %s\n" "$name"
        FAIL=$((FAIL + 1))
    fi
}

check_docker() {
    local svc="$1"
    docker compose ps --format '{{.Service}} {{.State}}' 2>/dev/null | grep -q "^${svc} running"
}

echo "=== OpenClaw Monitoring Health Check ==="
echo ""

# 1. Docker services
echo "Docker services:"
for svc in node-exporter otel-collector prometheus grafana; do
    check "$svc" check_docker "$svc"
done
echo ""

# 2. openclaw_exporter (launchd)
echo "openclaw_exporter:"
check "launchd loaded" bash -c "launchctl list 2>/dev/null | grep -q com.openclaw.exporter"
check "port 9101 responding" curl -sf --max-time 3 http://localhost:9101/metrics
check "openclaw metrics present" bash -c "curl -sf --max-time 3 http://localhost:9101/metrics | grep -q '^openclaw_'"
echo ""

# 3. Prometheus
echo "Prometheus:"
check "API reachable" curl -sf --max-time 3 http://localhost:9090/api/v1/status/config

TARGETS_JSON=$(curl -sf --max-time 3 http://localhost:9090/api/v1/targets 2>/dev/null || echo '{}')
if [ "$TARGETS_JSON" != '{}' ]; then
    for job in prometheus node-exporter openclaw-otel openclaw-exporter; do
        check "target: $job" python3 -c "
import json, sys
data = json.loads('''$TARGETS_JSON''')
targets = data.get('data',{}).get('activeTargets',[])
ok = any(t['labels']['job'] == '$job' and t['health'] == 'up' for t in targets)
sys.exit(0 if ok else 1)
"
    done
fi
echo ""

# 4. Grafana
echo "Grafana:"
check "UI reachable" curl -sf --max-time 3 http://localhost:3000/api/health
check "datasource configured" bash -c "curl -sf --max-time 3 http://${GF_ADMIN_USER:-admin}:${GF_ADMIN_PASSWORD:-changeme}@localhost:3000/api/datasources | python3 -c 'import sys,json; sys.exit(0 if json.load(sys.stdin) else 1)'"
check "dashboards loaded" bash -c "curl -sf --max-time 3 http://${GF_ADMIN_USER:-admin}:${GF_ADMIN_PASSWORD:-changeme}@localhost:3000/api/search | python3 -c 'import sys,json; sys.exit(0 if json.load(sys.stdin) else 1)'"
echo ""

# Summary
TOTAL=$((PASS + FAIL))
echo "=== Result: ${PASS}/${TOTAL} passed ==="
if [ "$FAIL" -gt 0 ]; then
    printf "${RED}%d check(s) failed${NC}\n" "$FAIL"
    exit 1
else
    printf "${GREEN}All checks passed${NC}\n"
    exit 0
fi
