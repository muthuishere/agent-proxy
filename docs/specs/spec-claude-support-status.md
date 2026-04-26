# Spec: Claude Support Status Tracker

**Status:** Tracking (living document)
**Priority:** High
**Depends on:** `internal/provider/anthropic.go`, `bin/claudeproxy`, `config/agentproxy.yaml` (intercepted_domains), `tests/e2e_claude_test.sh`, `tests/go/masking_acceptance_test.go`

---

## Problem

Claude (Anthropic API + Claude Code CLI) is the primary first-class agent target for AgentProxy, but our coverage state is currently scattered across multiple specs (`spec-provider-parsers.md`, `spec-json-aware-masking.md`, `spec-shape-preserving-secret-surrogates.md`, etc.) and ad-hoc shell tests. There is no single document that answers: **"For Claude, what works today, what is broken or unverified, and what is outstanding?"**

This spec is a *living tracker* — it does not introduce new functional requirements of its own. It captures the present coverage state for Claude support so we can decide what to fix next without re-scanning the repo each time.

Update this spec whenever Claude support state changes (new endpoint covered, regression found, test added, etc.).

---

## Goals

- Maintain a single source of truth for Claude / Anthropic interception coverage.
- Make it obvious at a glance which Claude flows are: **working**, **partial / unverified**, or **missing**.
- Link to the underlying spec / code / test for each item so we don't duplicate detail.
- Track open questions and known issues specific to Claude.

---

## Non-Goals

- Tracking Codex, Copilot, or other providers (those get their own status specs if needed).
- Defining new masking patterns, parser logic, or test scaffolding here — those belong in their own specs.
- Live-updating from CI (manual updates for now).

---

## Coverage Matrix

Legend: ✅ working & tested | 🟡 implemented but unverified or partial | ❌ missing | ⚠️ known issue

### CLA-1 — Transport & Interception

| Item | State | Notes |
|------|-------|-------|
| `api.anthropic.com` listed in `detection.intercepted_domains` | ✅ | `config/agentproxy.yaml` |
| `claudeproxy` wrapper sets HTTP_PROXY / cert env vars | ✅ | `bin/claudeproxy` |
| Liveness check before exec | ✅ | `agentproxy status -q` gate |
| HTTP/2 SSE pass-through | ✅ | `AnthropicProvider.Transport()` returns nil — HTTP/2 fine |
| WebSocket support for Claude (if any future endpoints use it) | 🟡 | Vendored `goproxywss` is in place; no Anthropic WS endpoints today |

### CLA-2 — Request Parsing & Masking

| Item | State | Notes |
|------|-------|-------|
| `AnthropicProvider` registered & domain-routed | ✅ | `internal/provider/anthropic.go` |
| Request body parsing | 🟡 | Currently inherits `DefaultProvider` — no Anthropic-specific `ParseRequest`. Means body text is scanned generically; nested structures (system prompt array, tool definitions, image blocks) rely on JSON walker in `internal/codec/json.go` |
| Shape-preserving surrogate replacement | ✅ | Per `spec-shape-preserving-secret-surrogates.md`; verified in `tests/go/masking_acceptance_test.go` |
| Base64 / hex / URL-encoded blob masking inside Claude bodies | ✅ | `internal/codec/encoded.go`; covered by `tests/go/base64_boundary_test.go` |
| Tool-use input/output blocks (function calls) | ❌ | Not specifically tested. Generic JSON walking may catch strings, but no targeted assertions exist |
| Multi-modal image blocks (base64 image data) | 🟡 | Image base64 should pass through (not a secret) but no explicit test confirms we don't accidentally mask large base64 chunks as "encoded blobs" |

### CLA-3 — Response Handling

| Item | State | Notes |
|------|-------|-------|
| Non-streaming JSON response restore | ✅ | `TestClaudeScenarioDotenvAndEncodedPayload` and others in `tests/go/masking_acceptance_test.go` |
| SSE streaming basic restore | ✅ | `TestSSEResponseRestoresVaultTokens` (`tests/go/masking_acceptance_test.go:434`) — restores surrogate tokens from `text/event-stream` body. `internal/runtime/sse.go` buffers per `\n\n` event so within-event splits are safe |
| Surrogate split *across* SSE event boundaries | ✅ | Delta-aware buffering shipped 2026-04-25 (`internal/runtime/sse_delta.go`, `spec-sse-delta-aware-restore.md`). Tests: `TestOpenAIKey_SSE_SurrogateSplitAcrossEvents`, `TestOpenAIKey_SSE_SurrogateFragmentedIntoManyEvents` |
| `tool_use` / `tool_result` round-trip | ❌ | Confirmed — no targeted test in `tests/go/` |
| Error response shapes (`{"type":"error", "error":{...}}`) | 🟡 | Pass-through expected; no explicit test |

