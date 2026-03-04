#!/usr/bin/env python3
"""
OpenClaw Prometheus Exporter v3
- Proper logging instead of print()
- Efficient file reading (tail instead of readlines)
- Stale metric cleanup
- Removed redundant metrics
- Better error handling (no bare except)
"""

import os
import io
import time
import glob
import json
import logging
from prometheus_client import start_http_server, Gauge

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S',
)
log = logging.getLogger('openclaw_exporter')

# --- Agent metrics ---
ACTIVE_SESSIONS = Gauge('openclaw_active_sessions', 'Total active sessions')
AGENT_SESSIONS = Gauge('openclaw_agent_sessions', 'Sessions per agent', ['agent_name'])
AGENT_STATE = Gauge(
    'openclaw_agent_state',
    'Agent state (0=idle, 1=working, 2=thinking, 3=error)',
    ['agent_name'],
)
AGENT_LAST_ACTIVITY = Gauge(
    'openclaw_agent_last_activity_seconds',
    'Seconds since last activity',
    ['agent_name'],
)

# --- Cron job metrics ---
CRON_JOBS_TOTAL = Gauge('openclaw_cron_jobs_total', 'Total cron jobs')
CRON_JOBS_ENABLED = Gauge('openclaw_cron_jobs_enabled', 'Enabled cron jobs')
CRON_JOB_ENABLED = Gauge(
    'openclaw_cron_job_enabled',
    'Job enabled (1) or disabled (0)',
    ['job_name', 'job_id'],
)
CRON_JOB_LAST_RUN_AT = Gauge(
    'openclaw_cron_job_last_run_at_seconds',
    'Unix timestamp of last run',
    ['job_name', 'job_id'],
)
CRON_JOB_LAST_RUN_AGE = Gauge(
    'openclaw_cron_job_last_run_age_seconds',
    'Seconds since last run',
    ['job_name', 'job_id'],
)
CRON_JOB_NEXT_RUN_AT = Gauge(
    'openclaw_cron_job_next_run_at_seconds',
    'Unix timestamp of next run',
    ['job_name', 'job_id'],
)
CRON_JOB_NEXT_RUN_IN = Gauge(
    'openclaw_cron_job_next_run_in_seconds',
    'Seconds until next run',
    ['job_name', 'job_id'],
)
CRON_JOB_CONSECUTIVE_ERRORS = Gauge(
    'openclaw_cron_job_consecutive_errors',
    'Consecutive errors count',
    ['job_name', 'job_id'],
)
CRON_JOB_LAST_DURATION = Gauge(
    'openclaw_cron_job_last_duration_ms',
    'Last run duration in ms',
    ['job_name', 'job_id'],
)
CRON_JOB_LAST_DELIVERED = Gauge(
    'openclaw_cron_job_last_delivered',
    'Last message delivered (1/0)',
    ['job_name', 'job_id'],
)
CRON_JOB_CREATED_AT = Gauge(
    'openclaw_cron_job_created_at_seconds',
    'Job creation Unix timestamp',
    ['job_name', 'job_id'],
)

STATE_MAP = {'idle': 0, 'working': 1, 'thinking': 2, 'error': 3}
TAIL_BYTES = 8192  # Read last 8KB instead of entire file

# --- Workspace MD file metrics ---
MD_FILE_BYTES = Gauge("openclaw_md_file_bytes", "MD file size in bytes", ["workspace", "filename"])
MD_FILE_TOKENS_EST = Gauge("openclaw_md_file_tokens_estimated", "Estimated token count", ["workspace", "filename"])
MD_WORKSPACE_TOTAL_BYTES = Gauge("openclaw_md_workspace_total_bytes", "Total MD bytes in workspace", ["workspace"])
MD_WORKSPACE_TOTAL_TOKENS_EST = Gauge("openclaw_md_workspace_total_tokens_estimated", "Total estimated tokens", ["workspace"])

# --- Cron session token metrics ---
CRON_SESSION_TOKENS = Gauge("openclaw_cron_session_tokens_last", "Token usage in last cron session", ["agent", "cron_name", "token_type"])
CRON_SESSION_COST = Gauge("openclaw_cron_session_cost_last_usd", "Cost of last cron session", ["agent", "cron_name"])
CRON_SESSION_TOTAL_TOKENS = Gauge("openclaw_cron_session_total_tokens_last", "Total tokens in last session", ["agent", "cron_name"])

