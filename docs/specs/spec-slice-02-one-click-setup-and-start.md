# Slice 02 — One-Click Setup and Start

**Status:** Proposed
**Owner:** _tbd_
**Depends on:** Slice 01 (binary distribution)
**Demo:** After install, a user runs *one command* (`agentproxy setup`) which trusts the CA, starts the proxy and dashboard, and prints (and optionally opens) the dashboard URL.

---

## Why

Even after install, today's flow is multi-step and confusing:

1. `agentproxy ca-setup` (export cert)
2. Manual `sudo` command from printed instructions (trust cert)
3. `agentproxy-start` in a separate terminal (start proxy)
4. User has to know the dashboard runs on port 7718 and open it manually

We want a single command (`agentproxy setup`) that handles steps 1-3 idempotently, prints the dashboard URL prominently, and is safe to re-run. Plus a one-line `agentproxy start` that always tells the user where the dashboard is.

---

## What Changes

- New `agentproxy setup` command in `cmd/agentproxy/`:
  - Runs `ca-setup` (export cert).
  - Trusts cert in OS root store (re-execs under `sudo` / UAC if needed; idempotent — skips if already trusted).
  - Starts the proxy + dashboard as a backgrounded process (or instructs user to run `agentproxy start` if `--no-start` passed).
  - Prints a clear summary: proxy URL, dashboard URL, log file path, next-step commands.
- Existing `agentproxy start` always logs the dashboard URL on a single dedicated line right after startup, e.g.:
  ```
  AgentProxy started — proxy: http://127.0.0.1:7717   dashboard: http://127.0.0.1:7718
  ```
- New `--open` flag on both commands: opens the dashboard in the default browser (`open` on macOS, `xdg-open` on Linux, `start` on Windows).
- New `agentproxy status` already exists; extend to print the dashboard URL when running.

---

## Impact

- **Affected:** `cmd/agentproxy/` (new `setup.go`), `cmd/agentproxy/start.go` (URL banner), `cmd/agentproxy/status.go` (dashboard URL), `internal/proxy/server.go` (no change expected).
- **Not affected:** masking pipeline, providers, configs.
- **Backward compatible:** existing `ca-setup` and `start` keep working.

---

## Tasks

1. ~~Add `cmd/agentproxy/setup.go` implementing the orchestration: ca-setup → trust → start (or print start instructions).~~ **DONE 2026-04-25** — `runSetup` exposes `agentproxy setup [--start] [--open] [--no-trust]`.
2. ~~Trust subcommand: detect platform, call `security add-trusted-cert` / `update-ca-certificates` / `update-ca-trust` / `certutil -addstore Root` as appropriate.~~ **DONE 2026-04-25** — `trustCAInOSStore` shells out via `runMaybeSudo`, which prepends `sudo` only when not already root.
3. ~~Idempotency: before trusting, check whether the cert is already in the OS store~~ **DONE 2026-04-25** — uses existing `isCACertTrusted`; setup short-circuits with "CA already trusted ✓".
4. ~~Update `start.go` to print a single banner line with both URLs, formatted for easy eyeballing.~~ **DONE 2026-04-25** — `runStart` now prints `AgentProxy ready — proxy: http://HOST:PORT   dashboard: http://127.0.0.1:UIPORT` after the per-line prints (kept for grep compatibility).
5. ~~Add `--open` flag wired to the platform's default-browser opener.~~ **DONE 2026-04-25** — `openURL()` covers macOS/Linux/Windows.
6. ~~Update `status.go` to include the dashboard URL when the proxy is running.~~ **DONE 2026-04-25** — `runStatus` prints `dashboard: http://127.0.0.1:7718` in the same format as `start`.
7. ~~Update README "Quick Start" to be exactly: install → `agentproxy setup --open` → done.~~ **DONE 2026-04-25** — Quick Start reduced to `task install` + `agentproxy setup --open`.
8. ~~Add an integration test that runs `agentproxy setup --no-start` and asserts the cert ends up trusted (gated by `INTEGRATION_TRUST=1`).~~ **DONE 2026-04-25** — `cmd/agentproxy/setup_integration_test.go` runs `runSetup --no-trust` under `INTEGRATION_TRUST=1`; full auto-trust is skipped because it needs sudo/UAC and is not feasible in CI.

---

## Done When

- [ ] `agentproxy setup` on a fresh machine: trusts CA, starts proxy + dashboard, prints both URLs, and (with `--open`) opens the dashboard.
- [ ] Re-running `agentproxy setup` is a no-op aside from "already trusted ✓ / already running ✓" messages.
- [ ] `agentproxy start` always prints `dashboard: http://…:7718` within the first 5 lines of output.
- [ ] `agentproxy status` prints the dashboard URL when running.
- [ ] On Windows, `setup` triggers UAC once (for trust) and never again on subsequent runs.
- [ ] README Quick Start is reduced to two commands: install + `agentproxy setup --open`.

---

## Team Review — 2026-04-25

### Verified state of the codebase

- **No `agentproxy setup` command yet** — only `start`, `status`, `ca-setup`, `validate`, `config`, `version`. Building it is greenfield.
- **CA trust idempotency primitives already exist** in `internal/casetup/certsupport.go` (or equivalent — `isCACertTrusted` with platform branches: `security find-certificate -Z` on macOS, file-existence on Linux, `certutil -verify` on Windows). The trust *execution* is shell-script-only today (`scripts/trust-ca.sh`); not callable from the Go binary.
- **Dashboard URL banner is already partly done** — `cmd/agentproxy/main.go` (around line 118) prints `dashboard: http://127.0.0.1:7718` on start. Slice 02's "URL banner" task is *minor cleanup*, not new work.
- **`status` does not print the dashboard URL** — confirmed missing.
- **No browser opener exists** — straightforward to add via `os/exec` + platform switch (`open` / `xdg-open` / `start`); no new dependency needed.

### P0 — Spec corrections

- Move task "ca-setup auto-trust integration" up: this is the *real* work of slice 02. The new `setup` command must call the existing `isCACertTrusted` checks and shell out to platform trust commands itself (don't depend on `scripts/trust-ca.sh` — it must work post-npm-install where the script may not be on disk).
- Privilege escalation: macOS/Linux re-exec under `sudo`; Windows must use `runas` / UAC manifest, not just exit-with-instructions, or the "one click" promise fails on Windows.

### Tasks added

9. **P0** Refactor `scripts/trust-ca.sh` logic into Go in `internal/casetup/trust.go` so the Go CLI can trust without an external script.
10. **P0** Implement self-elevation: on Unix, re-exec under `sudo` with original args + a flag to prevent recursion; on Windows, use a UAC manifest or shell out via PowerShell `Start-Process -Verb RunAs`.
11. **P1** Have `setup` skip the trust step entirely (with a printed warning) if running non-interactive (CI, no TTY) — guard against `sudo` hanging.
12. **P1** Confirm dashboard URL banner format is greppable (e.g. machine-readable line `AGENTPROXY_DASHBOARD_URL=http://127.0.0.1:7718`) so postinstall scripts can extract it.
