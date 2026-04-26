# Spec: Install UX Hardening + Proxy Liveness Check

**Status:** Planned
**Priority:** High
**Depends on:** Current Go binary (`agentproxy`) on PATH after install

---

## Problem

Two gaps in the current user experience:

1. **Install leaves the cert un-trusted by default.** After `./install.sh` the user sees a block of manual `sudo` instructions. Most users skip them, then hit cryptic TLS errors when running agents through the proxy. The proxy should not be usable without the cert trusted.

2. **Wrapper scripts silently fail if the proxy isn't running.** Running `claudeproxy "explain this"` with no proxy up causes the agent to hang or emit confusing connection errors. There is no actionable message pointing the user to `agentproxy-start`.

---

## Goals

- First-time install completes with a trusted cert and a ready proxy — no manual steps required on a TTY.
- Uninstall removes both the local CA file and OS trust-store entry by default, so a normal uninstall fully reverses certificate installation.
- `agentproxy start` refuses to run if the cert file is missing.
- `claudeproxy` / `codexproxy` / `copilotproxy` fail fast with a clear, actionable message if the proxy is not listening.
- All behaviour is cross-platform: macOS, Linux, Windows (Git Bash / PowerShell).

---

## Features

### DEV-1 — `task prepare-local`: developer local bootstrap

This is a developer-only convenience path, not a user-facing install flow.

**Behaviour:**
1. Build the repo-local binary with `go build -o agentproxy ./cmd/agentproxy`.
2. Run `task ca-setup` so the CA cert is exported, runtime directories are ready, and the cert is trusted in the OS store.
3. Create `~/.local/bin` if it does not exist.
4. Refresh symlinks in `~/.local/bin`:
   - `agentproxy` -> repo-local `./agentproxy`
   - `claudeproxy` -> repo-local `./bin/claudeproxy`
   - `codexproxy` -> repo-local `./bin/codexproxy`
   - `copilotproxy` -> repo-local `./bin/copilotproxy`
   - `agentproxy-start` -> repo-local `./bin/agentproxy-start`
5. Do not remove or alter OS trust stores beyond the trust installation done by `task ca-setup`.

Developers use existing separate tasks for certificate and runtime setup:

```bash
task ca-setup       # export CA cert, create runtime dirs, and trust cert in the OS store only
task prepare-local  # build, run ca-setup, and refresh local command symlinks
task start          # run the proxy
```

### DEV-2 — `task ca-setup`: developer CA trust setup

This is a developer convenience wrapper around the existing CLI command.

**Behaviour:**
1. Run `./agentproxy ca-setup --no-trust-instructions` to export the CA cert and create runtime directories without printing manual instructions before the automatic trust step.
2. Check whether `~/.agentproxy/certs/agentproxy-ca-cert.pem` is already trusted in the OS trust store.
3. If already trusted, print `CA cert already trusted ✓` and exit successfully.
4. If not trusted, install it into the OS store:
   - macOS: `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain <cert>`
   - Linux Debian/Ubuntu: copy to `/usr/local/share/ca-certificates/agentproxy-ca.crt`, then run `sudo update-ca-certificates`
   - Linux RHEL/Fedora: copy to `/etc/pki/ca-trust/source/anchors/agentproxy-ca.crt`, then run `sudo update-ca-trust`
5. Keep `agentproxy ca-setup` itself as the lower-level CLI operation that exports the cert and prints manual trust instructions unless `--no-trust-instructions` is supplied.

### DEV-3 — `task dev`: Air-based local reload

This is a developer-only watch mode for iterating on the Go proxy locally.

**Behaviour:**
1. Run `task prepare-local` first so the binary, CA trust, runtime dirs, and wrapper symlinks are ready.
2. Start Air with `.air.toml`.
3. Air rebuilds `./agentproxy` with `go build -o agentproxy ./cmd/agentproxy`.
4. Air runs `bash bin/agentproxy-start --restart` as the dev process.
5. On watched file changes, Air sends interrupt to stop the running proxy before rebuilding/restarting.
6. Watch Go source and embedded/config files; exclude generated/runtime directories such as `certs/`, `logs/`, `tmp/`, `.git/`, and `third_party/`.
7. If `air` is not installed, the task may use `go run github.com/air-verse/air@latest -c .air.toml`.

### INSTALL-1 — `install.sh`: Auto-trust CA cert on TTY

**Current behaviour:** Prints trust instructions, requires the user to manually re-run with `--trust`.

**New behaviour:**
1. After `agentproxy ca-setup`, check whether the cert is already trusted in the OS store.
2. If **already trusted** → skip, print "CA cert already trusted ✓".
3. If **not trusted and running on a TTY** → attempt `sudo security add-trusted-cert` (macOS) or `sudo cp + sudo update-ca-certificates` (Linux) inline. The sudo prompt appears naturally in the terminal.
4. If **not trusted and NOT on a TTY** (CI/piped install) → print the manual command and continue without failing.
5. Remove the `--trust` flag — trust is now always attempted on interactive installs.

