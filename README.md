# OpenClaw Monitoring Stack

OpenClaw + 系統監控完整方案，包含 LLM 費用追蹤、Agent 狀態、Cron Job 監控及系統資源。

## 架構

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

### 資料流

| 來源 | 協議 | 收集方式 | 指標範例 |
|------|------|----------|----------|
| OpenClaw Gateway | OTLP HTTP/gRPC | Push → OTEL Collector → Prometheus | `openclaw_cost_usd_total`, `openclaw_tokens_total` |
| openclaw_exporter | Prometheus scrape | Pull (Prometheus → host:9101) | `openclaw_agent_state`, `openclaw_cron_job_*` |
| Node Exporter | Prometheus scrape | Pull (Prometheus → container:9100) | `node_cpu_seconds_total`, `node_memory_*` |

### 元件說明

| 元件 | 部署 | Port | 用途 |
|------|------|------|------|
| **Grafana** | Docker | 3000 | Dashboard 視覺化 |
| **Prometheus** | Docker | 9090 | 指標儲存 (保留 30 天 / 5GB) |
| **OTEL Collector** | Docker | 4317/4318/8889 | OTLP → Prometheus 轉換 |
| **Node Exporter** | Docker | 9100 | 系統 CPU/Memory/Disk/Network |
| **openclaw_exporter** | Host (launchd) | 9101 | Agent 狀態、Cron Job 監控 |

### 架構設計考量

**openclaw_exporter 跑在 host 而非 container：**
- 需直接讀取 `~/.openclaw/` 目錄下的 JSONL 和 JSON 檔案
- 避免 volume mount 的權限和效能問題
- 用 launchd 管理，開機自動啟動、crash 自動重啟

**OTEL Collector 為何需要：**
- OpenClaw Gateway 使用 OTLP push 協議，Prometheus 是 pull-based
- OTEL Collector 負責 OTLP → Prometheus 格式轉換及 batch 處理
- 如果 OpenClaw 未來改為 Prometheus 格式直出，可移除 OTEL Collector

**Alerting：**
- Alert rules 定義在 `prometheus/rules.yml`（CPU/Memory/Disk 閾值告警）
- `alerting/` 目錄保留給未來的 Alertmanager 設定（Slack/Email 通知）
- 目前 Prometheus alerting config 中 alertmanager targets 為空

---

## Quick Start

```bash
# 完整安裝（venv + launchd + docker + health check）
make setup

# 或手動：
make up               # 啟動 docker services
make exporter-restart  # 重啟 openclaw_exporter
make health            # 確認所有服務正常
```

## 常用操作 (Makefile)

| 命令 | 說明 |
|------|------|
| `make up` | 啟動所有 Docker 服務 |
| `make down` | 停止所有 Docker 服務 |
| `make restart` | 重啟 Docker 服務 |
| `make status` | 顯示所有服務狀態 + Prometheus targets |
| `make logs` | tail Docker 服務 logs |
| `make exporter-restart` | 重啟 openclaw_exporter (launchd) |
| `make exporter-log` | tail exporter log |
| `make exporter-status` | 查看 exporter launchd 狀態 |
| `make reload-dashboard` | 重新載入 Grafana dashboard provisioning |
| `make metrics` | 檢視 exporter 目前指標 |
| `make health` | 執行完整健康檢查 |
| `make setup` | 首次安裝（venv + launchd + docker） |

---

## Dashboard

### OpenClaw Complete Monitor
URL: `http://localhost:3000/d/openclaw-complete`

| Section | 指標 |
|---------|------|
| 業務概覽 | 今日訊息數、費用(USD)、Tokens、Active Agents |
| LLM 使用 | 費用趨勢 by model、pie chart、input/output tokens |
| Agent 狀態 | Sessions 數量、Last Activity、working/thinking 狀態 |
| Session 運作 | 處理速率、Run Duration P95、訊息處理 P50/P95 |
| Cron Jobs | 啟用率、Job 詳細表（名稱、狀態、錯誤數、執行時間）|
| Channel 活躍 | telegram / heartbeat / cron 流量分布 |
| 效能 | Queue 深度、Wait time P95、Tool calls 速率 |
| 系統資源 | CPU / Memory / Disk (virtiofs) / Network |

---

## Prometheus 設定指南

### Scrape Config

設定檔：`prometheus/prometheus.yml`

