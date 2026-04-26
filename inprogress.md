# Work In Progress

Snapshot of where the launch work stands. Pair this with the canonical roadmap
in [`docs/specs/index-launch-slices.md`](docs/specs/index-launch-slices.md) and
the per-slice spec files under `docs/specs/spec-slice-*.md`.

---

## Done

### Cross-cutting P0 fixes

- **`go.mod` module path fix** — was `module github.com/ ` (invalid). Now
  `github.com/muthuishere/agentproxy`. Build is clean.
- **Conditional CONNECT MITM** — `internal/proxy/server.go:32` now uses
  `HandleConnectFunc` gated on `service.ShouldIntercept(host)`. Non-intercepted
  hosts (Firebase, Razorpay, Stripe, etc.) pass through TCP-bridged with no
  cert presented. Pinned by `TestConditionalConnectMITM`. **Root cause of the
  04-08 Firebase/Razorpay regression — closed.**
- **Request IDs end-to-end** — `event.RequestID` field added to logger; minted
  in proxy server (`<session>-<atomic counter>`), stashed in `ctx.UserData`,
  carried into `LogRequest` / `LogResponse` / `LogWebSocket` and the dashboard
  `MaskingSink`. Pinned by `TestTrafficLoggerEmitsRequestID`.
- **SSE delta-aware surrogate restore** — `internal/runtime/sse_delta.go` +
  rewritten `sse.go` parses Anthropic `content_block_delta` JSON, builds a
  virtual concatenated buffer across events, rewrites contributing events to
  embed the original. Closes the production OpenAI key bug from the 04-25
  user transcript. Pinned by `TestOpenAIKey_SSE_SurrogateSplitAcrossEvents`
  and `TestOpenAIKey_SSE_SurrogateFragmentedIntoManyEvents`.

### Slice 02 — One-Click Setup

- `agentproxy setup [--start --open --no-trust]` — orchestrates ca-setup →
  idempotent OS-store trust (uses `runMaybeSudo` to prepend `sudo` only when
  not already root) → optional start with browser open.
- `cmd/agentproxy/setup.go` — new command, including `openURL()` for
  macOS/Linux/Windows.
- `runStart` prints the eyeball-friendly banner
  `AgentProxy ready — proxy: http://HOST:PORT   dashboard: http://HOST:PORT`
  alongside the existing per-line greppable prints.
- `runStatus` prints the dashboard URL when running, in the same format.
- README Quick Start collapsed to two commands: `task install` →
  `agentproxy setup --open`.
- `cmd/agentproxy/setup_integration_test.go` — gated on `INTEGRATION_TRUST=1`.

### Slice 03 — Dashboard mask/restore detail

- **Backend:** `runtime.MaskingSink` interface; `internal/ui/store.go`
  `EventStore` (10-min TTL ring buffer, originals never go to disk);
  `Service.SetSink` wired in `cmd/agentproxy/main.go`.
- **Vault:** `vault.Session.Replacements()` and `Surrogates()` exposed.
- **Endpoints:** `GET /api/traffic` (newest-first list with replacement counts
  and `has_originals` flag); `GET /api/traffic/{id}` (full 4-pane detail).
  Routed via Go 1.22 pattern syntax to dodge the 301 subtree-redirect bug.
- **Frontend:** `internal/ui/dashboard.html` extended — clicking a row lazily
  fetches `/api/traffic/{id}` and renders four panes (orig req / masked req /
  upstream resp / restored resp) with `<mark>`-highlighted surrogates;
  Replacements panel below with pattern, full surrogate, mid-truncated
  original (first 4 + last 4 chars); "modified only" filter toggle; privacy
  note explaining 10-min in-memory retention.

### Slice 04 — Claude end-to-end

- All non-disruption claims now hold (depends on the cross-cutting MITM fix).
- **Tests:**
  - `TestOpenAIKey_*` — 12 round-trip cases (JSON, SSE single-event, SSE
    cross-event, fragmented, WebSocket, multi-key, escaped JSON, gzip,
    idempotent, very-long key).
  - `TestClaudeShape_ToolUseInputRoundTrip` — surrogate inside `tool_use.input.command`.
  - `TestClaudeShape_ImageBlockPassThrough` — base64 image block byte-identical.
  - `TestClaudeShape_ErrorResponsePassThrough` + `TestClaudeShape_SSEErrorEventPassThrough`.