**macOS trust check:**
```bash
security find-certificate -c "AgentProxy" \
  /Library/Keychains/System.keychain >/dev/null 2>&1
```
Exit 0 = already trusted.

**Linux trust check:**
```bash
# Debian/Ubuntu
[ -f /usr/local/share/ca-certificates/agentproxy-ca.crt ]
# RHEL/Fedora
[ -f /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt ]
```

**TTY detection:**
```bash
[ -t 1 ]   # stdout is a terminal
```

---

### INSTALL-2 — `install.bat`: Auto-trust CA cert on Windows

**Current behaviour:** Prints `certutil -addstore` instructions only.

**New behaviour:**
1. After `ca-setup`, check with `certutil -verify <cert>` whether the cert is trusted.
2. If **already trusted** → skip, print "CA cert already trusted".
3. If **not trusted** → run `certutil -addstore "Root" <cert>`. Windows will show a UAC / Admin prompt. If it fails (no Admin rights), print the manual command.

---

### UNINSTALL-1 — Remove CA trust by default

**Current behaviour:** `./uninstall.sh` and `uninstall.bat` remove the local CA file/directory by default, but require `--trust` to remove the OS trust-store entry. This makes a normal uninstall incomplete after installers auto-trust the CA.

**New behaviour:**
1. `./uninstall.sh` removes OS CA trust by default, then removes `~/.agentproxy/certs/`.
2. `uninstall.bat` removes Windows Root-store trust by default, then removes `%USERPROFILE%\.agentproxy\certs`.
3. `--trust` remains accepted as a backward-compatible no-op alias for older documentation/scripts.
4. `--keep-trust` skips OS trust removal when a user intentionally wants the CA to remain trusted.
5. If the OS trust entry is already missing, uninstall prints an already-removed message and continues.

**macOS trust removal:**
```bash
sudo security delete-certificate -Z <sha1 fingerprint> /Library/Keychains/System.keychain
```

Use the fingerprint from `~/.agentproxy/certs/agentproxy-ca-cert.pem` when available. Fall back to the legacy GoProxy common name only if the cert file is unavailable.

**Linux trust removal:**
```bash
sudo rm -f /usr/local/share/ca-certificates/agentproxy-ca.crt
sudo rm -f /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt
sudo update-ca-certificates   # Debian/Ubuntu, when available
sudo update-ca-trust          # RHEL/Fedora, when available
```

**Windows trust removal:**
```bat
certutil -delstore "Root" "goproxy.github.io"
```

This matches the current exported GoProxy CA identity until AgentProxy moves to its own generated CA identity.

---

### STARTUP-1 — `agentproxy start`: Refuse if cert missing; warn if not OS-trusted

**New behaviour at startup:**

1. Check if `~/.agentproxy/certs/agentproxy-ca-cert.pem` exists.
   - If **missing** → print error and exit non-zero:
     ```
     ERROR: CA cert not found at ~/.agentproxy/certs/agentproxy-ca-cert.pem
            Run: agentproxy ca-setup
     ```
2. Check if the cert is trusted in the OS store (best-effort, platform-specific).
   - If **not trusted** → print warning and continue:
     ```
     WARNING: CA cert is not trusted by the OS trust store.
              Rust-based agents (e.g. codex) will fail TLS verification.
              To trust: sudo security add-trusted-cert -d -r trustRoot \
                          -k /Library/Keychains/System.keychain \
                          ~/.agentproxy/certs/agentproxy-ca-cert.pem
     ```
   - Node.js/Python agents use `SSL_CERT_FILE` so they will still work.

The cert-file check is a hard gate (exit 1). The OS-trust check is a soft warning (continue).

---

### WRAPPER-1 — Wrappers: Liveness check before exec

All three wrapper scripts (`bin/claudeproxy`, `bin/codexproxy`, `bin/copilotproxy`) add a liveness check before `exec`-ing the agent.

**Check logic:**
```bash
HOST="${AGENTPROXY_HOST:-127.0.0.1}"
PORT="${AGENTPROXY_PORT:-7717}"

if ! agentproxy status --host "$HOST" --port "$PORT" >/dev/null 2>&1; then
  echo "⚠️  AgentProxy is not running on ${HOST}:${PORT}"
  echo "   Start it with: agentproxy-start"
  exit 1
fi
```

Exit code 1 so CI pipelines also fail clearly.

---

### WRAPPER-2 — `agentproxy status` subcommand

New CLI subcommand. Checks whether the proxy is listening on the configured host/port.

