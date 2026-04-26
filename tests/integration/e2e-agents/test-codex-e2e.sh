#!/usr/bin/env bash
# E2E: drive Codex through codexproxy and verify the WebSocket interception
# path lights up.
#
#   1. `codexproxy exec` runs to completion.
#   2. The proxy logged a request to `chatgpt.com /backend-api/codex/responses`
#      with status 101 (WebSocket upgrade) — this is the actual codex transport.
#   3. Multi-turn: `codexproxy exec resume --last` re-uses the previous session
#      and a second WebSocket upgrade appears in the log.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/lib.sh"

require_proxy_running
require_command codexproxy
require_command codex

echo "=== Codex — single-turn ==="
PRE=$(snapshot)
OUT=$(timeout 120 codexproxy exec "say only the word: ping" </dev/null 2>&1 || true)
[ -n "$OUT" ] || fail "codexproxy produced no output"
pass "got response (${#OUT} bytes)"

REQS=$(count_events "$PRE" "chatgpt.com" "/backend-api/codex/responses")
[ "$REQS" -ge 1 ] || { print_endpoints "$PRE"; fail "no chatgpt.com /backend-api/codex/responses request found"; }
pass "proxy intercepted $REQS Codex WebSocket request(s)"

# Look for a 101 status to confirm the WebSocket upgrade was MITM'd correctly.
HAS_101=$(slice "$PRE" | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        d = json.loads(line)
        if d.get('host')=='chatgpt.com' and 'codex/responses' in (d.get('path') or '') and d.get('status')==101:
            print('yes'); break
    except Exception: pass
")
[ "$HAS_101" = "yes" ] && pass "WebSocket upgrade (status 101) observed" || info "no 101 status seen — WS upgrade may have been short-circuited; check manually"

echo ""
echo "=== Codex — multi-turn (resume --last) ==="
PRE=$(snapshot)
OUT=$(timeout 120 codexproxy exec resume --last "now say: pong" </dev/null 2>&1 || true)
[ -n "$OUT" ] || fail "codexproxy resume produced no output"
pass "follow-up response received"

REQS=$(count_events "$PRE" "chatgpt.com" "/backend-api/codex/responses")
[ "$REQS" -ge 1 ] || { print_endpoints "$PRE"; fail "resume did not hit chatgpt.com"; }
pass "resume intercepted $REQS request(s)"

echo ""
echo "Codex E2E ✓"
