.PHONY: build run test lint docker-build docker-run \
       stack-up stack-down stack-restart stack-status stack-logs \
       metrics

# ── Exporter ────────────────────────────────────────────────

build:
	uv sync

run:
	uv run python -m openclaw_exporter

test:
	uv run pytest

lint:
	uv run ruff check .

docker-build:
	docker build -t openclaw-exporter .

docker-run:
	docker run --rm -p 9101:9101 -v ~/.openclaw:/home/exporter/.openclaw:ro openclaw-exporter

# ── Monitoring Stack ────────────────────────────────────────

COMPOSE  := docker compose -f deploy/docker-compose.yml
EXPORTER := http://localhost:9101

stack-up:
	$(COMPOSE) up -d

stack-down:
	$(COMPOSE) down

stack-restart:
	$(COMPOSE) restart

stack-status:
	@$(COMPOSE) ps

stack-logs:
	$(COMPOSE) logs -f --tail=50

# ── Quick checks ─────────────────────────────────────────────

metrics:
	@curl -sf $(EXPORTER)/metrics | head -30
	@echo "..."
	@curl -sf $(EXPORTER)/metrics | grep -c "^openclaw_" | xargs -I{} echo "{} openclaw metrics exposed"
