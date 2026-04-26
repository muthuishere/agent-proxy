# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working in this repository.

## Working Contract

This repo is run as a **spec-first codebase**. Read `AGENTS.md` before any non-trivial change.

1. Understand the current code and existing docs.
2. Create or update a spec in `docs/specs/`.
3. Implement only after the spec exists.
4. Update durable docs (`README.md`, this file, `docs/`) when shipped behaviour changes.

The launch slices in `docs/specs/index-launch-slices.md` are the canonical roadmap. Each slice has a "Team Review — 2026-04-25" section with audit findings and elevated tasks; honour those when implementing.

## What This Project Is

AgentProxy is a local MITM proxy that intercepts HTTP/HTTPS traffic from AI coding agents (Claude, Codex, Copilot), masks secrets before they reach upstream APIs, and restores them in responses. It runs entirely locally — no cloud account, no telemetry.

**Module:** `github.com/muthuishere/agentproxy` (Go 1.23.7)
**License:** MIT
**Vendored fork:** `github.com/elazarl/goproxy` is replaced by `./third_party/goproxywss` (adds WebSocket support).

## Build and Run

```bash
# Build
go build -o agentproxy ./cmd/agentproxy
# or:
task build

# Dev bootstrap: build, set up + trust CA, refresh ~/.local/bin wrappers
task prepare-local

# Dev watch mode (rebuild + restart on changes)
task dev

# Start proxy (:7717) + dashboard (:7718)
task start
# or: ./agentproxy start -config ./config/agentproxy.yaml

# Install to ~/.local/bin
task install

# CA cert export only (prints manual trust commands)
./agentproxy ca-setup

# CA export + OS-store trust (sudo on macOS/Linux, UAC on Windows)
task ca-setup

# Config validation
./agentproxy validate -config ./config/agentproxy.yaml
```

## Testing

```bash
# Unit + acceptance
task test
# or: go test ./...

# Acceptance only (no live API)
task test-acceptance
# or: go test ./tests/go/...

# Single test
go test ./internal/runtime/... -run TestHandleRequestMasksAndRestoresResponse -v

# Coverage
task test-cov

# E2E (need real keys + running proxy)
task test-e2e-claude     # needs ANTHROPIC_API_KEY
task test-e2e-masking    # needs OPENAI_API_KEY
```

Acceptance tests use the **real** `runtime.Service` stack with real pattern files, real vault, real scanner — just no live API. The helper `newService()` in `tests/go/masking_acceptance_test.go` loads config from the repo root. Prefer this over mocking when adding new round-trip tests.

## Architecture

### Data flow

```
AI Agent → HTTP_PROXY=127.0.0.1:7717 → proxy.Server (goproxy MITM, conditional)
   ↓
proxy.Server.OnRequest:
   ├── mints requestID = "<session>-<atomic counter>", stashes in ctx.UserData
   └── runtime.Service.HandleRequest(sessionID, host, path, method, headers, body, requestID)
        ├── codec.DecodeBody (gzip)
        ├── provider.ParseRequest (extract scannable text)
        ├── scanner.ScanText (parallel regex)
        ├── vault.Session.Mask (shape-preserving surrogate)
        ├── codec.RewriteEncodedBlobs (mask inside base64/hex/URL blobs)
        ├── logger.LogRequest (NDJSON to traffic.jsonl, with `id`)
        └── MaskingSink.RecordRequest (in-memory store: originals + replacements)
   → forward masked body to upstream
   ↓
Upstream response → proxy.Server.OnResponse:
   ├── retrieves requestID from ctx.UserData
   └── HandleResponse | HandleSSEResponse | HandleWebSocket
        ├── vault.Session.Restore (surrogate → original)
        ├── codec.RestoreEncodedBlobs
        ├── logger.LogResponse (with same `id`)
        └── MaskingSink.RecordResponse
   → return restored body to agent
```

### Key packages

| Package | Responsibility |
|---|---|
| `cmd/agentproxy/` | CLI entrypoint. Commands: `start`, `status`, `ca-setup`, `validate`, `config`, `version` |
| `internal/proxy/server.go` | goproxy MITM hooks. **Conditional CONNECT MITM** via `HandleConnectFunc` gated on `service.ShouldIntercept(host)` — non-intercepted domains pass through (no MITM, no cert presented) |
| `internal/runtime/service.go` | Orchestrates the full pipeline. Exposes `MaskingSink` interface so the dashboard can subscribe to per-request originals + replacements without an import cycle |
| `internal/runtime/sse.go`, `sse_delta.go` | **Delta-aware SSE restore.** Buffers events, parses `delta.text_delta`, runs surrogate substitution across event boundaries, holds back events until past the longest-surrogate window |
| `internal/scanner/scanner.go` | Parallel regex matcher with worker pool; returns `Match` with position info |
| `internal/vault/session.go` | Per-session surrogate↔original store. Surrogates are random-looking, shape-preserving, deterministic per (value, name). Exposes `Surrogates()` and `Replacements()` for the SSE reader and dashboard |
| `internal/codec/` | `body.go` (gzip), `json.go` (JSON string walking), `encoded.go` (base64/hex/URL blob masking + restore) |
| `internal/provider/` | `Provider` interface + registry mapping domains → handlers. Anthropic, OpenAI, Copilot, Default. Currently most providers inherit `DefaultProvider` for body parsing; provider-specific parsers are tracked work |
| `internal/logger/traffic.go` | NDJSON to `logs/traffic.jsonl`. Every event carries `id` (the per-request identifier minted in proxy server) so request and response can be paired by the dashboard |
| `internal/ui/store.go` | In-memory `EventStore` keyed by request id, TTL ring (default 10 min). Implements `runtime.MaskingSink`. **Originals never persisted to disk** |
| `internal/ui/handler.go` | Embedded dashboard SPA + endpoints `/api/metrics`, `/api/logs/stream`, `/api/traffic/{id}` |

