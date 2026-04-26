#!/usr/bin/env bash
# Shared helpers for the e2e-agents test harness.
#
# Conventions:
#   - The proxy must already be running. We don't start it (could clash with
#     the user's running instance).
#   - We snapshot the line count of logs/traffic.jsonl before each prompt and
#     diff it after to find the new events the wrapper produced.
#   - Each test prints PASS:/FAIL: lines and exits non-zero on first FAIL.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TRAFFIC_LOG="${TRAFFIC_LOG:-$REPO_ROOT/logs/traffic.jsonl}"
AGENTPROXY_BIN="${AGENTPROXY_BIN:-$REPO_ROOT/agentproxy}"

CLR_RED='\033[0;31m'
CLR_GREEN='\033[0;32m'
CLR_DIM='\033[2m'
CLR_OFF='\033[0m'

pass() { printf "${CLR_GREEN}PASS${CLR_OFF}: %s\n" "$1"; }
fail() { printf "${CLR_RED}FAIL${CLR_OFF}: %s\n" "$1"; exit 1; }
info() { printf "${CLR_DIM}      %s${CLR_OFF}\n" "$1"; }

require_proxy_running() {
  if ! "$AGENTPROXY_BIN" status -q >/dev/null 2>&1; then
    echo "AgentProxy is not running on 127.0.0.1:7717."
    echo "Start it in another terminal first:  task start"
    exit 1
  fi
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "command not on PATH: $1"
}

# snapshot returns the current line count of the traffic log.
snapshot() { wc -l < "$TRAFFIC_LOG" 2>/dev/null | tr -d ' '; }

# slice returns lines from `pre+1` through end-of-file (the events appended
# during the most recent prompt).
slice() {
  local pre="$1"
  tail -n +"$((pre + 1))" "$TRAFFIC_LOG"
}

# count_events filters the slice for events to a specific host/path and
# returns the count.
count_events() {
  local pre="$1" host_substr="$2" path_substr="$3"
  slice "$pre" | python3 -c "
import sys, json
hs, ps = sys.argv[1], sys.argv[2]
n = 0
for line in sys.stdin:
    try:
        d = json.loads(line)
        if hs in (d.get('host') or '') and ps in (d.get('path') or ''):
            n += 1
    except Exception:
        pass
print(n)
" "$host_substr" "$path_substr"
}

# print_endpoints summarises every event in the slice.
print_endpoints() {
  local pre="$1"
  slice "$pre" | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        d = json.loads(line)
        print(f\"  {d.get('event','?')} {d.get('host','')} {d.get('path','')} status={d.get('status','-')} masked={d.get('masked_count','-')}\")
    except Exception:
        pass
" | head -10
}
