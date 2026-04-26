# AgentProxy: Python → Go Migration Task Plan

**Project:** AgentProxy — MITM proxy for AI agent secret masking
**Migration Target:** Python 3.11 + mitmproxy → Go (single static binary)
**Prepared for:** Engineering Team
**Date:** 2026-03-27
**Source of truth for test scenarios:** `docs/testing-scenarios.md`

---

## Executive Summary

Replace the Python/mitmproxy stack with a Go-based static binary that:
- Intercepts HTTPS traffic via MITM (cert generation + system trust)
- Masks secrets before they reach AI APIs (Anthropic, OpenAI, Copilot)
- Ships as a single cross-platform binary (no Python/uv runtime dependency)
- Handles high concurrency via goroutines (10–20x throughput improvement)
- Self-installs: generates CA cert, trusts it in OS keychain, patches bat/shell env files

**Critical rule:** The Python version stays in production. Go version is dark until all spikes
resolve and Phase 4 DoD is checked off.

---

## ⚡ SPIKES — Do These First, Before Any Phase 1 Work

> Spikes are time-boxed research/prototype tasks where we do not know the answer yet.
> The outcome of each spike is a **decision document** (1 page max), not production code.
> **No Phase 1 work should begin on the affected tracks until the relevant spike resolves.**

---

### SPIKE-1: SSE / Chunked Streaming — Cross-Chunk Secret Spanning

**Time box:** 4 days
**Owner:** 1 engineer
**Blocks:** Track A (TASK-A4, TASK-A5), Track F entirely

**Background:**
`docs/testing-scenarios.md` explicitly lists _"streaming/SSE chunk-split placeholder
restoration is still a known limitation"_ in the Python version. Every AI API response
(Claude, Codex, Copilot) is streamed SSE. This is not an edge case — it is the primary
response path. We cannot carry this bug into Go; we need to resolve it upfront.

**What to build:**
- Local mock SSE server that streams a response where a secret (`sk-ant-api03-xxxx`) is
  deliberately split across two `data:` chunks
