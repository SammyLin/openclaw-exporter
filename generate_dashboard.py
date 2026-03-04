#!/usr/bin/env python3
"""Generate OpenClaw Complete Grafana Dashboard JSON"""

import json
import os

DS = {"type": "prometheus", "uid": "PBFA97CFB590B2093"}
_id = 0

def next_id():
    global _id
    _id += 1
    return _id

def row(title, y):
    return {
        "type": "row",
        "id": next_id(),
        "title": title,
        "collapsed": False,
        "gridPos": {"x": 0, "y": y, "w": 24, "h": 1},
        "panels": []
    }

def stat_panel(title, expr, x, y, w=6, h=4, unit=None, thresholds=None):
    p = {
        "type": "stat",
        "id": next_id(),
        "title": title,
        "datasource": DS,
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "targets": [{"expr": expr, "legendFormat": "", "refId": "A"}],
        "fieldConfig": {"defaults": {"thresholds": {"mode": "absolute", "steps": thresholds or [{"color": "green", "value": None}]}}},
        "options": {"reduceOptions": {"calcs": ["lastNotNull"]}, "colorMode": "value", "graphMode": "area"}
    }
    if unit:
        p["fieldConfig"]["defaults"]["unit"] = unit
    return p

def timeseries_panel(title, targets, x, y, w=12, h=8, unit=None):
    p = {
        "type": "timeseries",
        "id": next_id(),
        "title": title,
        "datasource": DS,
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "targets": targets,
        "fieldConfig": {
            "defaults": {
                "custom": {"lineWidth": 2, "gradientMode": "opacity"}
            }
        },
        "options": {"legend": {"displayMode": "list", "placement": "bottom"}}
    }
    if unit:
        p["fieldConfig"]["defaults"]["unit"] = unit
    return p

def piechart_panel(title, expr, legend, x, y, w=6, h=8):
    return {
        "type": "piechart",
        "id": next_id(),
        "title": title,
        "datasource": DS,
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "targets": [{"expr": expr, "legendFormat": legend, "refId": "A"}],
        "fieldConfig": {"defaults": {}},
        "options": {"legend": {"displayMode": "list", "placement": "right"}, "pieType": "pie"}
    }

def bargauge_panel(title, expr, legend, x, y, w=8, h=8, unit=None, orientation="horizontal", thresholds=None):
    p = {
        "type": "bargauge",
        "id": next_id(),
        "title": title,
        "datasource": DS,
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "targets": [{"expr": expr, "legendFormat": legend, "refId": "A"}],
        "fieldConfig": {
            "defaults": {
                "thresholds": {"mode": "absolute", "steps": thresholds or [{"color": "green", "value": None}]}
            }
        },
        "options": {"orientation": orientation, "displayMode": "gradient", "reduceOptions": {"calcs": ["lastNotNull"]}}
    }
    if unit:
        p["fieldConfig"]["defaults"]["unit"] = unit
    return p

def gauge_panel(title, expr, x, y, w=6, h=6, unit="percent", min_val=0, max_val=100, thresholds=None):
    return {
        "type": "gauge",
        "id": next_id(),
        "title": title,
        "datasource": DS,
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "targets": [{"expr": expr, "legendFormat": "", "refId": "A"}],
        "fieldConfig": {
            "defaults": {
                "unit": unit,
                "min": min_val,
                "max": max_val,
                "thresholds": {"mode": "absolute", "steps": thresholds or [{"color": "green", "value": None}]}
            }
        },
        "options": {"reduceOptions": {"calcs": ["lastNotNull"]}}
    }

def table_panel(title, targets, x, y, w=12, h=10):
    return {
        "type": "table",
        "id": next_id(),
        "title": title,
        "datasource": DS,
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "targets": targets,
        "fieldConfig": {"defaults": {}},
        "options": {},
        "transformations": [{"id": "merge", "options": {}}]
    }

def target(expr, legend="", ref="A", instant=False):
    t = {"expr": expr, "legendFormat": legend, "refId": ref}
    if instant:
        t["instant"] = True
        t["format"] = "table"
    return t

