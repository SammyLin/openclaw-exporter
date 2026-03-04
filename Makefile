.PHONY: up down restart status logs \
       exporter-restart exporter-log exporter-status \
       reload-dashboard metrics health setup

COMPOSE  := docker compose
PLIST    := com.openclaw.exporter
EXPORTER := http://localhost:9101
GRAFANA  := http://localhost:3000
PROM     := http://localhost:9090

# ── Docker services ──────────────────────────────────────────

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

restart:
	$(COMPOSE) restart

status:
	@echo "=== Docker services ==="
	@$(COMPOSE) ps
	@echo ""
	@echo "=== openclaw_exporter (launchd) ==="
	@launchctl list | grep $(PLIST) || echo "  not loaded"
	@echo ""
	@echo "=== Prometheus targets ==="
	@curl -s $(PROM)/api/v1/targets | python3 -c \
	  "import sys,json; [print(f\"  {t['labels']['job']}: {t['health']}\") for t in json.load(sys.stdin).get('data',{}).get('activeTargets',[])]" 2>/dev/null || echo "  prometheus unreachable"

logs:
	$(COMPOSE) logs -f --tail=50

# ── openclaw_exporter (launchd) ──────────────────────────────

exporter-restart:
	launchctl stop $(PLIST) && launchctl start $(PLIST)
	@sleep 1
	@curl -sf $(EXPORTER)/metrics > /dev/null && echo "exporter restarted OK" || echo "exporter failed to start"

exporter-log:
	tail -f exporter.log

exporter-status:
	@launchctl list | grep $(PLIST) || echo "not loaded"

# ── Grafana ──────────────────────────────────────────────────

reload-dashboard:
	curl -sf -X POST http://admin:admin123@localhost:3000/api/admin/provisioning/dashboards/reload && echo " OK" || echo " FAIL"

# ── Quick checks ─────────────────────────────────────────────

metrics:
	@curl -sf $(EXPORTER)/metrics | head -30
	@echo "..."
	@curl -sf $(EXPORTER)/metrics | grep -c "^openclaw_" | xargs -I{} echo "{} openclaw metrics exposed"

health:
	@bash scripts/health-check.sh

setup:
	@bash scripts/setup.sh