- Go proxy prototype that intercepts it
- Try both approaches:
  - **Option A — Per-event buffer:** accumulate bytes until `\n\n` SSE delimiter, scan
    the complete event, restore vault tokens, then forward
  - **Option B — Full-response buffer:** buffer entire response, scan, forward
    (defeats streaming from client's perspective)
  - **Option C — Sliding window:** maintain a rolling buffer of last N bytes across chunks
    to catch cross-boundary secrets

**Decision output:**
- Which approach? What is the added latency per chunk (must be <50ms)?
- Does per-event buffering miss secrets that span the `data:` payload boundary vs the
  chunk boundary? (These are different boundaries.)
- Does the approach preserve SSE event structure (`event:`, `data:`, `id:` fields)?

**Go/no-go gate:** If no approach achieves <50ms latency AND catches cross-chunk secrets,
escalate before starting Phase 3 Track F.

---

### SPIKE-2: Vault Restoration Semantics for Encoded Blobs

**Time box:** 3 days
**Owner:** 1 engineer
**Blocks:** Track B (TASK-B1, TASK-B4), decoder design entirely

**Background:**
`docs/testing-scenarios.md` states: _"encoded-secret masking still uses inline
`[MASKED:...]` markers instead of full vault restoration semantics"_. This is a known
unfixed bug in the Python version. If we port the same behavior to Go, we bake the same
bug in permanently. The decision must happen before the vault and decoder interfaces
are designed, because the answer changes both module shapes.

**The core question:**
When a secret is found inside a base64-encoded blob:

```
Original body:  {"data": "c2stYW50LWFwaTAzLWFiYw=="}   ← base64 of "sk-ant-api03-abc"
```

**Option A — Marker (current Python behavior, broken):**
Decode the blob, replace the secret with `[MASKED:ANTHROPIC_API_KEY]`, re-encode.
The response cannot restore this. The agent gets back a corrupted value.

**Option B — Full vault token in re-encoded blob:**
Decode the blob, replace the secret with vault placeholder, re-encode the whole blob
with the placeholder inside. On response, decode again, restore, re-encode.
Fully reversible but complex.

**Option C — Replace entire encoded blob with vault token:**
The vault token stands in for the whole encoded blob. On response, restore the original
encoded blob wholesale. Simpler, but the agent loses the non-secret parts of the blob.

**What to prototype:**
- Implement Option B and Option C as standalone functions
- Run the 6 encoding scenarios from `docs/testing-scenarios.md`:
  - base64-encoded connection string
  - base64-encoded dotenv block (multi-secret)
  - hex-encoded secret
  - URL-encoded secret
  - nested encoded content (up to depth 4)
  - encoded masking must preserve valid JSON body structure
- Verify: does the agent receive a valid, usable response in each case?

**Decision output:**
- Which option is chosen and why
- Does the chosen option break the JSON structure preservation requirement?
- Updated vault and decoder interface contracts (Go struct signatures)

---

### SPIKE-3: Vault Lifetime and Request Scoping

**Time box:** 2 days
**Owner:** 1 engineer
**Blocks:** Track B (TASK-B1 interface), affects all of Phase 1

**Background:**
`docs/testing-scenarios.md` flags: _"vault lifetime is process-wide and not
request-scoped"_. At the Go concurrency scale we are targeting (thousands of concurrent
agent connections), a process-wide vault that never evicts is:
- A memory leak (vault grows unbounded)
- A cross-request secret contamination risk (secret from conn #1 visible in memory
  during conn #10,000)
- A GC pressure source

This is not just a performance concern. It is a security design question that must be
settled before `vault.go` is written, because the interface changes based on the answer.

**Options to evaluate:**

| Option | How it works | Risk |
|---|---|---|
| **A — Process-wide, no eviction** | Current Python behavior. Simple. | Unbounded memory growth |
| **B — Request-scoped vault** | New vault per HTTP request. Secrets cannot leak across requests. GC handles cleanup. | Cross-request restoration impossible (SSE responses that reference earlier request tokens fail) |
| **C — TTL-based eviction** | Vault entries expire after N minutes (configurable). Background goroutine sweeps. | TTL must outlive longest SSE stream |
| **D — Session-scoped vault** | One vault per client connection. Cleaned up on connection close. | Need to track connection → vault mapping |

**What to prototype:**
- Implement Option C (TTL) and Option D (session-scoped) as minimal Go structs
- Simulate 1000 concurrent goroutines, each masking 10 secrets, running for 5 minutes
- Measure: memory growth, GC pauses, any vault misses on restoration
- Test: what happens when an SSE stream takes 90 seconds and TTL is 60 seconds?

**Decision output:**
- Chosen option + justification
- Vault interface contract: method signatures, constructor, lifetime management
- Configuration keys needed in `agentproxy.yaml`

---

### SPIKE-4: goproxy Library vs Custom MITM for WebSocket Support

**Time box:** 3 days
**Owner:** 1 engineer
**Blocks:** Track A (TASK-A4) — do not pick the proxy library until this resolves

**Background:**
Codex uses WebSocket to `chatgpt.com/backend-api/codex/responses`. This is confirmed
working in the Python/mitmproxy version and is in the required regression scenarios
in `docs/testing-scenarios.md`. `elazarl/goproxy` — the recommended Go MITM library —
has documented issues with WebSocket upgrades (the HTTP `Upgrade` header handling and
the subsequent TCP tunnel handoff are non-trivial).

If we pick goproxy and it cannot handle WebSocket, we discard Phase 1 Track A work and
rebuild from scratch. This spike must happen before TASK-A4.

**What to prototype:**
- Stand up a local WebSocket echo server (using `gorilla/websocket`)
- Point a WebSocket client at it through a goproxy-based proxy
- Send text frames containing a mock Codex payload (raw Postgres URL, AWS creds)
- Verify: does the proxy intercept the WebSocket upgrade? Can a hook read/modify frame
  payloads? Does the connection survive beyond the upgrade?

**If goproxy fails:** prototype the same thing using raw `net.Conn` hijacking from
`net/http` (manual CONNECT + TLS + WebSocket frame parser). Measure implementation
complexity.

**Decision output:**
- `goproxy` is viable — proceed with TASK-A4 using it, OR
- `goproxy` cannot handle WebSocket — use custom CONNECT tunnel with `net.Conn` hijack
- Estimated additional complexity if going custom
- Interface contract for TASK-A5 hook (does it need a separate WebSocket hook type?)

---

## Spike Decision Gate

All four spikes run in parallel. When done, hold a 1-hour decision meeting before
Phase 1 begins. Agenda:

| Spike | Decision needed |
|---|---|
| SPIKE-1 | SSE buffering approach + latency budget confirmed |
| SPIKE-2 | Vault token vs marker for encoded blobs — interface contracts signed off |
| SPIKE-3 | Vault lifetime model — vault.go interface finalized |
| SPIKE-4 | Proxy library choice locked |

**Only after all four decisions are documented does Phase 1 begin.**

---

## Dependency Graph Overview

```
[SPIKES — all parallel, run first]
  SPIKE-1 (SSE streaming)
  SPIKE-2 (encoded blob vault)
  SPIKE-3 (vault lifetime)
  SPIKE-4 (goproxy vs custom)
         │
         ▼ (decision gate)
Phase 1: Core Proxy (parallel tracks A + B)
    │
    ├── Track A: Proxy Engine + TLS  ← shape determined by SPIKE-4
    └── Track B: Secret Scanner + Vault  ← interface determined by SPIKE-2 + SPIKE-3

Phase 2: Installer (depends on Phase 1 binary buildable)
    │
    ├── Track C: Cert generation + OS trust
    └── Track D: Bat/Shell file patching

Phase 3: Feature Parity + Integration (depends on Phase 1 + 2)
    │
    ├── Track E: Upstream proxy + PAC
    ├── Track F: Streaming / SSE  ← implementation determined by SPIKE-1
    └── Track G: Config, Logging, CLI

Phase 4: Testing + Hardening (depends on Phase 3)
    │
    ├── Track H: Unit + integration tests  ← scenarios from testing-scenarios.md
    └── Track I: E2E tests + Python parity
```

---

## What is STATIC (must be done sequentially)

| Order | Task | Depends On |
|---|---|---|
| 0 | All 4 spikes complete + decision gate meeting | — |
| 1 | Repo scaffold: Go module, project layout, CI skeleton | Spike gate |
| 2 | CA cert generation | Repo scaffold |
| 3 | Per-host dynamic cert signing | CA cert generation |
| 4 | CONNECT tunnel (library chosen in SPIKE-4) | Per-host certs |
| 5 | Request/response hook interface (shape from SPIKE-2 + SPIKE-4) | CONNECT tunnel |
| 6 | Vault finalized (interface from SPIKE-2 + SPIKE-3) | Spike gate |
| 7 | Secret scanner wired into hook interface | Hook interface + vault |
| 8 | Installer produces working binary | All Phase 1 done |
| 9 | OS cert trust | CA cert generation |
| 10 | SSE implementation (approach from SPIKE-1) | Phase 1 complete |
| 11 | E2E tests against real proxy (all 6 Claude scenarios + all 7 Codex scenarios) | Phase 3 complete |

---

## What is PARALLEL (can run simultaneously)

| Parallel Group | Tasks |
|---|---|
| **Spikes** (start immediately) | SPIKE-1, SPIKE-2, SPIKE-3, SPIKE-4 all run at the same time |
| **Phase 1 Group 1** (after spike gate) | Track A (proxy engine) AND Track B (scanner/vault) |
| **Phase 2 Group 2** (after scaffold) | Track C (cert trust) AND Track D (bat patching) |
| **Phase 3 Group 3** (after Phase 1) | Track E (upstream proxy) AND Track F (SSE) AND Track G (config/logging) |
| **Phase 4 Group 4** (after Phase 3) | Track H (unit tests) AND Track I (E2E tests) |

---

## Phase 1 — Core Proxy Engine

**Goal:** Working HTTPS MITM proxy that hooks requests and responses.
**Duration:** 2–3 weeks (after spike gate)
**Owner:** 2 engineers (Track A + Track B in parallel)

---

### Track A — Proxy Engine + TLS  _(start after SPIKE-4 resolves)_

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
- Add `Taskfile.yml` targets: `build`, `test`, `lint`, `cross-compile`
- Set up GitHub Actions CI: build matrix (linux/mac/windows, amd64/arm64)
- **Static dependency:** all other tasks depend on this

#### TASK-A2: CA Certificate Generation
- Use `crypto/x509` + `crypto/rsa` (stdlib) to generate a local CA keypair
- Store at `~/.agentproxy/ca-cert.pem` + `~/.agentproxy/ca-key.pem`
- Cert fields: `CN=AgentProxy CA`, `IsCA=true`, `KeyUsageCertSign`, 10yr validity
- Regenerate only if missing or expired
- Replaces the mitmproxy auto-generated cert

#### TASK-A3: Per-Host Dynamic Certificate Signing
- On each CONNECT request, generate a leaf cert for the target hostname
- Sign with CA from TASK-A2
- Cache signed certs in-memory (`sync.Map`, keyed by hostname)
- Cache TTL: 1 hour (certs valid 24h)
- **Depends on:** TASK-A2

#### TASK-A4: HTTPS MITM CONNECT Tunnel
- Library: use outcome of **SPIKE-4** (goproxy or custom)
- On CONNECT: complete TLS handshake with client using per-host cert, open upstream TLS
- Handle HTTP/1.1; HTTP/2 ALPN negotiation if feasible
- WebSocket upgrade path must be handled (confirmed working in Python — required regression)
- **Depends on:** TASK-A3, SPIKE-4 decision

#### TASK-A5: Request/Response Hook Interface
- Interface shape informed by **SPIKE-2** (does vault need to be passed per-request?)
- and **SPIKE-3** (session-scoped vault needs connection context)
  ```go
  type RequestHook interface {
      HandleRequest(ctx context.Context, req *http.Request) (*http.Request, error)
  }
  type ResponseHook interface {
      HandleResponse(ctx context.Context, resp *http.Response, req *http.Request) (*http.Response, error)
  }
  ```
- Domain allowlist check (skip non-intercepted domains — pass through untouched)
- **Depends on:** TASK-A4, SPIKE-2 + SPIKE-3 decisions

---

### Track B — Secret Scanner + Vault  _(start after SPIKE-2 + SPIKE-3 resolve)_

#### TASK-B1: Vault (Mask/Restore Store)
- Interface contract from **SPIKE-3** decision
- Thread-safe: `sync.RWMutex` + `map[string]string`
- Token format: `{prefix}{stars}{suffix}[{name}:{digest8}]` (same as Python, no format change)
- Methods: `Mask(original, name, prefix, suffix) string`, `Restore(body string) string`
- Lifetime model: TTL eviction or session-scoped (as decided in SPIKE-3)
- **Depends on:** SPIKE-3 decision

#### TASK-B2: Secret Scanner
- Port `scanner.py` → `internal/scanner/scanner.go`
- Load 22 patterns from `patterns.yaml` at startup
- `regexp` package (RE2 semantics — no backtracking, safe for untrusted input)
- Parallel scanning: one goroutine per pattern, collect via channel
- Return `[]Match{Name, Value, Start, End}`
- **No library dependencies — can start immediately after scaffold**

#### TASK-B3: PII Scanner (off by default)
- Port `pii_patterns.yaml` patterns (email, phone, SSN, credit card, IP)
- Same interface as TASK-B2
- Gated by `pii.enabled` config flag
- Test: PII disabled — email/phone/SSN remain untouched (required regression)
- Test: PII + secret masking coexist in same body (required regression)
- **Depends on:** TASK-B2 interface finalized

#### TASK-B4: Body Decoder (gzip / base64 / hex / URL)
- Implementation approach from **SPIKE-2** decision
- Decompress: `compress/gzip`
- Decode blobs: `encoding/base64`, `encoding/hex`, `net/url`
- Recursive scanning up to `MAX_DECODE_DEPTH=4`
- Must preserve valid JSON body structure after masking (known failure mode in Python)
- **Depends on:** TASK-B2, SPIKE-2 decision

---

## Phase 2 — Installer

**Goal:** Single binary that self-installs: cert trust + env/bat patching.
**Duration:** 1–2 weeks
**Owner:** 1–2 engineers (Track C + Track D in parallel)
**Depends on:** TASK-A1 scaffold only (can overlap early Phase 1)

---

### Track C — Certificate Trust  _(parallel with Track D)_

#### TASK-C1: Cert Trust — macOS
- `security add-trusted-cert -d -r trustRoot -k ~/Library/Keychains/login.keychain-db <cert>`
- Idempotent: check with `security find-certificate` before adding
- Uninstall: `security delete-certificate -c "AgentProxy CA"`

#### TASK-C2: Cert Trust — Windows
- `certutil -addstore -user Root <cert>`
- Idempotent: `certutil -verifystore -user Root "AgentProxy CA"`
- Uninstall: `certutil -delstore -user Root "AgentProxy CA"`

#### TASK-C3: Cert Trust — Linux (distro-aware)
- Detect distro from `/etc/os-release`
- Ubuntu/Debian: copy to `/usr/local/share/ca-certificates/`, run `update-ca-certificates`
- RHEL/CentOS: copy to `/etc/pki/ca-trust/source/anchors/`, run `update-ca-trust extract`
- Arch: copy to `/etc/ca-certificates/trust-source/anchors/`, run `trust extract-compat`
- Uninstall: remove file + re-run update command

#### TASK-C4: Runtime Env Var Injection
- Wrapper scripts set: `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`,
  `CURL_CA_BUNDLE` → `~/.agentproxy/ca-cert.pem`
- Windows: `.cmd` wrappers; Unix: shell script wrappers

---

### Track D — Bat / Shell File Patching  _(parallel with Track C)_

#### TASK-D1: Bat File Patcher (Windows)
- Locate `.bat` / `.cmd` files (configurable list + PATH auto-discovery)
- Inject:
  ```bat
  REM agentproxy-patched
  set HTTP_PROXY=http://127.0.0.1:7717
  set HTTPS_PROXY=http://127.0.0.1:7717
  set NODE_EXTRA_CA_CERTS=%USERPROFILE%\.agentproxy\ca-cert.pem
  ```
- **Always backup** to `<file>.agentproxy.bak` before patching
- Idempotent: skip if `REM agentproxy-patched` marker present
- Uninstall: restore from `.bak`

#### TASK-D2: Shell Script Patcher (Unix)
- Same pattern for shell wrappers and `~/.bashrc`, `~/.zshrc`
- Inject block between `# agentproxy-start` and `# agentproxy-end` markers
- Uninstall: remove marker block

#### TASK-D3: Installer CLI Subcommands
- `agentproxy install` — cert trust + file patching + write config
- `agentproxy uninstall` — reverses all changes, restores backups
- `agentproxy status` — cert trusted?, proxy running?, patched files list
- All subcommands idempotent and safe to re-run
- **Depends on:** TASK-C1/C2/C3 and TASK-D1/D2

---

## Phase 3 — Feature Parity

**Goal:** Match all features of the Python version, resolve all known open gaps.
**Duration:** 2–3 weeks
**Owner:** 2–3 engineers (Tracks E + F + G in parallel)
**Depends on:** Phase 1 complete

---

### Track E — Upstream Proxy + PAC  _(parallel with F and G)_

#### TASK-E1: Env-Based Upstream Proxy
- Read `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` from environment
- Support `username:password@host:port`

#### TASK-E2: OS System Proxy Detection
- macOS: parse `scutil --proxy` output
- Windows: registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
- Linux: env vars only

#### TASK-E3: PAC File Support
- Library: `jackwakefield/gopac`
- Per-request: `FindProxyForURL(url, host)` → parse result
- Cache PAC fetch (5 min TTL)
- Gate behind `upstream_proxy.pac_file` config

---

### Track F — Streaming / SSE  _(implementation from SPIKE-1)_

#### TASK-F1: Chunked Transfer + SSE Implementation
- Implement the approach decided in **SPIKE-1**
- Handle `Transfer-Encoding: chunked` + `Content-Type: text/event-stream`
- Must handle the cross-chunk secret split case (known gap in Python, must be fixed here)
- Preserve SSE event structure (`event:`, `data:`, `id:` fields)
- Latency budget: <50ms added per SSE chunk
- **Depends on:** SPIKE-1 decision

#### TASK-F2: WebSocket Frame Handling
- On `Upgrade: websocket`, switch to WebSocket frame pipeline
- Required regression scenarios from `docs/testing-scenarios.md`:
  - Codex WebSocket to `chatgpt.com/backend-api/codex/responses`
  - Client-to-server frame masking
  - Server-to-client frame restoration
  - Verify no `UnknownIssuer` or TLS fallback
- **Depends on:** SPIKE-4 decision (WebSocket hook interface)

---

### Track G — Config, Logging, CLI  _(parallel with E and F)_

#### TASK-G1: Config Loader
- Parse `agentproxy.yaml` using `gopkg.in/yaml.v3`
- **Same schema as Python version** — no breaking changes for existing users
- Load `.env` via `godotenv`
- Search order: `./agentproxy.yaml` → `~/.agentproxy/agentproxy.yaml` → defaults

#### TASK-G2: JSONL Logger
- Port `logger.py` → `internal/logger/logger.go`
- Event types: `request`, `response`, `websocket`, `passthrough`
- Async write: buffered channel → single writer goroutine
- Rotate at 100MB
- `log_originals: false` enforced from day 1 — never log pre-masked values
- Log must contain vault placeholders, never raw secrets (required for all E2E tests)

#### TASK-G3: CLI Entrypoint
- `agentproxy start [--config path] [--port 7717]`
- `agentproxy install` / `uninstall` / `status` / `version`
- Use `spf13/cobra`

---

## Phase 4 — Testing + Hardening

**Goal:** All scenarios from `docs/testing-scenarios.md` pass. Python parity validated.
**Duration:** 2–3 weeks
**Owner:** All engineers
**Depends on:** Phase 3 complete

---

### Track H — Unit + Integration Tests  _(parallel with Track I)_

These map directly to the **Required Regression Scenarios** in `docs/testing-scenarios.md`.

#### TASK-H1: Scanner — Secret Masking Regression Suite
All of the following must pass (from testing-scenarios.md):
- [ ] Anthropic API key in JSON request body
- [ ] OpenAI API key in JSON request body
- [ ] OpenAI `sk-proj-...` key
- [ ] GitHub token in prompt body
- [ ] AWS access key id in plain text
- [ ] AWS access key id in assignment form
- [ ] AWS secret access key in assignment form
- [ ] AWS secret access key in quoted export form
- [ ] AWS secret access key in JSON-escaped export form
- [ ] AWS session token in assignment form
- [ ] Database connection string in plain text
- [ ] Database connection string inside JSON without leaking into escape sequences
- [ ] OpenSSH private key multiline block

#### TASK-H2: Decoder — Encoded Content Regression Suite
- [ ] base64-encoded connection string
- [ ] base64-encoded dotenv block
- [ ] hex-encoded secret
- [ ] URL-encoded secret
- [ ] Nested encoded content up to depth 4
- [ ] Encoded masking preserves valid JSON body structure

#### TASK-H3: Transport Behavior Regression Suite
- [ ] Plain HTTP request body masking
- [ ] Gzip-compressed request body masking
- [ ] Response restoration of vault placeholders
- [ ] WebSocket client-to-server masking
- [ ] WebSocket server-to-client restoration

#### TASK-H4: Domain Interception Regression Suite
- [ ] Listed AI domains intercepted
- [ ] Unlisted domains pass through untouched
- [ ] Local gateway with port-only host

#### TASK-H5: PII Behavior Regression Suite
- [ ] PII disabled: email/phone/SSN remain untouched
- [ ] PII enabled: only selected entities masked
- [ ] PII + secret masking coexist in same body
- [ ] Encoded blobs containing enabled PII entities masked

#### TASK-H6: Vault Thread Safety
- Port `test_vault.py` with `go test -race`
- Simulate 100 concurrent goroutines masking + restoring simultaneously
- Verify vault eviction (TTL or session) does not drop in-flight tokens

---

### Track I — E2E Tests + Python Parity  _(parallel with Track H)_

These map directly to the **Claude Scenarios** and **Codex Scenarios** in
`docs/testing-scenarios.md`.

#### TASK-I1: Claude E2E Scenarios
All 6 Claude scenarios from testing-scenarios.md must pass:
- [ ] Scenario 1: Plain prompt with raw Postgres URL
- [ ] Scenario 2: Prompt with dotenv block (DATABASE_URL, OPENAI_API_KEY, AUTH_TOKEN,
  AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN)
- [ ] Scenario 3: Prompt with base64 and URL-encoded copies of Postgres URL
- [ ] Scenario 4: Long AGENTS-style prompt with YAML, shell exports, OpenSSH private key
- [ ] Scenario 5: Claude returns expected response text (agent not broken by masking)
- [ ] Scenario 6: `logs/traffic.jsonl` contains masked placeholders, zero raw secrets

Additionally:
- [ ] PII disabled: email/phone/SSN in prompt remain untouched
- [ ] PII enabled: selected PII entities masked

#### TASK-I2: Codex E2E Scenarios
All 7 Codex scenarios from testing-scenarios.md must pass:
- [ ] Scenario 1: HTTPS request flow through Go proxy
- [ ] Scenario 2: WebSocket upgrade to `chatgpt.com/backend-api/codex/responses`
- [ ] Scenario 3: Prompt with raw Postgres URL
- [ ] Scenario 4: Prompt with AWS credentials
- [ ] Scenario 5: Prompt with OpenSSH private key block
- [ ] Scenario 6: No `UnknownIssuer` or TLS fallback
- [ ] Scenario 7: Masked websocket frames in `logs/traffic.jsonl`

#### TASK-I3: Python Parity Validation
- Run Python proxy and Go proxy side-by-side
- Feed identical requests to both
- Assert masking is functionally equivalent
- This is the migration sign-off test

#### TASK-I4: Cross-Platform Smoke Tests
- Build matrix: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
- Install + start on each platform in CI
- 5-minute soak with synthetic load

---

## Open Gaps from Python — Must Be Resolved in Go (Not Carried Forward)

These are listed in `docs/testing-scenarios.md` as known gaps in the current Python version.
They are not optional deferred work — they are required to be fixed before the Go version ships.

| Gap | Where it surfaces | Resolution track |
|---|---|---|
| Encoded-blob masking uses irreversible `[MASKED:...]` markers | decoder.py | SPIKE-2 → TASK-B4 |
| SSE chunk-split secret not restored | addon.py streaming path | SPIKE-1 → TASK-F1 |
| Vault is process-wide, grows unbounded | vault.py | SPIKE-3 → TASK-B1 |
| Copilot coverage incomplete | No e2e test exists | TASK-I2 extension (post-ship) |

---

## Cross-Cutting Concerns

| Concern | Owner | Notes |
|---|---|---|
| **Secret never in logs** | Assigned from day 1 | `log_originals: false` enforced; audit test in TASK-H6 |
| **Race detector** | All | `go test -race` on every PR; vault and logger are hotspots |
| **Memory bounds** | Eng 1 | Cap body scan at 10MB; log warning, do not crash |
| **Cert rotation** | Eng 1 | Remove old trusted cert before re-adding regenerated cert |
| **Idempotency** | Eng 2 | Install/uninstall safe to re-run; tested in TASK-I4 |
| **Config backwards-compat** | Eng 2 | `agentproxy.yaml` schema identical; no migration needed |
| **JSON structure preservation** | Eng 3 | Critical — regex broadening that breaks JSON is a known failure mode |

---

## Recommended Team Split (4 engineers)

| Engineer | Spikes | Phase 1 | Phase 2 | Phase 3 | Phase 4 |
|---|---|---|---|---|---|
| **Eng 1** | SPIKE-4 (goproxy) | Track A (proxy engine) | Track C (cert trust) | Track E (upstream) | Track H |
| **Eng 2** | SPIKE-3 (vault lifetime) | Track B (scanner/vault) | Track D (bat patching) | Track G (config/CLI) | Track H |
| **Eng 3** | SPIKE-1 (SSE streaming) | Track B support | Track D support | Track F (SSE impl) | Track I (E2E) |
| **Eng 4** | SPIKE-2 (encoded blobs) | Track A support | Track C support | Track F (WebSocket) | Track I + cross-platform |

---

## Definition of Done

**Spikes:**
- [ ] All 4 spike decision documents written and reviewed
- [ ] Decision gate meeting held, interfaces signed off

**Install:**
- [ ] `agentproxy install` succeeds on macOS, Linux (Ubuntu + RHEL), Windows
- [ ] CA cert trusted in system keychain on all three platforms
- [ ] `agentproxy uninstall` cleanly reverses all changes, restores bat file backups

**Proxy:**
- [ ] HTTPS traffic from `claude`, `codex`, `copilot` wrappers flows through proxy
- [ ] All 22 secret patterns masked in requests; restored in responses
- [ ] SSE/chunked streaming works with <50ms added latency per chunk
- [ ] Cross-chunk secret spanning handled (fixed gap from Python)
- [ ] Encoded blob masking is fully reversible (fixed gap from Python)
- [ ] Vault eviction implemented (fixed gap from Python)

**Tests:**
- [ ] All required regression scenarios from `docs/testing-scenarios.md` pass
- [ ] All 6 Claude E2E scenarios pass
- [ ] All 7 Codex E2E scenarios pass
- [ ] `go test -race` passes on vault and logger
- [ ] Python parity validation passes (TASK-I3)

**Binary:**
- [ ] Static binary: no Python, no uv, no mitmproxy dependency
- [ ] Config format backwards-compatible
- [ ] Ships on all 5 target platforms (TASK-I4)

---

## Out of Scope

- HTTP/2 push (AI APIs don't use it)
- gRPC interception (direct TCP, bypasses proxy by design)
- Multi-machine proxy (stays localhost-only: `127.0.0.1:7717`)
- GUI / web dashboard
- Cloud-synced vault
- Copilot real-host E2E coverage (deferred post-ship per testing-scenarios.md)
- Contextual PII detection (regex-only is acceptable for v1)

---

## Key Libraries (Go)

| Library | Use | Confirmed by |
|---|---|---|
| `elazarl/goproxy` or custom | MITM proxy engine | SPIKE-4 decision |
| `crypto/x509`, `crypto/tls` | Cert generation + TLS | stdlib |
| `gopkg.in/yaml.v3` | Config parsing | — |
| `joho/godotenv` | `.env` file loading | — |
| `jackwakefield/gopac` | PAC file evaluation | optional |
| `spf13/cobra` | CLI subcommands | — |
| `compress/gzip`, `encoding/base64` | Body decode | stdlib |
| `regexp` | RE2 regex | stdlib |
| `sync` | `RWMutex`, `Map` for vault | stdlib |
| `gorilla/websocket` | WebSocket frame handling (if needed) | SPIKE-4 decision |

---

*Python version stays in production until Phase 4 DoD is fully checked off.
Testing baseline: `docs/testing-scenarios.md` validated 2026-03-27.*

---

## Execution Orchestration

Execution tracker: `docs/migration-go-tracker.md`

Decision documents:
- `docs/decisions/spike-1-sse-buffering.md`
- `docs/decisions/spike-2-encoded-blob-restoration.md`
- `docs/decisions/spike-3-vault-lifetime.md`
- `docs/decisions/spike-4-proxy-library-websocket.md`

Safe work that can start immediately without violating the spike gate:
- Go module scaffold and binary entrypoint
- `go-spikes/` sandboxes for each spike
- fixture extraction from `docs/testing-scenarios.md`
- benchmark and prototype harnesses inside spike directories

Unsafe work that must wait for spike closure:
- final proxy engine implementation under `internal/proxy`
- final vault contract under `internal/vault`
- final decoder contract under `internal/decoder`
- final WebSocket hook path in production code
