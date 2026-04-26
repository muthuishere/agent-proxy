#!/usr/bin/env bash
# Non-disruption integration test for AgentProxy.
#
# Verifies the invariant from CLAUDE.md: with the proxy running and its CA
# trusted by the OS, unrelated apps that do NOT route through the proxy must
# still reach unrelated HTTPS hosts without TLS errors. This guards against
# regressions where the trusted CA or the conditional CONNECT MITM logic
# accidentally breaks general internet connectivity.
#
# Pre-conditions (not started by this script):
#   - AgentProxy is running on 127.0.0.1:7717 (`task start` in another terminal).
#   - The AgentProxy CA is installed and trusted in the OS trust store.
#
# Each curl below runs in a fresh subshell with HTTP_PROXY/HTTPS_PROXY
# explicitly unset, so we are testing what an unrelated app on the same
# machine would see -- not traffic going through the proxy.

set -euo pipefail

PASS=0
FAIL=0

pass() {
  echo "PASS: $1"
  PASS=$((PASS + 1))
}

fail() {
  echo "FAIL: $1"
  FAIL=$((FAIL + 1))
}

# 1. Pre-condition: proxy must be running.
if ! command -v agentproxy >/dev/null 2>&1; then
  echo "ERROR: 'agentproxy' binary not found on PATH. Run 'task prepare-local' or 'task install' first."
  exit 1
fi

if ! agentproxy status -q >/dev/null 2>&1; then
  echo "ERROR: AgentProxy is not running on 127.0.0.1:7717."
  echo "Start it in another terminal with 'task start' or 'agentproxy-start' and re-run this test."
  exit 1
fi

echo "Pre-condition OK: AgentProxy is running."
echo

# Helper: run curl in a fresh subshell with no proxy env vars.
# Args: <label> <url> [--allow-non-2xx]
run_curl_check() {
  local label="$1"
  local url="$2"
  local allow_non_2xx="${3:-}"

  local output
  local exit_code=0

  if [[ "$allow_non_2xx" == "--allow-non-2xx" ]]; then
    # We only care that TLS works; any HTTP status is acceptable.
    output=$(
      bash -c '
        unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy ALL_PROXY all_proxy
        curl -sS -o /dev/null -w "%{http_code}" --max-time 15 "$0"
      ' "$url" 2>&1
    ) || exit_code=$?

    if [[ $exit_code -ne 0 ]]; then
      fail "$label -- curl error (likely TLS): $output"
      return
    fi
    pass "$label -- HTTP $output (TLS OK)"
  else
    # Expect 2xx; -fsS makes curl exit non-zero on >=400.
    output=$(
      bash -c '
        unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy ALL_PROXY all_proxy
        curl -fsS -o /dev/null -w "%{http_code}" --max-time 15 "$0"
      ' "$url" 2>&1
    ) || exit_code=$?

    if [[ $exit_code -ne 0 ]]; then
      fail "$label -- curl failed: $output"
      return
    fi
    if [[ "$output" != 2* ]]; then
      fail "$label -- expected 2xx, got $output"
      return
    fi
    pass "$label -- HTTP $output"
  fi
}

# 2. Google APIs discovery -- expect 200.
run_curl_check "googleapis.com discovery" "https://www.googleapis.com/discovery/v1/apis"

# 3. Razorpay API root -- expect any HTTP response (no TLS error).
run_curl_check "api.razorpay.com" "https://api.razorpay.com/" --allow-non-2xx

# 4. Firebase API root -- expect any HTTP response (no TLS error).
run_curl_check "firebase.googleapis.com" "https://firebase.googleapis.com/" --allow-non-2xx

echo
echo "Summary: $PASS passed, $FAIL failed."

if [[ $FAIL -gt 0 ]]; then
  exit 1
fi
exit 0
