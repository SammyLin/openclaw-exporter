FROM ghcr.io/astral-sh/uv:python3.11-bookworm-slim AS builder

WORKDIR /app

COPY pyproject.toml requirements.txt ./
RUN uv venv /app/.venv && uv pip install --python /app/.venv/bin/python -r requirements.txt

COPY openclaw_exporter/ openclaw_exporter/

FROM python:3.11-slim

RUN groupadd --gid 1000 exporter && \
    useradd --uid 1000 --gid exporter --shell /bin/sh exporter

COPY --from=builder /app/.venv /app/.venv
COPY --from=builder /app/openclaw_exporter /app/openclaw_exporter

WORKDIR /app
USER exporter

ENV PATH="/app/.venv/bin:$PATH"

EXPOSE 9101

ENTRYPOINT ["python", "-m", "openclaw_exporter"]