# --- Agent session token metrics ---
AGENT_SESSION_AVG_TOKENS = Gauge("openclaw_agent_session_avg_tokens", "Avg tokens per session (last 5)", ["agent", "token_type"])
AGENT_SESSION_AVG_COST = Gauge("openclaw_agent_session_avg_cost_usd", "Avg cost per session (last 5)", ["agent"])
AGENT_SESSION_LAST_TOKENS = Gauge("openclaw_agent_session_last_tokens", "Tokens in latest session", ["agent", "token_type"])
AGENT_SESSION_LAST_COST = Gauge("openclaw_agent_session_last_cost_usd", "Cost of latest session", ["agent"])

# Workspace name -> agent name mapping
WORKSPACE_MAP = {
    'workspace': 'main',
    'workspace-kanbei': 'kanbei',
    'workspace-mitsunari': 'mitsunari',
    'workspace-leyoyo': 'leyoyo',
}

AGENT_NAMES = ['main', 'kanbei', 'mitsunari', 'leyoyo']
SEVEN_DAYS_MS = 7 * 24 * 3600 * 1000
USAGE_TAIL_LINES = 500


def tail_lines(filepath, nbytes=TAIL_BYTES):
    """Read last N bytes of a file and return lines. Memory-safe for large files."""
    try:
        size = os.path.getsize(filepath)
        with open(filepath, 'rb') as f:
            if size > nbytes:
                f.seek(-nbytes, io.SEEK_END)
                f.readline()  # skip partial first line
            data = f.read().decode('utf-8', errors='replace')
            return data.strip().splitlines()
    except OSError as e:
        log.warning("Failed to read %s: %s", filepath, e)
        return []


def count_sessions(agent_path):
    """Count session files for an agent."""
    sessions_dir = os.path.join(agent_path, 'sessions')
    if not os.path.isdir(sessions_dir):
        return 0
    return len(glob.glob(os.path.join(sessions_dir, '*.jsonl')))


def get_agent_state(agent_path):
    """Detect agent state from most recent session file."""
    sessions_dir = os.path.join(agent_path, 'sessions')
    if not os.path.isdir(sessions_dir):
        return 'idle', 999

    files = glob.glob(os.path.join(sessions_dir, '*.jsonl'))
    if not files:
        return 'idle', 999

    latest = max(files, key=os.path.getmtime)
    seconds_ago = time.time() - os.path.getmtime(latest)

    lines = tail_lines(latest)
    for line in reversed(lines[-10:]):
        try:
            data = json.loads(line)
        except (json.JSONDecodeError, ValueError):
            continue

        msg = data.get('message', {})
        content = msg.get('content', [])
        if not isinstance(content, list):
            continue

        for c in content:
            c_type = c.get('type', '')
            if c_type == 'toolCall' and seconds_ago < 60:
                return 'working', seconds_ago
            if c_type == 'thinking' and seconds_ago < 120:
                return 'thinking', seconds_ago

        if msg.get('role') == 'assistant' and seconds_ago < 300:
            return 'working', seconds_ago

    return 'idle', seconds_ago


def get_cron_jobs():
    """Load cron jobs from disk."""
    cron_file = os.path.expanduser('~/.openclaw/cron/jobs.json')
    if not os.path.isfile(cron_file):
        return []
    try:
        with open(cron_file) as f:
            data = json.load(f)
            return data.get('jobs', [])
    except (json.JSONDecodeError, OSError) as e:
        log.warning("Failed to read cron jobs: %s", e)
        return []


def collect_agents(agents_dir, prev_agents):
    """Collect agent metrics. Returns set of current agent names."""
    current_agents = set()
    total_sessions = 0

    if not os.path.isdir(agents_dir):
        ACTIVE_SESSIONS.set(0)
        return current_agents

    for entry in os.scandir(agents_dir):
        if not entry.is_dir():
            continue
        name = entry.name
        current_agents.add(name)

        sessions = count_sessions(entry.path)
        state, seconds_ago = get_agent_state(entry.path)

        AGENT_SESSIONS.labels(agent_name=name).set(sessions)
        AGENT_STATE.labels(agent_name=name).set(STATE_MAP.get(state, 0))
        AGENT_LAST_ACTIVITY.labels(agent_name=name).set(seconds_ago)
        total_sessions += sessions

    ACTIVE_SESSIONS.set(total_sessions)

    # Clean up stale agent labels
    for stale in prev_agents - current_agents:
        AGENT_SESSIONS.remove(stale)
        AGENT_STATE.remove(stale)
        AGENT_LAST_ACTIVITY.remove(stale)
        log.info("Removed stale agent: %s", stale)

    return current_agents


