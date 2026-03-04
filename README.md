# OpenClaw Monitoring Stack

> Production-ready monitoring for [OpenClaw](https://github.com/nicholasgriffintn/openclaw) AI agents — LLM cost tracking, agent status, cron job monitoring, and system resources.

## Overview

```
OpenClaw Gateway
    │ OTLP (port 4318)
    ▼
OTEL Collector ──► Prometheus (9090) ──► Grafana (3000)
       │                  ▲
       │    Node Exporter ┘  (9100, container)
       │                  ▲
       │  openclaw_exporter ┘  (9101, host/launchd)
       │
       └─ batch processor (10s / 1000 metrics)
```

## Features

- **LLM Cost Tracking** — Real-time USD cost per model, token usage breakdowns (input/output/cache)
- **Agent Monitoring** — Session counts, working/thinking state, last activity per agent
- **Cron Job Dashboard** — Enabled rate, execution timing, consecutive errors, next-run countdown
- **Channel Analytics** — Message rate and token consumption by channel (Telegram, heartbeat, cron)
- **Performance Metrics** — Queue depth, wait time P95, tool call rate, message processing latency
- **System Resources** — CPU, memory, disk (virtiofs on macOS), and network I/O
- **Token Budget Analysis** — Workspace context size estimation, per-file token counts, cron job token costs

## Quick Start

```bash
git clone <repo-url> monitoring
cd monitoring
make setup
```

`make setup` runs a 5-step installer: `.env` creation, Python venv via `uv`, launchd service (macOS), Docker Compose, and a health check.

## Prerequisites

- **Docker** + **Docker Compose**
- **Python 3.11+** with [uv](https://docs.astral.sh/uv/)
- **macOS** (for launchd auto-start) or Linux (run exporter manually)
- An **OpenClaw** instance sending OTLP telemetry to port 4318

## What Gets Monitored

| Source | Protocol | Collection | Example Metrics |
|--------|----------|-----------|-----------------|
| OpenClaw Gateway | OTLP HTTP/gRPC | Push → OTEL Collector → Prometheus | `openclaw_cost_usd_total`, `openclaw_tokens_total` |
| openclaw_exporter | Prometheus scrape | Pull (Prometheus → host:9101) | `openclaw_agent_state`, `openclaw_cron_job_*` |
| Node Exporter | Prometheus scrape | Pull (Prometheus → container:9100) | `node_cpu_seconds_total`, `node_memory_*` |

### Agent Metrics

| Metric | Description | Labels |
|--------|-------------|--------|
| `openclaw_active_sessions` | Total session count | — |
| `openclaw_agent_sessions` | Sessions per agent | agent_name |
| `openclaw_agent_state` | State (0=idle, 1=working, 2=thinking, 3=error) | agent_name |
| `openclaw_agent_last_activity_seconds` | Seconds since last activity | agent_name |

### Cron Job Metrics

| Metric | Description | Labels |
|--------|-------------|--------|
| `openclaw_cron_jobs_total` | Total cron jobs | — |
| `openclaw_cron_jobs_enabled` | Enabled cron jobs | — |
| `openclaw_cron_job_enabled` | Job enabled state (1/0) | job_name, job_id |
| `openclaw_cron_job_last_run_age_seconds` | Seconds since last run | job_name, job_id |
| `openclaw_cron_job_next_run_in_seconds` | Seconds until next run | job_name, job_id |
| `openclaw_cron_job_consecutive_errors` | Consecutive error count | job_name, job_id |
| `openclaw_cron_job_last_duration_ms` | Last execution duration (ms) | job_name, job_id |

### OTEL Metrics (from OpenClaw Gateway)

| Metric | Description |
|--------|-------------|
| `openclaw_cost_usd_total` | Cumulative cost (USD) |
| `openclaw_tokens_total` | Cumulative token usage |
| `openclaw_message_processed_total` | Messages processed |
| `openclaw_session_state_total` | Session state transitions |
| `openclaw_run_duration_ms_milliseconds_*` | Agent run duration histogram |
| `openclaw_message_duration_ms_milliseconds_*` | Message processing time histogram |
| `openclaw_queue_depth_*` | Queue depth |
| `openclaw_tool_calls_total` | Cumulative tool calls |

## Dashboards

### OpenClaw Complete Monitor

`http://localhost:3000/d/openclaw-complete`

All-in-one dashboard with 8 sections: Business Overview, LLM Usage, Agent Status, Session Operations, Cron Jobs, Channel Activity, Performance, and System Resources. Includes an agent team reference panel showing each agent's role and responsibilities.

### Token Usage & Cost

`http://localhost:3000/d/token-usage`

Deep-dive into token economics: daily token trends by model, cost breakdowns (by model and channel), workspace context size estimation, cron job token consumption, and per-agent session cost analysis.

## Configuration

Copy `.env.example` to `.env` and edit as needed:

| Variable | Description | Default |
|----------|-------------|---------|
| `GF_ADMIN_USER` | Grafana admin username | `admin` |
| `GF_ADMIN_PASSWORD` | Grafana admin password | `changeme` |
| `EXPORTER_PORT` | openclaw_exporter listen port | `9101` |
| `OPENCLAW_HOME` | Path to `~/.openclaw` directory | `~/.openclaw` |

## Maintenance

| Command | Description |
|---------|-------------|
| `make up` | Start all Docker services |
| `make down` | Stop all Docker services |
| `make restart` | Restart Docker services |
| `make status` | Show all service status + Prometheus targets |
| `make logs` | Tail Docker service logs |
| `make exporter-restart` | Restart openclaw_exporter (launchd) |
| `make exporter-log` | Tail exporter log |
| `make exporter-status` | Check exporter launchd status |
| `make reload-dashboard` | Reload Grafana dashboard provisioning |
| `make metrics` | View current exporter metrics |
| `make health` | Run full health check |
| `make setup` | First-time install (venv + launchd + Docker) |

## Architecture

### Component Overview

| Component | Deployment | Port | Purpose |
|-----------|-----------|------|---------|
| **Grafana** | Docker | 3000 | Dashboard visualization |
| **Prometheus** | Docker | 9090 | Metrics storage (30-day / 5GB retention) |
| **OTEL Collector** | Docker | 4317/4318/8889 | OTLP → Prometheus conversion |
| **Node Exporter** | Docker | 9100 | System CPU/Memory/Disk/Network |
| **openclaw_exporter** | Host (launchd) | 9101 | Agent state, cron job monitoring |

### Design Decisions

**Why openclaw_exporter runs on the host (not in a container):**
- Needs direct access to `~/.openclaw/` JSONL and JSON files
- Avoids volume mount permission and performance issues
- Managed by launchd — auto-starts on boot, auto-restarts on crash

**Why OTEL Collector is needed:**
- OpenClaw Gateway uses OTLP push protocol; Prometheus is pull-based
- OTEL Collector handles OTLP → Prometheus format conversion and batch processing
- Can be removed if OpenClaw adds native Prometheus export in the future

**Alerting:**
- Alert rules defined in `prometheus/rules.yml` (CPU/Memory/Disk threshold alerts)
- `alerting/` directory reserved for future Alertmanager configuration (Slack/Email)
- Prometheus alertmanager targets currently empty

### Disk Metrics Note

Node Exporter runs in an OrbStack container. Mac disk is mounted as virtiofs:
```promql
(1 - (node_filesystem_avail_bytes{device="mac",mountpoint="/Users"}
    / node_filesystem_size_bytes{device="mac",mountpoint="/Users"})) * 100
```

### Datasource UID

Fixed as `PBFA97CFB590B2093`, defined in `grafana/provisioning/datasources/datasources.yml`. All dashboard JSONs and `generate_dashboard.py` reference this value.

## File Structure

```
monitoring/
├── Makefile                           # Common operation shortcuts
├── README.md
├── docker-compose.yml                 # Docker service definitions
├── openclaw_exporter.py               # Custom Prometheus exporter (v3)
├── generate_dashboard.py              # Dashboard JSON generator
├── requirements.txt                   # Python dependencies
├── scripts/
│   ├── health-check.sh                # Health check
│   └── setup.sh                       # First-time setup
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── datasources.yml        # Prometheus datasource (fixed UID)
│   │   └── dashboards/
│   │       └── default.yaml           # Dashboard auto-load config
│   └── dashboards/
│       ├── openclaw-complete.json     # Main dashboard (40+ panels)
│       ├── openclaw-monitor.json      # OpenClaw overview
│       ├── system-monitor.json        # System resources
│       └── token-usage.json           # Token usage & cost
├── prometheus/
│   ├── prometheus.yml                 # Scrape config
│   └── rules.yml                      # Alert rules
├── otel-collector/
│   └── config.yaml                    # OTLP receiver → Prometheus exporter
└── alerting/                          # Reserved for Alertmanager
```

## Troubleshooting

```bash
# Check all service status
make status

# Exporter not responding
make exporter-log                      # View logs
launchctl list | grep openclaw         # Check launchd status
make exporter-restart                  # Restart

# Prometheus target down
curl http://localhost:9090/api/v1/targets  # View target status
docker compose logs prometheus             # Prometheus logs

# Dashboard not updating
make reload-dashboard                  # Manual reload
docker compose restart grafana         # Or restart Grafana

# Full health check
make health
```

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## License

MIT
