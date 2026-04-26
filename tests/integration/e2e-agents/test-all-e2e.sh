#!/usr/bin/env bash
# Drive all three agents end-to-end against a running proxy.
#
# This is the closest thing to "test the actual product as a user would". It
# burns real API quota on Anthropic / OpenAI / GitHub Copilot. Run on demand,
# not in CI.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

bash "$SCRIPT_DIR/test-claude-e2e.sh"
echo ""
bash "$SCRIPT_DIR/test-codex-e2e.sh"
echo ""
bash "$SCRIPT_DIR/test-copilot-e2e.sh"
echo ""
echo "All three agent E2E tests passed ✓"