- **Wrapper hardening:** `--no-proxy` flag on all three wrappers
  (`bin/claudeproxy`, `bin/codexproxy`, `bin/copilotproxy`).
- **Non-disruption test harness:** `tests/integration/non-disruption/` shell
  script runs Google / Razorpay / Firebase from a no-`HTTP_PROXY` subshell
  with proxy + CA trusted; Taskfile target `task test-non-disruption`.

### Slice 05 — Copilot

- Removed `api.github.com` from `intercepted_domains` in
  `config/agentproxy.yaml` AND `internal/provider/copilot.go::Domains()`
  AND the registry test in `internal/provider/provider_test.go`.
- `tests/go/copilot_shapes_test.go` — three tests covering suggest round-trip,
  chat-stream round-trip, and the regression-pin
  `TestCopilotApiGitHubComPassesThrough`.
- `docs/specs/spec-copilot-fix.md` — Status updated.

### Slice 06 — Codex

- **`spec-codex-websocket-fix.md` resolved as Option A** (HTTP/1.1 forced via
  `OpenAIProvider.WSTransport()` returning a `*http.Transport` with empty
  `TLSNextProto`). Pinned by `TestOpenAIWSTransportIsHTTP1Only`.
- `tests/go/codex_shapes_test.go` — four tests including SSE single-event
  surrogate inside `delta.content`, chat-completion round-trip, unlisted
  domain pass-through, and the WebSocket round-trip.
- `spec-websocket-test-gaps.md` — table marked done; cross-frame split kept
  as a single open follow-up.

### Live agent E2E harness (verified against real APIs 2026-04-25)

`tests/integration/e2e-agents/` — drives each CLI through its wrapper and
asserts the right endpoint shows up in `traffic.jsonl`:

| Agent  | Wrapper invocation                        | Endpoint asserted                              | Verified |
|--------|-------------------------------------------|------------------------------------------------|----------|
| Claude | `claudeproxy -p` + `claudeproxy --continue -p` | `api.anthropic.com /v1/messages` (SSE)        | ✅ |
| Codex  | `codexproxy exec` + `codexproxy exec resume --last` | `chatgpt.com /backend-api/codex/responses` (WS, status 101) | ✅ |
| Copilot| `copilotproxy --allow-all-tools -p` (×2)  | `api.individual.githubcopilot.com /responses` AND `api.github.com` count = 0 | ✅ |

Taskfile: `task test-e2e-agents` (all three) plus per-agent variants
`test-e2e-claude-cli`, `test-e2e-codex-cli`, `test-e2e-copilot-cli`.

### Documentation

- `CLAUDE.md` rewritten end-to-end with current architecture, the five
  critical invariants, and a where-to-look-for-more index pointing at every
  spec.
- 7 vertical-slice specs under `docs/specs/spec-slice-*.md` with
  Why / What Changes / Impact / Tasks / Done When + a Team Review section
  documenting the audit findings each agent acted on.
- `docs/specs/index-launch-slices.md` — sequencing graph.
- `docs/specs/spec-sse-delta-aware-restore.md` — the SSE fix design.
- `docs/specs/spec-claude-support-status.md` — living tracker of Claude
  coverage (every row now ✅ or has a tracked open follow-up).

---

## Still to do

### Slice 01 — Install via npm + GoReleaser

User-explicit: do this last.

- `.goreleaser.yaml` covering darwin/{amd64,arm64}, linux/{amd64,arm64},
  windows/amd64.
- `cmd/agentproxy/version.go` with `-ldflags`-injected `Version`, `Commit`,
  `BuildDate` (replace the hardcoded literal in `main.go`).
- `.github/workflows/release.yml` triggered on `v*` tag.
- `packages/npm/` — `package.json` declaring `bin` entries; postinstall
  resolves platform → downloads release archive → verifies SHA256 → places
  binary.