### CLA-4 — End-to-End

| Item | State | Notes |
|------|-------|-------|
| `tests/e2e_claude_test.sh` against live API | 🟡 | Script exists; requires `ANTHROPIC_API_KEY`; not run in CI |
| Claude Code CLI (`claude` binary) usage via `claudeproxy` | 🟡 | Manual smoke only; no automated harness |
| Logs land in `logs/traffic.jsonl` with masked + restored bodies | ✅ | Verified manually |

### CLA-5 — Patterns & Secret Coverage Relevant to Claude

| Item | State | Notes |
|------|-------|-------|
| Anthropic API key pattern (`sk-ant-…`) | ✅ | `config/patterns.yaml:5` → `sk-ant-api\d{2}-[A-Za-z0-9\-_]{20,}`. Exercised by `TestMaskAnthropicAPIKey` and `TestAnthropicHostInterception` |
| OpenAI keys leaking through Claude requests (e.g. user pasting OpenAI key into a chat) | ✅ | Generic patterns apply; not Claude-specific but covered |
| GitHub tokens, AWS keys, etc. in pasted code | ✅ | Generic patterns apply |

---

## Known Issues / Open Questions

1. **SSE chunk-boundary surrogate split** (CLA-3) — if a surrogate token straddles two `data:` frames, restoration will fail silently. Need a targeted test and possibly a small buffer in `HandleSSEResponse`.
2. **Anthropic-specific `ParseRequest`** (CLA-2) — should we keep inheriting `DefaultProvider`, or write a dedicated parser that knows the message structure (system, messages[], content blocks, tool_use)? Decision pending.
3. **Tool use round-trip coverage** (CLA-2, CLA-3) — no tests assert that a surrogate placed inside a `tool_use.input` JSON value survives the round-trip and is restored in the corresponding `tool_result`.
4. **Image base64 false positives** (CLA-2) — confirm large image base64 in messages isn't being scanned as encoded blob and bloating mask attempts.
5. **CI for E2E** — `e2e_claude_test.sh` needs a strategy: nightly with secret in repo CI, or skip in CI and document local-only.

---

## Action Items (current)

- [ ] Add SSE *cross-event-boundary* test for surrogate restore (covers CLA-3 ⚠️ row). Likely needs a small lookback buffer in `sseRestoreReader`.
- [ ] Decide on Anthropic-specific `ParseRequest` (open question 2). If yes, file dedicated spec.
- [ ] Add a `tool_use` / `tool_result` round-trip acceptance test in `tests/go/`.
- [ ] Add an image-block test confirming large base64 image data isn't corrupted by encoded-blob masking.
- [ ] Add an explicit error-response pass-through test (`{"type":"error", ...}`).
- [ ] Document local E2E flow for `e2e_claude_test.sh` in README until CI strategy is decided.

(When an item is done, move it to "Recently Closed" below with date.)

---

## Recently Closed

- 2026-04-25 — Verified `sk-ant-api…` pattern present in `config/patterns.yaml:5` and covered by `TestMaskAnthropicAPIKey` + `TestAnthropicHostInterception`. (was Action Item)
- 2026-04-25 — Verified basic SSE surrogate restore is implemented (`internal/runtime/sse.go`) and tested (`TestSSEResponseRestoresVaultTokens`). Cross-event-boundary edge case remains open.

---

## Acceptance Criteria

This spec is "healthy" when:

- [ ] Every row in the Coverage Matrix is either ✅ or has an explicit Action Item / Known Issue tracking it.
- [ ] No row has been 🟡 for more than two release cycles without a deliberate decision recorded in "Known Issues".
- [ ] Action Items section is non-empty only when work is genuinely outstanding; closed items move to "Recently Closed".

---

## Verification

- Run `task test-acceptance` and confirm all Claude-related rows in CLA-2/CLA-3 marked ✅ have a corresponding test reference.
- Manual: `task start` → `claudeproxy --version` (or any Claude Code invocation) → confirm `logs/traffic.jsonl` shows a request to `api.anthropic.com` with masked body and a restored response.
- Re-read this doc whenever a Claude-related PR merges; update the matrix in the same PR.