def collect_cron_jobs(prev_job_keys):
    """Collect cron job metrics. Returns set of current (job_name, job_id) tuples."""
    jobs = get_cron_jobs()
    current_keys = set()

    CRON_JOBS_TOTAL.set(len(jobs))
    CRON_JOBS_ENABLED.set(sum(1 for j in jobs if j.get('enabled', False)))

    now = time.time()
    for job in jobs:
        job_id = job.get('id', '')[:8]
        job_name = job.get('name', 'Unknown')
        enabled = job.get('enabled', False)
        state = job.get('state', {})
        labels = dict(job_name=job_name, job_id=job_id)
        current_keys.add((job_name, job_id))

        CRON_JOB_ENABLED.labels(**labels).set(1 if enabled else 0)

        last_run_ms = state.get('lastRunAtMs')
        if last_run_ms:
            CRON_JOB_LAST_RUN_AGE.labels(**labels).set(now - last_run_ms / 1000)
            CRON_JOB_LAST_RUN_AT.labels(**labels).set(last_run_ms / 1000)

        next_run_ms = state.get('nextRunAtMs')
        if next_run_ms:
            CRON_JOB_NEXT_RUN_IN.labels(**labels).set(next_run_ms / 1000 - now)
            CRON_JOB_NEXT_RUN_AT.labels(**labels).set(next_run_ms / 1000)

        CRON_JOB_CONSECUTIVE_ERRORS.labels(**labels).set(state.get('consecutiveErrors', 0))

        last_duration = state.get('lastDurationMs')
        if last_duration is not None:
            CRON_JOB_LAST_DURATION.labels(**labels).set(last_duration)

        CRON_JOB_LAST_DELIVERED.labels(**labels).set(1 if state.get('lastDelivered') else 0)

        created_at_ms = job.get('createdAtMs')
        if created_at_ms:
            CRON_JOB_CREATED_AT.labels(**labels).set(created_at_ms / 1000)

    # Clean up stale job labels
    for stale_name, stale_id in prev_job_keys - current_keys:
        sl = dict(job_name=stale_name, job_id=stale_id)
        for metric in (CRON_JOB_ENABLED, CRON_JOB_LAST_RUN_AGE, CRON_JOB_LAST_RUN_AT,
                       CRON_JOB_NEXT_RUN_IN, CRON_JOB_NEXT_RUN_AT,
                       CRON_JOB_CONSECUTIVE_ERRORS, CRON_JOB_LAST_DURATION,
                       CRON_JOB_LAST_DELIVERED, CRON_JOB_CREATED_AT):
            try:
                metric.remove(stale_name, stale_id)
            except KeyError:
                pass
        log.info("Removed stale cron job: %s/%s", stale_name, stale_id)

    return current_keys


def collect_md_workspaces():
    """Scan workspace .md files and report size/token estimates."""
    base = os.path.expanduser('~/.openclaw')
    for ws_dir, agent in WORKSPACE_MAP.items():
        ws_path = os.path.join(base, ws_dir)
        if not os.path.isdir(ws_path):
            continue

        total_bytes = 0
        total_tokens = 0

        # Scan root .md files + memory/ subdirectory
        md_paths = glob.glob(os.path.join(ws_path, '*.md'))
        md_paths += glob.glob(os.path.join(ws_path, 'memory', '*.md'))

        for md_file in md_paths:
            try:
                content = open(md_file, 'rb').read()
                size = len(content)
                tokens = round(size / 3.5)
                filename = os.path.relpath(md_file, ws_path)
                MD_FILE_BYTES.labels(workspace=agent, filename=filename).set(size)
                MD_FILE_TOKENS_EST.labels(workspace=agent, filename=filename).set(tokens)
                total_bytes += size
                total_tokens += tokens
            except OSError:
                continue

        MD_WORKSPACE_TOTAL_BYTES.labels(workspace=agent).set(total_bytes)
        MD_WORKSPACE_TOTAL_TOKENS_EST.labels(workspace=agent).set(total_tokens)


def _load_sessions(agent_name):
    """Load sessions.json for an agent, return list of session dicts."""
    path = os.path.expanduser(f'~/.openclaw/agents/{agent_name}/sessions/sessions.json')
    if not os.path.isfile(path):
        return []
    try:
        with open(path) as f:
            data = json.load(f)
        if isinstance(data, dict):
            return list(data.values())
        return data
    except (json.JSONDecodeError, OSError):
        return []