```yaml
scrape_configs:
  - job_name: 'openclaw-exporter'
    static_configs:
      - targets: ['host.docker.internal:9101']
```

**重點：** Prometheus 跑在 Docker 裡，要 scrape host 上的 exporter 需用 `host.docker.internal`。

### 新增 Scrape Target

在 `prometheus/prometheus.yml` 的 `scrape_configs` 加入新 job：

```yaml
  - job_name: 'my-new-exporter'
    scrape_interval: 30s          # 可選，預設用 global 15s
    static_configs:
      - targets: ['hostname:port']
    # 如果需要 relabel：
    # metric_relabel_configs:
    #   - source_labels: [__name__]
    #     regex: 'unwanted_.*'
    #     action: drop
```

修改後 reload Prometheus（不用重啟）：
```bash
curl -X POST http://localhost:9090/-/reload
```

### 資料保留策略

目前設定：**30 天** 或 **5GB**（先到者為準）。
修改位置：`docker-compose.yml` 的 prometheus command：
```yaml
- '--storage.tsdb.retention.time=30d'
- '--storage.tsdb.retention.size=5GB'
```

### Alert Rules

定義在 `prometheus/rules.yml`，目前包含：

| Alert | 閾值 | 持續時間 | 嚴重度 |
|-------|------|----------|--------|
| HighCPUUsage | > 85% | 5m | warning |
| HighMemoryUsage | > 85% | 5m | warning |
| HighDiskUsage | > 85% | 5m | warning |
| CriticalCPUUsage | > 95% | 2m | critical |
| CriticalMemoryUsage | > 95% | 2m | critical |
| CriticalDiskUsage | > 95% | 2m | critical |
| NodeDown | up == 0 | 1m | critical |

---

## Grafana 設定指南

### Datasource 設定

設定檔：`grafana/provisioning/datasources/datasources.yml`

```yaml
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    uid: PBFA97CFB590B2093    # 固定 UID，dashboard JSON 使用此值
    isDefault: true
```

**重要：** `uid: PBFA97CFB590B2093` 是固定的。所有 dashboard JSON 的 `datasource.uid` 都使用這個值。如果改了 UID，所有 dashboard 都需要一起更新。

### Dashboard Provisioning

設定檔：`grafana/provisioning/dashboards/default.yaml`

Dashboard JSON 放在 `grafana/dashboards/` 目錄，Grafana 每 30 秒自動掃描載入。

### 匯入 Dashboard

**方法 1：放檔案（推薦）**
```bash
# 將 JSON 放入 dashboards 目錄
cp my-dashboard.json grafana/dashboards/
# 觸發立即載入
make reload-dashboard
```

**方法 2：用 generate_dashboard.py 產生**
```bash
# 修改 generate_dashboard.py 後重新產生
python3 generate_dashboard.py
make reload-dashboard
```

**方法 3：API 匯入**
```bash
# 從 Grafana UI 匯出的 JSON 透過 API 匯入
curl -X POST http://admin:admin123@localhost:3000/api/dashboards/db \
  -H 'Content-Type: application/json' \
  -d '{"dashboard": <json>, "overwrite": true}'
```

### 新增 Dashboard

1. 在 `grafana/dashboards/` 建立新 JSON 檔
2. 確保 dashboard JSON 中的 `datasource.uid` 設為 `PBFA97CFB590B2093`
3. `make reload-dashboard` 或等 30 秒自動載入

如果用 `generate_dashboard.py` 風格，datasource 定義：
```python
DS = {"type": "prometheus", "uid": "PBFA97CFB590B2093"}
```

### 更新現有 Dashboard

```bash
# 編輯 JSON 後重新載入
make reload-dashboard

# 或重啟 Grafana
docker compose restart grafana
```

---

## openclaw_exporter 說明

**檔案**：`openclaw_exporter.py` (v3)
**Python venv**：`./venv/`
**Port**：9101 (環境變數 `EXPORTER_PORT`)
**管理**：launchd (`com.openclaw.exporter`)

### 資料來源
- `~/.openclaw/agents/*/sessions/` — Agent session JSONL 檔案
- `~/.openclaw/cron/jobs.json` — Cron job 設定

### Agent 指標