# Build panels
panels = []

# Row 1: 業務概覽
panels.append(row("🎯 業務概覽", 0))
panels.append(stat_panel("今日訊息數", "increase(openclaw_message_processed_total[1d])", 0, 1))
panels.append(stat_panel("今日費用USD", "sum(increase(openclaw_cost_usd_total[1d]))", 6, 1, unit="currencyUSD",
    thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 0.5}, {"color": "red", "value": 1}]))
panels.append(stat_panel("今日Tokens", 'sum(increase(openclaw_tokens_total{openclaw_token=~"input|output"}[1d]))', 12, 1))
panels.append(stat_panel("Active Agents", 'count(openclaw_agent_state{state=~"working|thinking"} > 0) or vector(0)', 18, 1))

# Row 2: LLM 使用
panels.append(row("💰 LLM 使用", 5))
panels.append(timeseries_panel("費用趨勢 by model",
    [target("sum by (openclaw_model)(rate(openclaw_cost_usd_total[10m]))*3600", "{{openclaw_model}}")],
    0, 6, w=12, h=8, unit="currencyUSD"))
panels.append(piechart_panel("費用分佈",
    "sum by (openclaw_model)(increase(openclaw_cost_usd_total[1d]))", "{{openclaw_model}}", 12, 6))
panels.append(bargauge_panel("Input/Output Tokens",
    'sum by (openclaw_model,openclaw_token)(increase(openclaw_tokens_total{openclaw_token=~"input|output"}[1d]))',
    "{{openclaw_model}}-{{openclaw_token}}", 18, 6, w=6, h=8))

# Row 3: Agent 狀態
panels.append(row("🤖 Agent 狀態", 14))
panels.append(bargauge_panel("Sessions per Agent", "openclaw_agent_sessions", "{{agent_name}}", 0, 15))
panels.append(bargauge_panel("Last Activity秒", "openclaw_agent_last_activity_seconds", "{{agent_name}}", 8, 15, unit="s",
    thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 300}, {"color": "red", "value": 1800}]))
panels.append(bargauge_panel("Agent Working",
    'openclaw_agent_state{state="working"} or openclaw_agent_state{state="thinking"}',
    "{{agent_name}}-{{state}}", 16, 15))

# Row 4: Session 運作
panels.append(row("📋 Session 運作", 23))
panels.append(timeseries_panel("Session 處理速率",
    [target('sum(rate(openclaw_session_state_total{openclaw_state="processing"}[5m]))')],
    0, 24, w=8, h=6, unit="reqps"))
panels.append(timeseries_panel("Run Duration P95",
    [target("histogram_quantile(0.95,sum by(le)(rate(openclaw_run_duration_ms_milliseconds_bucket[5m])))")],
    8, 24, w=8, h=6, unit="ms"))
panels.append(timeseries_panel("訊息處理時間 P50/P95", [
    target("histogram_quantile(0.50,sum by(le)(rate(openclaw_message_duration_ms_milliseconds_bucket[5m])))", "P50", "A"),
    target("histogram_quantile(0.95,sum by(le)(rate(openclaw_message_duration_ms_milliseconds_bucket[5m])))", "P95", "B")
], 16, 24, w=8, h=6, unit="ms"))

# Row 5: Cron Jobs
panels.append(row("⏰ Cron Jobs", 30))
panels.append(stat_panel("啟用率", "openclaw_cron_jobs_enabled/openclaw_cron_jobs_total*100", 0, 31, w=4, h=4, unit="percent"))
panels.append(stat_panel("總計Jobs", "openclaw_cron_jobs_total", 4, 31, w=4, h=4))
panels.append(stat_panel("啟用Jobs", "openclaw_cron_jobs_enabled", 8, 31, w=4, h=4))
panels.append(table_panel("Cron Jobs 詳細", [
    target("openclaw_cron_job_enabled_detail", "{{job_name}}", "A", instant=True),
    target("openclaw_cron_job_last_run_age_seconds", "", "B", instant=True),
    target("openclaw_cron_job_next_run_in_seconds", "", "C", instant=True),
    target("openclaw_cron_job_consecutive_errors", "", "D", instant=True),
    target("openclaw_cron_job_last_duration_ms", "", "E", instant=True),
], 12, 31))

