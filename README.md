# OpenClaw Exporter

[![CI](https://github.com/SammyLin/openclaw-exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/SammyLin/openclaw-exporter/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/SammyLin/openclaw-exporter)](https://goreportcard.com/report/github.com/SammyLin/openclaw-exporter)
[![Docker Pulls](https://img.shields.io/docker/pulls/sammylin/openclaw-exporter)](https://hub.docker.com/r/sammylin/openclaw-exporter)

Languages: English | [繁體中文](README.zh-TW.md)

Prometheus exporter for [OpenClaw](https://github.com/nicholasgriffintn/openclaw) AI agent metrics — agent status, cron job monitoring, token usage, and workspace analytics.

## Overview

`openclaw-exporter` reads OpenClaw's local data files (`~/.openclaw/`) and exposes them as Prometheus metrics on `:9101/metrics`.

## Prerequisites

- **Go 1.22+** (for building from source)
- A running **OpenClaw** instance with data in `~/.openclaw/`

## Getting Started

### Docker (from Docker Hub)

```bash
docker run --rm -p 9101:9101 -v ~/.openclaw:/home/exporter/.openclaw:ro sammylin/openclaw-exporter
```

### Docker (build locally)

```bash
docker build -t openclaw-exporter .
docker run --rm -p 9101:9101 -v ~/.openclaw:/home/exporter/.openclaw:ro openclaw-exporter
```

### From source

```bash
make build
./openclaw-exporter
```

### Pre-built binary

Download from [Releases](https://github.com/SammyLin/openclaw-exporter/releases) and run directly:

```bash
./openclaw-exporter
```

## Command-Line Flags

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--web.listen-address` | `EXPORTER_LISTEN_ADDRESS` | `:9101` | Address to listen on |
| `--web.telemetry-path` | `EXPORTER_TELEMETRY_PATH` | `/metrics` | Metrics endpoint path |
| `--openclaw.home` | `OPENCLAW_HOME` | `~/.openclaw` | OpenClaw data directory |
| `--log.level` | `EXPORTER_LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |

## Exported Metrics

### Agent Metrics

| Metric | Description | Labels |
|--------|-------------|--------|
| `openclaw_active_sessions` | Total session count | — |
| `openclaw_agent_sessions` | Sessions per agent | `agent_name` |
| `openclaw_agent_state` | State (0=idle, 1=working, 2=thinking, 3=error) | `agent_name` |
| `openclaw_agent_last_activity_seconds` | Seconds since last activity | `agent_name` |

### Cron Job Metrics

| Metric | Description | Labels |
|--------|-------------|--------|
| `openclaw_cron_jobs_total` | Total cron jobs | — |
| `openclaw_cron_jobs_enabled` | Enabled cron jobs | — |
| `openclaw_cron_job_enabled` | Job enabled state (1/0) | `job_name`, `job_id` |
| `openclaw_cron_job_last_run_age_seconds` | Seconds since last run | `job_name`, `job_id` |
| `openclaw_cron_job_next_run_in_seconds` | Seconds until next run | `job_name`, `job_id` |
| `openclaw_cron_job_consecutive_errors` | Consecutive error count | `job_name`, `job_id` |
| `openclaw_cron_job_last_duration_ms` | Last execution duration (ms) | `job_name`, `job_id` |

### Token Usage Metrics

| Metric | Description | Labels |
|--------|-------------|--------|
| `openclaw_cron_session_tokens_last` | Token usage in last cron session | `agent`, `cron_name`, `token_type` |
| `openclaw_cron_session_cost_last_usd` | Cost of last cron session | `agent`, `cron_name` |
| `openclaw_agent_session_avg_tokens` | Avg tokens per session (last 5) | `agent`, `token_type` |
| `openclaw_agent_session_avg_cost_usd` | Avg cost per session (last 5) | `agent` |
| `openclaw_agent_session_last_tokens` | Tokens in latest session | `agent`, `token_type` |
| `openclaw_agent_session_last_cost_usd` | Cost of latest session | `agent` |

### Workspace Metrics

| Metric | Description | Labels |
|--------|-------------|--------|
| `openclaw_md_file_bytes` | MD file size in bytes | `workspace`, `filename` |
| `openclaw_md_file_tokens_estimated` | Estimated token count | `workspace`, `filename` |
| `openclaw_md_workspace_total_bytes` | Total MD bytes in workspace | `workspace` |
| `openclaw_md_workspace_total_tokens_estimated` | Total estimated tokens | `workspace` |

## Grafana Dashboard

Pre-built Grafana dashboards are included in [`deploy/grafana/dashboards/`](deploy/grafana/dashboards/):

| Dashboard | Description |
|-----------|-------------|
| `openclaw-complete.json` | All-in-one dashboard — agent status, cron jobs, LLM cost, token usage, system resources |
| `token-usage.json` | Token economics deep-dive — daily trends, cost breakdown, per-agent analysis |
| `system-monitor.json` | System resource monitoring — CPU, memory, disk, network |

**To import:** In Grafana, go to **Dashboards > Import**, then upload the JSON file or paste its contents. The dashboards expect a Prometheus datasource.

## Monitoring Stack (Optional)

A full monitoring stack (Prometheus + Grafana + OTEL Collector + Node Exporter) is provided in the `deploy/` directory:

```bash
cd deploy
cp .env.example .env  # edit as needed
docker compose up -d
```

Or from the project root:

```bash
make stack-up
```

Access:
- Grafana: http://localhost:3000
- Prometheus: http://localhost:9090

Dashboards are auto-provisioned when using the stack. See `deploy/docker-compose.yml` for the full configuration.

## Building from Source

```bash
make build          # Build binary
make run            # Build and run exporter
make test           # Run tests
make lint           # Run golangci-lint
make docker-build   # Build Docker image
make docker-run     # Run in Docker

# Monitoring stack
make stack-up       # Start Prometheus + Grafana + OTEL stack
make stack-down     # Stop stack
make stack-status   # Check all service status
make stack-logs     # Tail stack logs
```

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## License

[MIT](LICENSE)