## Configuration

| File | Purpose |
|---|---|
| `config/agentproxy.yaml` | Primary config. Proxy host/port, intercepted domains, masking options, logging |
| `config/patterns.yaml` | Regex secret patterns (Anthropic, OpenAI, GitHub, AWS, Stripe, etc.) |
| `config/pii_patterns.yaml` | Optional PII patterns (email, phone, SSN, credit card, IP) — disabled by default |

Proxy listens on `127.0.0.1:7717`. Dashboard on `127.0.0.1:7718`. Domains in `detection.intercepted_domains` are MITM'd; everything else passes through TCP-bridged with no cert presented.

## Wrapper scripts

`bin/claudeproxy`, `bin/codexproxy`, `bin/copilotproxy`, `bin/agentproxy-start`. Each:
1. Liveness-checks the proxy (`agentproxy status -q`).
2. Sets `HTTP_PROXY`, `HTTPS_PROXY`, `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE` to point at the local CA.
3. `exec`s the underlying agent. Env vars are scoped to the wrapper process — other terminals are unaffected.

## Critical invariants

These are non-obvious correctness rules. Violating them causes silent prod bugs.

1. **CONNECT MITM must be conditional on `intercepted_domains`.** If you change `internal/proxy/server.go`, do not switch back to `goproxy.AlwaysMitm` — that breaks every unrelated app sharing the proxy (Firebase, Razorpay, etc.). Pinned by `TestConditionalConnectMITM`.

2. **The masking pipeline must be SSE-event-aware.** A surrogate spanning multiple `content_block_delta` events must still be restored. Pinned by `TestOpenAIKey_SSE_SurrogateSplitAcrossEvents` and `TestOpenAIKey_SSE_SurrogateFragmentedIntoManyEvents`. If you simplify `sse.go`, you'll regress this — see `docs/specs/spec-sse-delta-aware-restore.md` for the design.

3. **Originals never go to disk.** `LogOriginals=true` is a debug option only; the dashboard's `/api/traffic/{id}` reads originals from the in-memory `EventStore` (TTL 10 min). Don't add a JSONL field that contains pre-mask original body.

4. **Vault sessions are per-`sessionID`** (currently the goproxy connection ID). Mask and Restore both use the same session, so if you ever fan out work across sessions, restore won't find the surrogate.

5. **Request IDs flow end-to-end.** `internal/proxy/server.go` mints `<session>-<counter>` per request, stashes in `ctx.UserData`, and the response hook reads it back. Every `LogRequest`/`LogResponse`/`LogWebSocket`/`MaskingSink` call takes the requestID. Don't add a new logger method without it.

## Wrapper scripts and the install story

The `install.sh` / `install.bat` scripts in the repo are the **post-extract installers** included inside GoReleaser archives — not source builders. The dev-source flow lives in Taskfile targets (`task prepare-local`, `task install`).

A user-friendly install path is being built per `docs/specs/spec-slice-01-install-via-npm-and-goreleaser.md` (npm one-liner, curl-pipe, brew tap, scoop bucket).

## Where to look for more

| Question | Where |
|---|---|
| Roadmap, slice sequencing | `docs/specs/index-launch-slices.md` |
| Current Claude support state | `docs/specs/spec-claude-support-status.md` |
| Why SSE has its own delta-aware reader | `docs/specs/spec-sse-delta-aware-restore.md` |
| How install will actually work for end users | `docs/specs/spec-slice-01-install-via-npm-and-goreleaser.md` |
| Dashboard architecture | `docs/specs/spec-slice-03-dashboard-request-response-diff.md` |
| The full agent-by-agent matrix | slices 04 (Claude) / 05 (Copilot) / 06 (Codex) |
| Marketing/launch playbook | `docs/specs/spec-slice-07-marketing-launch-plan.md` |
| Spec template | `docs/specs/spec-template.md` |

## Testing patterns

- **Acceptance:** `tests/go/masking_acceptance_test.go` and `tests/go/openai_key_roundtrip_test.go` exercise the full `runtime.Service` against real config. Use these for round-trip behaviour.
- **Unit:** package-local `_test.go` files for narrow logic (vault, scanner, codec, etc.).
- **Proxy server:** `internal/proxy/server_test.go` uses a real `runtime.Service` and a TCP listener for the proxy, asserts CONNECT behaviour against a fake upstream.
- **Dashboard:** `internal/ui/handler_test.go` uses `httptest` against the real handlers and a real `EventStore`.

When you find a production bug:

1. Write a failing test that reproduces it from the actual transport shape (JSON, SSE, WebSocket — whichever applies).
2. Drop it in the appropriate file. If the bug is multi-test scope, create a new file like `tests/go/openai_key_roundtrip_test.go`.
3. Fix. Confirm the test goes green and nothing else regresses (`go test ./...`).
4. Update the relevant spec.
