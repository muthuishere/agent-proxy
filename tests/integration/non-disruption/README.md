# Non-Disruption Integration Test

## What this tests

The **non-disruption invariant** from `CLAUDE.md` and `docs/specs/spec-slice-04-claude-end-to-end.md`:

> When AgentProxy is running and its CA is trusted by the OS, unrelated apps
> on the same machine — apps that do **not** set `HTTP_PROXY` to point at
> AgentProxy — must continue to reach unrelated HTTPS hosts without TLS errors
> or MITM interference.

The script `test-non-disruption.sh` runs three `curl` checks from a fresh
subshell with `HTTP_PROXY`/`HTTPS_PROXY` explicitly unset:

1. `https://www.googleapis.com/discovery/v1/apis` — expects HTTP 200.
2. `https://api.razorpay.com/` — expects any HTTP response (no TLS error).
3. `https://firebase.googleapis.com/` — expects any HTTP response (no TLS error).

The Razorpay and Firebase checks accept non-2xx status codes; we only care
that the TLS chain validates. Razorpay and Firebase were the hosts flagged
in the 2026-04-08 huddle as broken when CONNECT MITM was unconditional.

## Why it isn't in CI

This test requires a **real OS-trusted CA** and a **running proxy** on
`127.0.0.1:7717`. Both are heavy preconditions:

- Trusting a CA on the CI host modifies the system trust store.
- The proxy must be started and reachable for the duration of the test.
- The targets are real third-party HTTPS endpoints; CI flakiness against
  them is not a useful signal.

The unit-level guard for the conditional CONNECT MITM behavior already lives
in `internal/proxy/server_test.go::TestConditionalConnectMITM` and runs in CI.
This script is the integration-level smoke test for human-driven verification.

## How to run locally

In one terminal, start the proxy:

```bash
task start
```

In another terminal (so it inherits no proxy env vars from the first one):

```bash
task test-non-disruption
```

The script prints `PASS:` / `FAIL:` lines per check and exits non-zero if any
check fails. If the proxy isn't running, it exits with a clear message
pointing you to `task start`.
