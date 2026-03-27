# AgentProxy: Python → Go Migration Task Plan

**Project:** AgentProxy — MITM proxy for AI agent secret masking
**Migration Target:** Python 3.11 + mitmproxy → Go (single static binary)
**Prepared for:** Engineering Team
**Date:** 2026-03-27

---

## Executive Summary

Replace the Python/mitmproxy stack with a Go-based static binary that:
- Intercepts HTTPS traffic via MITM (cert generation + system trust)
- Masks secrets before they reach AI APIs (Anthropic, OpenAI, Copilot)
- Ships as a single cross-platform binary (no Python/uv runtime dependency)
- Handles high concurrency via goroutines (10–20x throughput improvement)
- Self-installs: generates CA cert, trusts it in OS keychain, patches bat/shell env files

---

## Dependency Graph Overview

```
Phase 1: Core Proxy (parallel tracks A + B)
    │
    ├── Track A: Proxy Engine + TLS
    └── Track B: Secret Scanner + Vault

Phase 2: Installer (depends on Phase 1 binary exists)
    │
    ├── Track C: Cert generation + OS trust
    └── Track D: Bat/Shell file patching

Phase 3: Feature Parity + Integration (depends on Phase 1 + 2)
    │
    ├── Track E: Upstream proxy + PAC
    ├── Track F: Streaming / SSE handling (hardest — prototype first)
    └── Track G: Config, Logging, CLI

Phase 4: Testing + Hardening (depends on Phase 3)
    │
    ├── Track H: Unit + integration tests
    └── Track I: E2E tests + Python parity validation
```

---

## What is STATIC (must be done sequentially)

These tasks have hard dependencies and cannot be parallelized:

| Order | Task | Depends On |
|---|---|---|
| 1 | Repo scaffold: Go module, project layout, CI skeleton | — |
| 2 | CA cert generation (rcgen equivalent in Go) | Repo scaffold |
| 3 | Per-host dynamic cert signing | CA cert generation |
| 4 | CONNECT tunnel + TLS interception working end-to-end | Per-host cert signing |
| 5 | Request/response hook interface defined | TLS interception |
| 6 | Secret scanner wired into hook interface | Hook interface + scanner (Track B) |
| 7 | Vault restore wired into response hook | Scanner wired in |
| 8 | Installer can produce a working binary | All Phase 1 done |
| 9 | OS cert trust (install step) | CA cert generation |
| 10 | E2E tests against real proxy | Full Phase 1 + 2 + 3 |

---

## What is PARALLEL (can run simultaneously)

| Parallel Group | Tasks |
|---|---|
| **Group 1** (start immediately) | Track A (proxy engine) AND Track B (scanner/vault) |
| **Group 2** (after scaffold) | Track C (cert trust) AND Track D (bat patching) |
| **Group 3** (after Phase 1 complete) | Track E (upstream proxy) AND Track F (SSE prototype) AND Track G (config/logging) |
| **Group 4** (after Phase 3) | Track H (unit tests) AND Track I (E2E tests) |

---

## Phase 1 — Core Proxy Engine

**Goal:** Working HTTPS MITM proxy that hooks requests and responses.
**Duration:** 2–3 weeks
**Owner:** 2 engineers (Track A + Track B in parallel)

---

### Track A — Proxy Engine + TLS  _(can start Day 1)_

#### TASK-A1: Repository Scaffold
- Initialize Go module (`go mod init`)
- Directory layout:
  ```
  cmd/agentproxy/         ← main entrypoint (start / install / uninstall subcommands)
  internal/proxy/         ← MITM proxy engine
  internal/scanner/       ← secret detection
  internal/vault/         ← mask/restore store
  internal/decoder/       ← gzip/base64/hex decode
  internal/installer/     ← cert + bat patching
  internal/logger/        ← JSONL logger
  internal/upstream/      ← upstream proxy resolver
  config/                 ← patterns.yaml, pii_patterns.yaml, agentproxy.yaml
  tests/                  ← unit + integration tests
  ```
