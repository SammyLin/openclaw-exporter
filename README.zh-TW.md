# OpenClaw Exporter

[![Tests](https://github.com/SammyLin/openclaw-exporter/actions/workflows/tests.yml/badge.svg)](https://github.com/SammyLin/openclaw-exporter/actions/workflows/tests.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/SammyLin/openclaw-exporter)](https://goreportcard.com/report/github.com/SammyLin/openclaw-exporter)
[![Docker Pulls](https://img.shields.io/docker/pulls/sammylin/openclaw-exporter)](https://hub.docker.com/r/sammylin/openclaw-exporter)

[OpenClaw](https://github.com/nicholasgriffintn/openclaw) AI 代理的 Prometheus exporter — 代理狀態、排程任務監控、token 用量與工作區分析。

## 概述

`openclaw-exporter` 讀取 OpenClaw 的本地資料檔案（`~/.openclaw/`），並透過 `:9101/metrics` 以 Prometheus 格式匯出指標。

## 前置需求

- **Go 1.22+**（從原始碼建置時需要）
- 一個正在運行的 **OpenClaw** 實例，資料位於 `~/.openclaw/`

## 快速開始

### Docker（從 Docker Hub）

```bash
docker run --rm -p 9101:9101 -v ~/.openclaw:/home/exporter/.openclaw:ro sammylin/openclaw-exporter
```

### Docker（本地建置）

```bash
docker build -t openclaw-exporter .
docker run --rm -p 9101:9101 -v ~/.openclaw:/home/exporter/.openclaw:ro openclaw-exporter
```

### 從原始碼

```bash
make build
./openclaw-exporter
```

### 預編譯二進位檔

從 [Releases](https://github.com/SammyLin/openclaw-exporter/releases) 下載後直接執行：

```bash
./openclaw-exporter
```

## 命令列參數

| 參數 | 環境變數 | 預設值 | 說明 |
|------|---------|--------|------|
| `--web.listen-address` | `EXPORTER_LISTEN_ADDRESS` | `:9101` | 監聽地址 |
| `--web.telemetry-path` | `EXPORTER_TELEMETRY_PATH` | `/metrics` | 指標端點路徑 |
| `--openclaw.home` | `OPENCLAW_HOME` | `~/.openclaw` | OpenClaw 資料目錄 |
| `--log.level` | `EXPORTER_LOG_LEVEL` | `info` | 日誌等級（debug/info/warn/error） |

## 匯出指標

### 代理指標

| 指標 | 說明 | 標籤 |
|------|------|------|
| `openclaw_active_sessions` | 總工作階段數 | — |
| `openclaw_agent_sessions` | 各代理的工作階段數 | `agent_name` |
| `openclaw_agent_state` | 狀態（0=閒置, 1=工作中, 2=思考中, 3=錯誤） | `agent_name` |
| `openclaw_agent_last_activity_timestamp_seconds` | 上次活動的 Unix 時間戳 | `agent_name` |

### 排程任務指標

| 指標 | 說明 | 標籤 |
|------|------|------|
| `openclaw_cron_jobs_total` | 排程任務總數 | — |
| `openclaw_cron_jobs_enabled` | 啟用的排程任務數 | — |
| `openclaw_cron_job_enabled` | 任務啟用狀態（1/0） | `job_name`, `job_id` |
| `openclaw_cron_job_last_run_at_seconds` | 上次執行的 Unix 時間戳 | `job_name`, `job_id` |
| `openclaw_cron_job_next_run_at_seconds` | 下次執行的 Unix 時間戳 | `job_name`, `job_id` |
| `openclaw_cron_job_consecutive_errors` | 連續錯誤次數 | `job_name`, `job_id` |
| `openclaw_cron_job_last_duration_seconds` | 上次執行時間（秒） | `job_name`, `job_id` |

### Token 用量指標

| 指標 | 說明 | 標籤 |
|------|------|------|
| `openclaw_cron_session_tokens_last` | 上次排程工作階段 token 用量 | `agent`, `cron_name`, `token_type` |
| `openclaw_cron_session_cost_last_usd` | 上次排程工作階段費用 | `agent`, `cron_name` |
| `openclaw_agent_session_avg_tokens` | 平均每次工作階段 token 數（近 5 次） | `agent`, `token_type` |
| `openclaw_agent_session_avg_cost_usd` | 平均每次工作階段費用（近 5 次） | `agent` |
| `openclaw_agent_session_last_tokens` | 最近一次工作階段 token 數 | `agent`, `token_type` |
| `openclaw_agent_session_last_cost_usd` | 最近一次工作階段費用 | `agent` |

### 工作區指標

| 指標 | 說明 | 標籤 |
|------|------|------|
| `openclaw_md_file_bytes` | MD 檔案大小（位元組） | `workspace`, `filename` |
| `openclaw_md_file_tokens_estimated` | 估計 token 數 | `workspace`, `filename` |
| `openclaw_md_workspace_bytes` | 工作區 MD 總位元組 | `workspace` |
| `openclaw_md_workspace_tokens_estimated` | 工作區估計總 token 數 | `workspace` |

## Grafana Dashboard

預建的 Grafana dashboard 位於 [`deploy/grafana/dashboards/`](deploy/grafana/dashboards/)：

| Dashboard | 說明 |
|-----------|------|
| `openclaw-complete.json` | 全方位 dashboard — 代理狀態、排程任務、LLM 費用、token 用量、系統資源 |
| `token-usage.json` | Token 經濟分析 — 每日趨勢、費用拆解、各代理分析 |
| `system-monitor.json` | 系統資源監控 — CPU、記憶體、磁碟、網路 |

**匯入方式：** 在 Grafana 中，前往 **Dashboards > Import**，上傳 JSON 檔案或貼上內容。Dashboard 需要 Prometheus 資料源。

## 監控堆疊（選用）

完整的監控堆疊（Prometheus + Grafana + OTEL Collector + Node Exporter）位於 `deploy/` 目錄：

```bash
cd deploy
cp .env.example .env  # 依需求修改
docker compose up -d
```

或從專案根目錄：

```bash
make stack-up
```

存取：
- Grafana: http://localhost:3000
- Prometheus: http://localhost:9090

使用堆疊時，dashboard 會自動載入。完整設定請參閱 `deploy/docker-compose.yml`。

## 從原始碼建置

```bash
make build          # 編譯二進位檔
make run            # 編譯並執行 exporter
make test           # 執行測試
make lint           # 執行 golangci-lint
make docker-build   # 建置 Docker 映像
make docker-run     # 以 Docker 執行

# 監控堆疊
make stack-up       # 啟動 Prometheus + Grafana + OTEL 堆疊
make stack-down     # 停止堆疊
make stack-status   # 檢查所有服務狀態
make stack-logs     # 查看堆疊日誌
```

## 貢獻

歡迎貢獻！請開 issue 或提交 pull request。

## 授權

[MIT](LICENSE)
