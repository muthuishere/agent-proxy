#!/usr/bin/env bash
# E2E: drive Copilot CLI through copilotproxy.
#
#   1. `copilotproxy --allow-all-tools -p` runs to completion.
#   2. The proxy logs a request to `api.individual.githubcopilot.com /responses`
#      (the actual chat endpoint) — and an SSE response.
#   3. The previously-flagged regression — `api.github.com` being MITM'd —
#      MUST NOT appear in the proxy log because we explicitly excluded it.
#   4. Multi-turn: a second `copilotproxy -p` call re-hits the same endpoint.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/lib.sh"

require_proxy_running
require_command copilotproxy
require_command copilot

echo "=== Copilot — single-turn ==="
PRE=$(snapshot)
OUT=$(timeout 120 copilotproxy --allow-all-tools -p "say only the word: ping" </dev/null 2>&1 || true)
[ -n "$OUT" ] || fail "copilotproxy produced no output"
pass "got response (${#OUT} bytes)"

REQS=$(count_events "$PRE" "githubcopilot.com" "/responses")
[ "$REQS" -ge 1 ] || { print_endpoints "$PRE"; fail "no githubcopilot.com /responses request found"; }
pass "proxy intercepted $REQS Copilot request(s)"

# Critical invariant: api.github.com must NOT be MITM'd.
GH_HITS=$(count_events "$PRE" "api.github.com" "")
if [ "$GH_HITS" -gt 0 ]; then
  print_endpoints "$PRE"
  fail "api.github.com was intercepted ($GH_HITS events) — overbroad scope regression!"
fi
pass "api.github.com correctly NOT in intercepted scope (0 events)"

echo ""
echo "=== Copilot — multi-turn (second invocation) ==="
PRE=$(snapshot)
OUT=$(timeout 120 copilotproxy --allow-all-tools -p "now say: pong" </dev/null 2>&1 || true)
[ -n "$OUT" ] || fail "second copilotproxy invocation produced no output"
pass "follow-up response received"

REQS=$(count_events "$PRE" "githubcopilot.com" "/responses")
[ "$REQS" -ge 1 ] || { print_endpoints "$PRE"; fail "follow-up did not hit githubcopilot.com"; }
pass "follow-up intercepted $REQS request(s)"

echo ""
echo "Copilot E2E ✓"