- Add `Makefile` or `Taskfile.yml` targets: `build`, `test`, `lint`, `cross-compile`
- Set up GitHub Actions CI: build matrix (linux/mac/windows, amd64/arm64)
- **Static dependency:** all other tasks depend on this

#### TASK-A2: CA Certificate Generation
- Use `crypto/x509` + `crypto/rsa` (stdlib) to generate a local CA keypair
- Store at `~/.agentproxy/ca-cert.pem` + `~/.agentproxy/ca-key.pem`
- Cert fields: `CN=AgentProxy CA`, `IsCA=true`, `KeyUsageCertSign`, 10yr validity
- Regenerate only if missing or expired
- **Note:** this replaces the mitmproxy auto-generated cert

#### TASK-A3: Per-Host Dynamic Certificate Signing
- On each CONNECT request, generate a leaf cert for the target hostname
- Sign with the CA from TASK-A2
- Cache signed certs in-memory (`sync.Map`, keyed by hostname) — avoid re-signing on every connection
- Cache TTL: 1 hour (certs valid 24h)
- **Depends on:** TASK-A2

#### TASK-A4: HTTPS MITM CONNECT Tunnel
- Implement HTTP CONNECT handler using `net/http` + `crypto/tls`
- On CONNECT: complete TCP handshake with client using per-host cert, then open upstream TLS to real server
- Library choice: `elazarl/goproxy` (recommended) OR custom `net.Listener` approach
- Handle both HTTP/1.1 and HTTP/2 (ALPN negotiation)
- **Depends on:** TASK-A3
- **Risk:** WebSocket + SSE upgrades — see Track F

#### TASK-A5: Request/Response Hook Interface
- Define Go interface that all interceptors implement:
  ```go
  type RequestHook interface {
      HandleRequest(req *http.Request) (*http.Request, error)
  }
  type ResponseHook interface {
      HandleResponse(resp *http.Response, req *http.Request) (*http.Response, error)
  }
  ```
- Wire hooks into the CONNECT tunnel
- Domain allowlist check here (skip non-intercepted domains)
- **Depends on:** TASK-A4

---

### Track B — Secret Scanner + Vault  _(can start Day 1, parallel to Track A)_

#### TASK-B1: Vault (Mask/Restore Store)
- Port `vault.py` → `internal/vault/vault.go`
- Thread-safe store: `sync.RWMutex` + `map[string]string`
- Token format preserved: `{prefix}{stars}{suffix}[{name}:{digest8}]`
- Methods: `Mask(original, name, prefix, suffix) string`, `Restore(body string) string`
- SHA256 digest generation for token uniqueness
- **No dependencies — can start immediately**

#### TASK-B2: Secret Scanner
- Port `scanner.py` → `internal/scanner/scanner.go`
- Load 22 patterns from `patterns.yaml` at startup
- Use `regexp` package (RE2 semantics, no backtracking — safe for untrusted input)
- Parallel scanning: launch one goroutine per pattern, collect matches via channel
- Return `[]Match{Name, Value, Start, End}`
- **No dependencies — can start immediately**

#### TASK-B3: PII Scanner (optional, off by default)
- Port `pii_patterns.yaml` patterns (email, phone, SSN, credit card, IP)
- Same interface as TASK-B2
- Gated by `pii.enabled` config flag
- **Depends on:** TASK-B2 interface finalized

#### TASK-B4: Body Decoder (gzip / base64 / hex / URL)
- Port `decoder.py` → `internal/decoder/decoder.go`
- Decompress: `compress/gzip`
- Decode blobs: `encoding/base64`, `encoding/hex`, `net/url`
- Recursive blob scanning up to `MAX_DECODE_DEPTH=4`
- Return decoded body + list of encoded segments that need re-encoding after masking
- **Depends on:** TASK-B2 (scanner called per decoded layer)

---

## Phase 2 — Installer

