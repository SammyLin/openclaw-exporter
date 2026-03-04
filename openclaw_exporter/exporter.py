"""Main exporter entry point — HTTP server and collection loop."""

import argparse
import logging
import os
import time

from prometheus_client import start_http_server

from . import __version__
from .collector import (
    collect_agents,
    collect_agent_session_tokens,
    collect_cron_jobs,
    collect_cron_session_tokens,
    collect_md_workspaces,
)

log = logging.getLogger("openclaw_exporter")


def parse_args():
    parser = argparse.ArgumentParser(
        description="OpenClaw Prometheus Exporter",
    )
    parser.add_argument(
        "--web.listen-address",
        dest="listen_address",
        default=os.environ.get("EXPORTER_LISTEN_ADDRESS", ":9101"),
        help="Address to listen on (default: :9101)",
    )
    parser.add_argument(
        "--web.telemetry-path",
        dest="telemetry_path",
        default=os.environ.get("EXPORTER_TELEMETRY_PATH", "/metrics"),
        help="Path under which to expose metrics (default: /metrics)",
    )
    parser.add_argument(
        "--openclaw.home",
        dest="openclaw_home",
        default=os.environ.get("OPENCLAW_HOME", os.path.expanduser("~/.openclaw")),
        help="Path to OpenClaw home directory (default: ~/.openclaw)",
    )
    parser.add_argument(
        "--log.level",
        dest="log_level",
        default=os.environ.get("EXPORTER_LOG_LEVEL", "info"),
        choices=["debug", "info", "warning", "error"],
        help="Log level (default: info)",
    )
    parser.add_argument(
        "--version",
        action="version",
        version=f"openclaw-exporter {__version__}",
    )
    return parser.parse_args()


def main():
    args = parse_args()

    logging.basicConfig(
        level=getattr(logging, args.log_level.upper()),
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )

    # Parse listen address
    host, _, port_str = args.listen_address.rpartition(":")
    port = int(port_str)
    addr = host if host else "0.0.0.0"

    openclaw_home = args.openclaw_home
    agents_dir = os.path.join(openclaw_home, "agents")

    log.info(
        "Starting openclaw-exporter %s on %s:%d (openclaw.home=%s)",
        __version__, addr, port, openclaw_home,
    )
    start_http_server(port, addr=addr)
    log.info("Metrics available at http://%s:%d%s", addr, port, args.telemetry_path)

    prev_agents = set()
    prev_job_keys = set()
    token_scan_counter = 0

    while True:
        try:
            prev_agents = collect_agents(agents_dir, prev_agents)
            prev_job_keys = collect_cron_jobs(openclaw_home, prev_job_keys)

            # Token/MD scanning every 60s (6 x 10s cycles)
            if token_scan_counter % 6 == 0:
                collect_md_workspaces(openclaw_home)
                collect_cron_session_tokens(openclaw_home)
                collect_agent_session_tokens(openclaw_home)
                log.debug("Token scan completed")
            token_scan_counter += 1

        except Exception:
            log.exception("Collection error")
        time.sleep(10)
