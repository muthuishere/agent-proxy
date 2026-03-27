#!/usr/bin/env bash
set -euo pipefail

# ── AgentProxy uninstaller for macOS and Linux ────────────────────────────
# Usage: ./uninstall.sh

cd "$(dirname "${BASH_SOURCE[0]}")"

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
    echo "  ERROR: Python 3.11+ is required."
    exit 1
fi

exec "$PYTHON" install.py uninstall