**Goal:** Single binary that self-installs: cert trust + env/bat patching.
**Duration:** 1–2 weeks
**Owner:** 1–2 engineers (Track C + Track D in parallel)
**Depends on:** Phase 1 binary buildable (TASK-A1 done, basic scaffold working)

---

### Track C — Certificate Trust  _(parallel with Track D)_

#### TASK-C1: Cert Trust — macOS
- Shell out to `security add-trusted-cert -d -r trustRoot -k ~/Library/Keychains/login.keychain-db <cert>`
- Detect if already trusted (parse `security find-certificate` output) to make idempotent
- Uninstall: `security delete-certificate -c "AgentProxy CA"`

#### TASK-C2: Cert Trust — Windows
- Shell out to `certutil -addstore -user Root <cert>`
- Idempotency: `certutil -verifystore -user Root "AgentProxy CA"`
- Uninstall: `certutil -delstore -user Root "AgentProxy CA"`

#### TASK-C3: Cert Trust — Linux (distro-aware)
- Detect distro from `/etc/os-release`
- Ubuntu/Debian: copy to `/usr/local/share/ca-certificates/`, run `update-ca-certificates`
- RHEL/CentOS: copy to `/etc/pki/ca-trust/source/anchors/`, run `update-ca-trust extract`
- Arch: copy to `/etc/ca-certificates/trust-source/anchors/`, run `trust extract-compat`
- Uninstall: remove file + re-run update command

#### TASK-C4: Runtime Env Var Injection
- Write wrapper scripts (or document) that set:
  - `NODE_EXTRA_CA_CERTS`
  - `SSL_CERT_FILE`
  - `REQUESTS_CA_BUNDLE`
  - `CURL_CA_BUNDLE`
- All pointing to `~/.agentproxy/ca-cert.pem`
- On Windows: wrapper `.cmd` files; on Unix: wrapper shell scripts

---

### Track D — Bat / Shell File Patching  _(parallel with Track C)_

#### TASK-D1: Bat File Patcher (Windows)
- Locate target `.bat` / `.cmd` files (configurable list + auto-discovery in PATH)
- Inject proxy env vars:
  ```bat
  set HTTP_PROXY=http://127.0.0.1:7717
  set HTTPS_PROXY=http://127.0.0.1:7717
  set NODE_EXTRA_CA_CERTS=%USERPROFILE%\.agentproxy\ca-cert.pem
  ```
- **Always backup original** to `<file>.agentproxy.bak` before patching
- Idempotent: detect if already patched (check for marker comment `REM agentproxy-patched`)
- Uninstall: restore from `.bak` file

#### TASK-D2: Shell Script Patcher (Unix)
- Same approach for `.sh` wrapper scripts and shell RC files (`~/.bashrc`, `~/.zshrc`)
- Inject block between `# agentproxy-start` and `# agentproxy-end` markers
- Uninstall: remove marker block

#### TASK-D3: Installer CLI Subcommands
- `agentproxy install` — runs TASK-C1/C2/C3 + TASK-D1/D2 + writes config
- `agentproxy uninstall` — reverses all changes
- `agentproxy status` — show cert trust status, proxy running status, patched files list
- All subcommands must be idempotent and safe to re-run
- **Depends on:** TASK-C1/C2/C3 and TASK-D1/D2

---

## Phase 3 — Feature Parity

**Goal:** Match all features of the Python version.
**Duration:** 2–3 weeks
**Owner:** 2–3 engineers (Tracks E + F + G in parallel)
**Depends on:** Phase 1 complete

---

### Track E — Upstream Proxy + PAC  _(parallel with F and G)_

#### TASK-E1: Env-Based Upstream Proxy
- Read `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` from environment
- Apply to all outbound connections from proxy
- Support `username:password@host:port` format

#### TASK-E2: OS System Proxy Detection
- macOS: `scutil --proxy` output parsing
- Windows: registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
- Linux: env vars only

