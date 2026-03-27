#!/usr/bin/env bash
set -euo pipefail

# ── AgentProxy installer for macOS and Linux ──────────────────────────────
# Usage: ./install.sh

cd "$(dirname "${BASH_SOURCE[0]}")"

# ── Check Python ────────────────────────────────────────────────────────────
PYTHON=""
for candidate in python3 python; do
    if command -v "$candidate" &>/dev/null; then
        if "$candidate" -c "import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)" 2>/dev/null; then
            PYTHON="$candidate"
            break
        fi
    fi
done

if [ -z "$PYTHON" ]; then
    echo ""
    echo "  ERROR: Python 3.11+ is required but was not found."
    echo ""
    echo "  Install it from:  https://www.python.org/downloads/"
    echo "  On macOS:         brew install python"
    echo "  On Ubuntu/Debian: sudo apt install python3"
    echo ""
    exit 1
fi

# ── Run installer ───────────────────────────────────────────────────────────
exec "$PYTHON" install.py
