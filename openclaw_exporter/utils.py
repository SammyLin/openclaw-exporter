"""Shared helper utilities."""

import io
import os
import logging

log = logging.getLogger("openclaw_exporter")

TAIL_BYTES = 8192  # Read last 8KB instead of entire file


def tail_lines(filepath, nbytes=TAIL_BYTES):
    """Read last N bytes of a file and return lines. Memory-safe for large files."""
    try:
        size = os.path.getsize(filepath)
        with open(filepath, "rb") as f:
            if size > nbytes:
                f.seek(-nbytes, io.SEEK_END)
                f.readline()  # skip partial first line
            data = f.read().decode("utf-8", errors="replace")
            return data.strip().splitlines()
    except OSError as e:
        log.warning("Failed to read %s: %s", filepath, e)
        return []
