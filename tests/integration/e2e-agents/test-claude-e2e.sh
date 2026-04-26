#!/usr/bin/env bash
# E2E: drive Claude through claudeproxy end-to-end and verify
#
#   1. Single-turn prompt via `claudeproxy -p` returns a non-empty response.
#   2. The proxy logged a request to `api.anthropic.com /v1/messages`.
#   3. A follow-up turn (`claudeproxy --continue -p ...`) hits the same
#      endpoint a second time — proves multi-turn flows through the proxy.
#
# Requires:
#   - agentproxy running and the CA trusted in the OS store.
#   - `claude` CLI installed and authenticated.
#
# Usage:  task test-e2e-claude     (or run this script directly)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/lib.sh"

require_proxy_running
require_command claudeproxy
require_command claude

echo "=== Claude — single-turn ==="
PRE=$(snapshot)
OUT=$(timeout 60 claudeproxy -p "say only the word: ping" </dev/null 2>&1 || true)
[ -n "$OUT" ] || fail "claudeproxy produced no output"
pass "got response (${#OUT} bytes)"

REQS=$(count_events "$PRE" "api.anthropic.com" "/v1/messages")
[ "$REQS" -ge 1 ] || { print_endpoints "$PRE"; fail "no api.anthropic.com /v1/messages request found in proxy log"; }
pass "proxy intercepted $REQS request(s) to api.anthropic.com /v1/messages"

echo ""
echo "=== Claude — multi-turn (continue) ==="
PRE=$(snapshot)
OUT=$(timeout 60 claudeproxy --continue -p "now say: pong" </dev/null 2>&1 || true)
[ -n "$OUT" ] || fail "claudeproxy --continue produced no output"
pass "follow-up response received"

REQS=$(count_events "$PRE" "api.anthropic.com" "/v1/messages")
[ "$REQS" -ge 1 ] || { print_endpoints "$PRE"; fail "follow-up did not hit api.anthropic.com"; }
pass "follow-up intercepted $REQS request(s)"

echo ""
echo "Claude E2E ✓"