# Row 6: Channel 活躍度
panels.append(row("📡 Channel 活躍度", 41))
panels.append(timeseries_panel("訊息量 by channel",
    [target("sum by(openclaw_channel)(rate(openclaw_message_processed_total[5m]))*60", "{{openclaw_channel}}")],
    0, 42, w=12, h=6, unit="reqps"))
panels.append(timeseries_panel("Tokens by channel",
    [target('sum by(openclaw_channel)(rate(openclaw_tokens_total{openclaw_token=~"input|output"}[10m]))', "{{openclaw_channel}}")],
    12, 42, w=12, h=6))

# Row 7: 效能
panels.append(row("⚡ 效能", 48))
panels.append(timeseries_panel("Queue 深度",
    [target("sum(openclaw_queue_lane_enqueue_total)-sum(openclaw_queue_lane_dequeue_total)")],
    0, 49, w=8, h=6))
panels.append(timeseries_panel("Queue Wait P95",
    [target("histogram_quantile(0.95,sum by(le)(rate(openclaw_queue_wait_ms_milliseconds_bucket[5m])))")],
    8, 49, w=8, h=6, unit="ms"))
panels.append(timeseries_panel("Tool Calls/min",
    [target("sum(rate(openclaw_tool_calls_total[5m]))*60")],
    16, 49, w=8, h=6, unit="short"))

# Row 8: 系統資源
sys_thresholds = [{"color": "green", "value": None}, {"color": "yellow", "value": 70}, {"color": "red", "value": 90}]
panels.append(row("🖥️ 系統資源", 55))
panels.append(gauge_panel("CPU",
    '100-(avg(irate(node_cpu_seconds_total{mode="idle"}[5m]))*100)',
    0, 56, thresholds=sys_thresholds))
panels.append(gauge_panel("Memory",
    "(1-(node_memory_MemAvailable_bytes/node_memory_MemTotal_bytes))*100",
    6, 56, thresholds=sys_thresholds))
panels.append(gauge_panel("Disk",
    '(1-(node_filesystem_avail_bytes{mountpoint="/"}/node_filesystem_size_bytes{mountpoint="/"}))*100',
    12, 56, thresholds=sys_thresholds))
panels.append(timeseries_panel("Network", [
    target('rate(node_network_receive_bytes_total{device!~"lo|veth.*|br-.*"}[5m])', "↓ RX", "A"),
    target('rate(node_network_transmit_bytes_total{device!~"lo|veth.*|br-.*"}[5m])', "↑ TX", "B"),
], 18, 56, w=6, h=6, unit="Bps"))

# Assemble dashboard
dashboard = {
    "uid": "openclaw-complete",
    "title": "🍡 OpenClaw Complete Monitor",
    "tags": ["openclaw", "monitoring"],
    "timezone": "Asia/Taipei",
    "schemaVersion": 38,
    "version": 1,
    "refresh": "30s",
    "time": {"from": "now-6h", "to": "now"},
    "fiscalYearStartMonth": 0,
    "liveNow": False,
    "editable": True,
    "panels": panels,
    "templating": {"list": []},
    "annotations": {"list": []},
    "links": []
}

# Write output
out_dir = os.path.join(os.path.dirname(__file__), "grafana", "dashboards")
os.makedirs(out_dir, exist_ok=True)
out_path = os.path.join(out_dir, "openclaw-complete.json")

with open(out_path, "w") as f:
    json.dump(dashboard, f, ensure_ascii=False, indent=2)

print(f"Dashboard written to {out_path}")
print(f"Total panels: {len(panels)} (including rows)")
print(f"Panel IDs: 1-{_id}")