**Usage:**
```
agentproxy status                         # uses config defaults
agentproxy status --host 127.0.0.1 --port 7717
```

**Behaviour:**
- Makes a TCP connection attempt to `host:port`.
- Exits **0** if listening, **1** if not.
- Prints a single line to stdout (suppressed with `-q`):
  - `AgentProxy is running on 127.0.0.1:7717` (exit 0)
  - `AgentProxy is not running on 127.0.0.1:7717` (exit 1)

**Implementation:** `net.DialTimeout("tcp", addr, 1*time.Second)` — no HTTP request, pure TCP. Works cross-platform.

---

### ENV-1 — Rename env vars: `PROXY_*` → `AGENTPROXY_*`

Rename for namespace consistency with the existing `AGENTPROXY_CA_CERT` var.

| Old | New | Default |
|-----|-----|---------|
| `PROXY_HOST` | `AGENTPROXY_HOST` | `127.0.0.1` |
| `PROXY_PORT` | `AGENTPROXY_PORT` | `7717` |
| `AGENTPROXY_CA_CERT` | `AGENTPROXY_CA_CERT` | `~/.agentproxy/certs/agentproxy-ca-cert.pem` |

Update all three wrapper scripts and `bin/agentproxy-start`.

---

## User Flow After These Changes

```
./install.sh
  → builds binary
  → runs agentproxy ca-setup      (cert written to ~/.agentproxy/certs/)
  → checks OS trust store
  → [TTY] sudo security add-trusted-cert ...   ← password prompt appears
  → installs binary + wrappers to ~/.local/bin
  → "AgentProxy installed. Start with: agentproxy-start"

./uninstall.sh
  → removes CA from OS trust store if present
  → removes ~/.agentproxy/certs/
  → removes local runtime cert cache and installed commands
  → leaves logs in place

agentproxy-start
  → checks cert file exists        (exits with error if not)
  → checks cert is OS-trusted      (warns if not)
  → starts proxy on 127.0.0.1:7717

claudeproxy "explain this codebase"
  → agentproxy status              (exits 1 if proxy not running → clear message)
  → sets HTTP_PROXY, CERT env vars
  → exec claude "$@"
```

---

## Out of Scope

- PAC file proxy trust chain validation
- Cert expiry checking
- Auto-restart of the proxy if it crashes
- `agentproxy status` returning JSON metrics (separate dashboard feature)
- Changing the CA identity away from the current GoProxy CA (separate security hardening item)
- Treating `task prepare-local` as a product installer

---

## Acceptance Criteria

- [ ] `task prepare-local`: builds the repo-local `agentproxy` binary
- [ ] `task prepare-local`: runs `task ca-setup`
- [ ] `task prepare-local`: refreshes `~/.local/bin` symlinks for `agentproxy`, `claudeproxy`, `codexproxy`, `copilotproxy`, and `agentproxy-start`
- [ ] `task prepare-local`: does not untrust or delete CA certificates
- [ ] `task ca-setup`: runs `./agentproxy ca-setup` and installs the CA cert into the OS trust store when not already trusted
- [ ] `task ca-setup`: exits successfully without reinstalling when the CA cert is already trusted
- [ ] `task dev`: runs `task prepare-local` before starting watch mode
- [ ] `task dev`: starts AgentProxy through `bash bin/agentproxy-start --restart`
- [ ] `task dev`: stops/restarts the running proxy on watched code changes
- [ ] `.air.toml`: excludes runtime/generated directories that would cause reload loops
- [ ] `./install.sh` on macOS TTY: cert trust attempted automatically; no manual step required
- [ ] `./install.sh` in CI (non-TTY): prints manual trust command, does not fail
- [ ] `install.bat` on Windows: `certutil -addstore` attempted; graceful fallback if no Admin
- [ ] `./uninstall.sh`: removes the matching CA cert from macOS System Keychain by default when present
- [ ] `./uninstall.sh`: removes Linux trust-store files and refreshes trust when available
- [ ] `uninstall.bat`: removes the matching CA cert from Windows Root store by default when present
- [ ] `--keep-trust`: skips OS trust removal but still removes local files and installed commands
- [ ] `agentproxy start` with no cert file: exits 1 with actionable error message
- [ ] `agentproxy start` with cert file but not OS-trusted: starts with warning
- [ ] `agentproxy status` exits 0 when proxy listening, 1 when not
- [ ] `claudeproxy` / `codexproxy` / `copilotproxy`: exit 1 with "not running" message when proxy is down
- [ ] `AGENTPROXY_HOST` / `AGENTPROXY_PORT` respected in wrappers and status check
- [ ] All existing `go test ./...` pass
- [ ] Tested on macOS, Linux (Ubuntu), Windows Git Bash