| 指標 | 說明 | Labels |
|------|------|--------|
| `openclaw_active_sessions` | 總 session 數 | — |
| `openclaw_agent_sessions` | 每個 agent 的 session 數 | agent_name |
| `openclaw_agent_state` | 狀態 (0=idle, 1=working, 2=thinking, 3=error) | agent_name |
| `openclaw_agent_last_activity_seconds` | 距離上次活動的秒數 | agent_name |

### Cron Job 指標

| 指標 | 說明 | Labels |
|------|------|--------|
| `openclaw_cron_jobs_total` | Cron job 總數 | — |
| `openclaw_cron_jobs_enabled` | 啟用中的 job 數 | — |
| `openclaw_cron_job_enabled` | Job 啟用狀態 (1/0) | job_name, job_id |
| `openclaw_cron_job_last_run_age_seconds` | 距離上次執行秒數 | job_name, job_id |
| `openclaw_cron_job_next_run_in_seconds` | 距離下次執行秒數 | job_name, job_id |
| `openclaw_cron_job_consecutive_errors` | 連續錯誤次數 | job_name, job_id |
| `openclaw_cron_job_last_duration_ms` | 上次執行時間 (ms) | job_name, job_id |
| `openclaw_cron_job_last_delivered` | 上次訊息送達 (1/0) | job_name, job_id |
| `openclaw_cron_job_created_at_seconds` | 建立時間 (Unix) | job_name, job_id |
| `openclaw_cron_job_last_run_at_seconds` | 上次執行時間 (Unix) | job_name, job_id |

### OTEL 指標（來自 OpenClaw Gateway）

| 指標 | 說明 |
|------|------|
| `openclaw_cost_usd_total` | 費用累計 (USD) |
| `openclaw_tokens_total` | Token 用量累計 |
| `openclaw_message_processed_total` | 處理訊息數 |
| `openclaw_session_state_total` | Session 狀態轉換 |
| `openclaw_run_duration_ms_milliseconds_*` | Agent run 時間 histogram |
| `openclaw_message_duration_ms_milliseconds_*` | 訊息處理時間 histogram |
| `openclaw_queue_depth_*` | Queue 深度 |
| `openclaw_tool_calls_total` | Tool call 累計 |

---

## 已知特殊設定

### Disk 指標
Node Exporter 跑在 OrbStack container，Mac 磁碟掛載為 virtiofs：
```promql
(1 - (node_filesystem_avail_bytes{device="mac",mountpoint="/Users"}
    / node_filesystem_size_bytes{device="mac",mountpoint="/Users"})) * 100
```

### Datasource UID
固定為 `PBFA97CFB590B2093`，定義在 `grafana/provisioning/datasources/datasources.yml`。
Dashboard JSON 和 `generate_dashboard.py` 都使用此值。

---

## 檔案結構

```
monitoring/
├── Makefile                           # 常用操作快捷命令
├── README.md
├── docker-compose.yml                 # Docker 服務定義
├── openclaw_exporter.py               # 自定義 Prometheus exporter (v3)
├── generate_dashboard.py              # Dashboard JSON 產生器
├── requirements.txt                   # Python 依賴
├── exporter.log                       # Exporter 日誌
├── scripts/
│   ├── health-check.sh                # 健康檢查
│   └── setup.sh                       # 首次安裝
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── datasources.yml        # Prometheus datasource (UID 固定)
│   │   └── dashboards/
│   │       └── default.yaml           # Dashboard 自動載入設定
│   └── dashboards/
│       ├── openclaw-complete.json     # 主 dashboard (40+ panels)
│       ├── openclaw-monitor.json      # OpenClaw 概覽
│       └── system-monitor.json        # 系統資源
├── prometheus/
│   ├── prometheus.yml                 # Scrape config
│   └── rules.yml                      # Alert rules
├── otel-collector/
│   └── config.yaml                    # OTLP receiver → Prometheus exporter
└── alerting/                          # 預留給 Alertmanager
```

---

## 故障排除

```bash
# 查看所有服務狀態
make status

# Exporter 沒回應
make exporter-log                      # 看 log
launchctl list | grep openclaw         # 確認 launchd 狀態
make exporter-restart                  # 重啟

# Prometheus target down
curl http://localhost:9090/api/v1/targets  # 查看 target 狀態
docker compose logs prometheus             # Prometheus logs

# Dashboard 沒更新
make reload-dashboard                  # 手動 reload
docker compose restart grafana         # 或重啟 Grafana

# 完整健康檢查
make health
```