- `scripts/install-online.sh` (POSIX) and `.ps1` (Windows) for the curl-pipe path.
- Self-hosted Homebrew tap (`muthuishere/homebrew-agent-proxy`) and Scoop
  bucket (`muthuishere/scoop-agent-proxy`) wired through GoReleaser.
- Repurpose the existing `install.sh` as the post-extract installer (drop
  the `go build` it does today; the source-build path moves to
  `scripts/install-from-source.sh` or stays as a Taskfile target).
- Wrappers' repo-relative `..` path resolution must change to a layout-portable
  resolver (`bin/*` audit finding).

### Slice 02 — One-Click Setup (loose ends)

- Confirm dashboard URL banner format is greppable as a machine-readable line
  (e.g. `AGENTPROXY_DASHBOARD_URL=...`) so postinstall scripts can extract it.
- `setup` should skip the trust step (with a printed warning) when running
  non-interactive (no TTY) — guard against a hung sudo prompt.
- Polish the README screenshots once Slice 07 demo videos exist.

### Slice 04 — Claude (loose ends)

- Real-world live test of the 04-25 transcript scenario (paste real OpenAI
  key → Claude generates a curl tool call → curl runs → OpenAI returns 200
  / a real auth-related response, NOT `sk-ldme-...` 401). The `tests/integration/e2e-agents/test-claude-e2e.sh`
  proves the proxy path is healthy; this is a stricter assertion that the
  surrogate-restore round-trip holds for tool calls in the wild.

### Slice 05 — Copilot (loose ends)

- Capture sanitised real Copilot traffic into `tests/go/fixtures/copilot/`
  (synthetic-fixture coverage exists today).
- Resolve the `spec-copilot-fix.md` TLS-pinning / ALPN items if real capture
  surfaces a regression.
- README "Verified Agents" section with `copilotproxy` snippet.
- `gh repo view cli/cli` non-disruption smoke from a separate terminal — the
  Slice 04 harness has the curl-based version; this is the equivalent for the
  GitHub CLI flow.

### Slice 06 — Codex (loose ends)

- Capture sanitised real Codex traffic into `tests/go/fixtures/codex/`.
- Confirm `OpenAIProvider.ParseRequest` covers latest Codex payload shape
  (function-call / tool-use blocks).
- WebSocket cross-frame surrogate split test (`spec-websocket-test-gaps.md`
  remaining open item).
- README "Verified Agents" section with `codexproxy` snippet.
- Live `curl https://api.openai.com/v1/models` non-disruption smoke from a
  separate terminal (the Slice 04 harness covers the generic case).

### OpenAI-shape SSE delta-aware restore (new follow-up)

The current delta-aware SSE parser only recognises Anthropic's
`delta.type == "text_delta"` shape. Copilot uses the new OpenAI Responses API
which streams `delta.content` instead. Within-event restore works (byte-level
substitution catches it), but cross-event surrogate splits over `delta.content`
are not yet handled. Track as `docs/specs/spec-openai-responses-sse-restore.md`
when scoped.

### Slice 07 — Marketing & launch (gated)

Hard-gated on Slices 01–06 being demo-able on a fresh machine.

- Register `agentproxy.dev` (or finalise the domain).
- Set up DNS / TLS / hosting for the landing page.
- Landing page copy.
- README rewrite top-to-bottom (Quick Start already done; the rest awaits demo
  video / screenshot work).
- Three demo videos (≤ 60s each): install + first-run, dashboard four-pane,
  one-agent-no-surprises.
- Trust / Privacy page backed by code references.
- Show HN, Reddit, X, LinkedIn, dev.to drafts.
- Pre-launch competitive sweep (7 days before).
- 48h pre-launch dry-run on a fresh macOS VM and a fresh Ubuntu VM.

---

## How to verify everything stays green

```bash
# Unit + acceptance (no live API)
go test ./...

# Live integration (proxy must be running, real CLIs authenticated, burns API quota)
task start                # in another terminal
task test-non-disruption  # firebase / razorpay / google still reachable
task test-e2e-agents      # Claude + Codex + Copilot end-to-end
```

`tests/integration/e2e-agents/README.md` and
`tests/integration/non-disruption/README.md` document the runbook in detail.