def _read_session_usage(agent_name, session_id):
    """Read usage records from a session JSONL. Returns aggregated totals."""
    path = os.path.expanduser(
        f'~/.openclaw/agents/{agent_name}/sessions/{session_id}.jsonl'
    )
    if not os.path.isfile(path):
        return None

    totals = {'input': 0, 'output': 0, 'cacheRead': 0, 'cacheWrite': 0, 'totalTokens': 0, 'cost': 0.0}

    try:
        size = os.path.getsize(path)
        with open(path, 'rb') as f:
            # For large files, only read last portion
            if size > 65536:
                f.seek(-65536, io.SEEK_END)
                f.readline()  # skip partial line
            data = f.read().decode('utf-8', errors='replace')

        count = 0
        for line in data.strip().splitlines():
            try:
                rec = json.loads(line)
            except (json.JSONDecodeError, ValueError):
                continue

            usage = rec.get('usage')
            if not usage and isinstance(rec.get('message'), dict):
                usage = rec['message'].get('usage')
            if not usage:
                continue

            totals['input'] += usage.get('input', 0)
            totals['output'] += usage.get('output', 0)
            totals['cacheRead'] += usage.get('cacheRead', 0)
            totals['cacheWrite'] += usage.get('cacheWrite', 0)
            totals['totalTokens'] += usage.get('totalTokens', 0)
            cost = usage.get('cost', {})
            if isinstance(cost, dict):
                totals['cost'] += cost.get('total', 0)
            elif isinstance(cost, (int, float)):
                totals['cost'] += cost
            count += 1

        return totals if count > 0 else None
    except OSError:
        return None


def collect_cron_session_tokens():
    """Collect token usage for cron sessions (last 7 days, latest per cron_name)."""
    now_ms = time.time() * 1000
    cutoff_ms = now_ms - SEVEN_DAYS_MS

    for agent in AGENT_NAMES:
        sessions = _load_sessions(agent)
        # Filter cron sessions within 7 days
        cron_sessions = {}
        for s in sessions:
            label = s.get('label') or ''
            if 'Cron:' not in label:
                continue
            updated = s.get('updatedAt', 0)
            if updated < cutoff_ms:
                continue
            cron_name = label.replace('Cron: ', '').replace('Cron:', '').strip()
            if cron_name not in cron_sessions or updated > cron_sessions[cron_name].get('updatedAt', 0):
                cron_sessions[cron_name] = s

        for cron_name, s in cron_sessions.items():
            usage = _read_session_usage(agent, s['sessionId'])
            if not usage:
                continue
            for token_type in ('input', 'output', 'cacheRead', 'cacheWrite'):
                CRON_SESSION_TOKENS.labels(agent=agent, cron_name=cron_name, token_type=token_type).set(usage[token_type])
            CRON_SESSION_COST.labels(agent=agent, cron_name=cron_name).set(usage['cost'])
            CRON_SESSION_TOTAL_TOKENS.labels(agent=agent, cron_name=cron_name).set(usage['totalTokens'])


def collect_agent_session_tokens():
    """Collect token usage for non-cron agent sessions (last 5, averages)."""
    for agent in AGENT_NAMES:
        sessions = _load_sessions(agent)
        # Filter non-cron sessions, sort by updatedAt desc
        regular = [s for s in sessions if not (s.get('label') or '').startswith('Cron')]
        regular.sort(key=lambda s: s.get('updatedAt', 0), reverse=True)
        recent = regular[:5]

        if not recent:
            continue

        all_usage = []
        for s in recent:
            usage = _read_session_usage(agent, s['sessionId'])
            if usage:
                all_usage.append(usage)

        if not all_usage:
            continue

        # Latest session
        latest = all_usage[0]
        for token_type in ('input', 'output', 'cacheRead', 'cacheWrite'):
            AGENT_SESSION_LAST_TOKENS.labels(agent=agent, token_type=token_type).set(latest[token_type])
        AGENT_SESSION_LAST_COST.labels(agent=agent).set(latest['cost'])

        # Averages
        n = len(all_usage)
        for token_type in ('input', 'output', 'cacheRead', 'cacheWrite'):
            avg = sum(u[token_type] for u in all_usage) / n
            AGENT_SESSION_AVG_TOKENS.labels(agent=agent, token_type=token_type).set(round(avg))
        avg_cost = sum(u['cost'] for u in all_usage) / n
        AGENT_SESSION_AVG_COST.labels(agent=agent).set(avg_cost)


def main():
    port = int(os.environ.get('EXPORTER_PORT', '9101'))
    agents_dir = os.path.expanduser('~/.openclaw/agents')

    log.info("Starting OpenClaw exporter v3 on port %d", port)
    start_http_server(port)
    log.info("Metrics: http://localhost:%d/metrics", port)

    prev_agents = set()
    prev_job_keys = set()
    token_scan_counter = 0

    while True:
        try:
            prev_agents = collect_agents(agents_dir, prev_agents)
            prev_job_keys = collect_cron_jobs(prev_job_keys)

            # Token/MD scanning every 60s (6 x 10s cycles)
            if token_scan_counter % 6 == 0:
                collect_md_workspaces()
                collect_cron_session_tokens()
                collect_agent_session_tokens()
                log.debug("Token scan completed")
            token_scan_counter += 1

        except Exception:
            log.exception("Collection error")
        time.sleep(10)


if __name__ == '__main__':
    main()