#### TASK-E3: PAC File Support
- Library: `jackwakefield/gopac` or custom JS evaluator
- Load PAC from file path or HTTP URL
- Per-request: call `FindProxyForURL(url, host)` → parse result
- Cache PAC fetch (5 min TTL)
- Gate behind `upstream_proxy.pac_file` config

---

### Track F — Streaming / SSE Handling  _(PROTOTYPE FIRST — highest risk)_

> **Warning:** This is the hardest part of the migration. SSE/chunked streaming is the primary response mode for all AI APIs. Prototype before committing to approach.

#### TASK-F1: Prototype — SSE Secret Masking (spike, 3–5 days)
- Build standalone test: Go proxy intercepting a chunked SSE response from a local mock server
- Test: does vault token restoration work when a secret spans two chunks?
- Test: does the proxy buffer, scan, and re-stream chunks with acceptable latency?
- **Decision point:** buffer-and-scan (higher latency) vs pass-through-and-scan (miss cross-chunk secrets)
- Recommended: buffer each SSE `data:` event line, scan and restore, then forward

#### TASK-F2: Chunked Transfer + SSE Implementation
- Based on prototype findings, implement production SSE handling
- Handle `Transfer-Encoding: chunked` + `Content-Type: text/event-stream`
- Per-event buffering: accumulate until `\n\n` SSE delimiter, scan, restore, forward
- Preserve SSE event structure (`event:`, `data:`, `id:` fields)
- **Depends on:** TASK-F1 prototype decision

#### TASK-F3: WebSocket Support
- On `Upgrade: websocket` header, switch to WebSocket frame handling
- Scan each frame payload for secrets (text frames only)
- Port `websocket_message()` hook logic from `addon.py`

---

### Track G — Config, Logging, CLI  _(parallel with E and F)_

#### TASK-G1: Config Loader
- Parse `agentproxy.yaml` using `gopkg.in/yaml.v3`
- Same schema as Python version (no breaking changes for existing users)
- Load `.env` file via `godotenv`
- Config search order: `./agentproxy.yaml` → `~/.agentproxy/agentproxy.yaml` → defaults

#### TASK-G2: JSONL Logger
- Port `logger.py` → `internal/logger/logger.go`
- Event types: `request`, `response`, `websocket`, `passthrough`
- Async write via buffered channel → single writer goroutine (avoids lock contention)
- Rotate log file at 100MB
- Respect `log_originals: false` — never log pre-masked values

#### TASK-G3: CLI Entrypoint
- `agentproxy start [--config path] [--port 7717]`
- `agentproxy install`
- `agentproxy uninstall`
- `agentproxy status`
- `agentproxy version`
- Use `cobra` or `urfave/cli` for subcommand parsing

---

## Phase 4 — Testing + Hardening

**Goal:** Full test coverage, parity with Python version, release readiness.
**Duration:** 2–3 weeks
**Owner:** All engineers
**Depends on:** Phase 3 complete

---

### Track H — Unit + Integration Tests  _(parallel with Track I)_

#### TASK-H1: Unit Tests — Scanner
- Port all `test_scanner.py` cases (22 patterns × positive + negative)
- Benchmark: `go test -bench=.` — must beat Python baseline

#### TASK-H2: Unit Tests — Vault
- Port `test_vault.py`: mask/restore, token format, thread safety (race detector: `go test -race`)

#### TASK-H3: Unit Tests — Decoder
- Port `test_decoder.py` + `test_decoder_encoded.py`
- Base64/hex/URL nested decode up to 4 levels
- Gzip compress/decompress round-trip

#### TASK-H4: Unit Tests — Upstream Proxy
- Port `test_upstream.py` (PAC, env, OS detection)
- Mock `scutil` / registry reads

#### TASK-H5: Integration Tests — Proxy Hook Pipeline
- Port `test_addon.py` cases
- Spin up local HTTP/HTTPS server in test, route through proxy, verify masking
- Test: gzip bodies, chunked responses, domain allowlist bypass

---

### Track I — E2E Tests + Python Parity  _(parallel with Track H)_

