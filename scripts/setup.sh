#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
BASE_DIR="$(pwd)"
OPENCLAW_HOME="${OPENCLAW_HOME:-$HOME/.openclaw}"
PLIST_DST="$HOME/Library/LaunchAgents/com.openclaw.exporter.plist"

echo "=== OpenClaw Monitoring Setup ==="
echo "Base directory: $BASE_DIR"
echo "OpenClaw home:  $OPENCLAW_HOME"
echo ""

# 1. .env
echo "[1/5] Checking .env..."
if [ ! -f .env ]; then
    cp .env.example .env
    echo "  Created .env from .env.example — please edit passwords!"
fi
source .env 2>/dev/null || true

# 2. uv + dependencies
echo "[2/5] Setting up Python env with uv..."
if ! command -v uv &>/dev/null; then
    echo "  Installing uv..."
    curl -LsSf https://astral.sh/uv/install.sh | sh
    export PATH="$HOME/.cargo/bin:$PATH"
fi
uv sync
echo "  Python env ready (.venv)"

# 3. launchd (macOS only)
if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "[3/5] Installing launchd service..."
    sed -e "s|INSTALL_DIR|$BASE_DIR|g" \
        -e "s|OPENCLAW_HOME_DIR|$OPENCLAW_HOME|g" \
        com.openclaw.exporter.plist.template > "$PLIST_DST"
    launchctl unload "$PLIST_DST" 2>/dev/null || true
    launchctl load "$PLIST_DST"
    launchctl start com.openclaw.exporter || true
    echo "  launchd service loaded"
else
    echo "[3/5] Non-macOS detected. Run exporter manually:"
    echo "  uv run python openclaw_exporter.py &"
fi

# 4. Docker services
echo "[4/5] Starting Docker services..."
docker compose up -d
echo "  Docker services started"

# 5. Health check
echo "[5/5] Running health check..."
sleep 5
bash scripts/health-check.sh

echo ""
echo "Setup complete!"
echo "   Grafana:    http://localhost:3000"
echo "   Prometheus: http://localhost:9090"
echo "   Exporter:   http://localhost:9101/metrics"