#### TASK-I1: E2E Test Harness
- Port `e2e_masking_test.sh` → Go test or shell (keep shell for simplicity)
- Start Go proxy, send requests with real secrets embedded, verify secrets not in forwarded request
- Verify secrets restored in response

#### TASK-I2: Claude CLI E2E Test
- Port `e2e_claude_test.sh` logic
- Run `claude` CLI through Go proxy, verify no secrets in upstream traffic

#### TASK-I3: Cross-Platform Smoke Tests
- Build matrix: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
- Install + start proxy on each platform in CI
- Run 5-minute soak test with synthetic load

#### TASK-I4: Python Parity Validation
- Run both Python and Go proxy side-by-side
- Feed identical requests to both
- Assert masked output is functionally equivalent (token format may differ, masking logic must not)

---

## Cross-Cutting Concerns (assign to one owner throughout)

| Concern | Notes |
|---|---|
| **Secret in logs** | `log_originals: false` must be enforced in Go logger from day 1. Audit test. |
| **Race detector** | Run all tests with `go test -race`. Vault and logger are the two hotspots. |
| **Memory bounds** | Scanner must not allocate unbounded memory on large bodies. Cap at configurable limit (default 10MB). |
| **Cert rotation** | If CA cert is regenerated, old trusted cert must be removed from keychain first. |
| **Idempotency** | Install/uninstall must be safe to run multiple times without corruption. |
| **Backwards compatibility** | Config file format (`agentproxy.yaml`) must remain identical. No migration needed for existing users. |

---

## Recommended Team Split (4 engineers)

| Engineer | Phase 1 | Phase 2 | Phase 3 | Phase 4 |
|---|---|---|---|---|
| **Eng 1** | Track A (proxy engine) | Track C (cert trust) | Track E (upstream proxy) | Track H (unit tests) |
| **Eng 2** | Track B (scanner/vault) | Track D (bat patching) | Track G (config/logging/CLI) | Track H (unit tests) |
| **Eng 3** | Track B support | Track D support | Track F (SSE — prototype + impl) | Track I (E2E tests) |
| **Eng 4** | Track A support | Track C support | Track F support | Track I + cross-platform |

---

## Definition of Done

- [ ] `agentproxy install` succeeds on macOS, Linux (Ubuntu + RHEL), Windows
- [ ] CA cert trusted in system keychain on all three platforms
- [ ] HTTPS traffic from `claude`, `codex`, `copilot` wrappers flows through proxy
- [ ] All 22 secret patterns masked in requests; secrets restored in responses
- [ ] SSE/chunked streaming works with <50ms added latency per chunk
- [ ] All unit tests pass with `-race` flag
- [ ] E2E test against real Claude CLI passes
- [ ] Static binary ships: no Python, no uv, no mitmproxy dependency
- [ ] `agentproxy uninstall` cleanly reverses all changes
- [ ] Config format backwards-compatible with Python version

---

## Out of Scope (not in this migration)

- HTTP/2 push (AI APIs don't use it)
- gRPC interception (direct TCP, bypasses proxy by design)
- Multi-machine proxy (stays localhost-only: `127.0.0.1:7717`)
- GUI / web dashboard
- Cloud-synced vault

---

## Key Libraries (Go)

| Library | Use |
|---|---|
| `elazarl/goproxy` | MITM proxy engine (CONNECT tunnel, hooks) |
| `crypto/x509`, `crypto/tls` | Cert generation + TLS (stdlib) |
| `gopkg.in/yaml.v3` | Config parsing |
| `joho/godotenv` | `.env` file loading |
| `jackwakefield/gopac` | PAC file evaluation (optional) |
| `spf13/cobra` | CLI subcommands |
| `compress/gzip`, `encoding/base64` | Body decode (stdlib) |
| `regexp` | RE2 regex (stdlib) |
| `sync` | `RWMutex`, `Map` for vault (stdlib) |

---

*This document covers migration from Python/mitmproxy to a Go static binary. The Python version remains the production system until Phase 4 Definition of Done is fully checked off.*
