# Slice 06 — Codex End-to-End, Non-Disruptive

**Status:** Proposed
**Owner:** _tbd_
**Depends on:** Slice 02, Slice 03
**Demo:** With AgentProxy installed and started, the user runs `codexproxy <prompt>` (OpenAI Codex CLI) and gets a normal Codex response. Browser and other apps remain unaffected.

---

## Why

Codex is the third supported agent. There's an existing `internal/provider/openai.go`, a `codexproxy` wrapper, and `spec-codex-websocket-fix.md` flagging WebSocket handling. Reaching launch quality requires:

1. End-to-end with the real Codex CLI.
2. WebSocket interception working correctly via the vendored `goproxywss` (already merged into `third_party/goproxywss`).
3. Mask/restore on the OpenAI request/response shape for chat completions and the streaming variants Codex uses.
4. Non-disruption: unrelated OpenAI usage (e.g. ChatGPT website in a browser, other clients calling `api.openai.com`) is not affected when those apps are not configured to use the proxy.

---

## What Changes

- Acceptance tests covering Codex request/response shapes (chat completion, streaming SSE, WebSocket if used).
- Resolve any open items in `spec-codex-websocket-fix.md` and `spec-websocket-test-gaps.md`.
- Confirm `internal/provider/openai.go` `ParseRequest` extracts `messages[].content` correctly for current Codex payloads.
- Wrapper hardening for `bin/codexproxy`: `--no-proxy` flag.
- Non-disruption test: with proxy + trusted CA, an unrelated browser request to `api.openai.com` (e.g. via `curl` from a fresh shell) must succeed.

---

## Impact

- **Affected:** `internal/provider/openai.go`, `internal/proxy/server.go` (WebSocket path), `tests/go/`, `bin/codexproxy`, `docs/specs/spec-codex-websocket-fix.md`, `docs/specs/spec-websocket-test-gaps.md`.
- **Not affected:** Claude/Copilot paths, install flow, dashboard.

---

## Tasks

1. Capture a real Codex request/response and check sanitised fixtures into `tests/go/fixtures/codex/`. (open — needs live capture)
2. **DONE 2026-04-25** — `TestCodexChatCompletionRoundTrip` in `tests/go/codex_shapes_test.go` (non-streaming, `api.openai.com`).
3. **DONE 2026-04-25** — `TestCodexChatCompletionSSE` in `tests/go/codex_shapes_test.go`. Single-event surrogate restore works via the opaque-event byte-level Restore path; cross-event split over `delta.content` is tracked as a follow-up (Anthropic-shaped delta parser only recognises `text_delta`).
4. **DONE 2026-04-25** — `TestCodexWebSocketRoundTrip` in `tests/go/codex_shapes_test.go` (asserts `api.openai.com` is intercepted and exercises the WebSocket mask/restore path). `chatgpt.com` already covered in `masking_acceptance_test.go`.
5. **DONE 2026-04-25** — `spec-codex-websocket-fix.md` resolved (Option A — HTTP/1.1 forced via `OpenAIProvider.WSTransport()` returning a `*http.Transport` with empty `TLSNextProto`; pinned by `provider_test.go::TestOpenAIWSTransportIsHTTP1Only`). `spec-websocket-test-gaps.md` updated to mark all 10 listed tests DONE; one cross-frame split case tracked as open.
6. Confirm `internal/provider/openai.go` parser is current for Codex payloads. (open — depends on task 1 capture)
7. Wire `codexproxy --no-proxy`. (open)
8. Add non-disruption case: `curl https://api.openai.com/v1/models -H "Authorization: Bearer $OPENAI_API_KEY"` from a fresh shell (no `HTTP_PROXY`) must succeed. (open — manual)
9. Update README "Verified Agents" with `codexproxy`. (open)

---

## Done When

- [ ] `task test-acceptance` passes Codex tests including SSE and WebSocket if applicable.
- [ ] Manual: `codexproxy 'write a python fizzbuzz'` returns a normal Codex response; dashboard shows it masked + restored.
- [ ] Manual: an unrelated `curl https://api.openai.com/...` from a separate shell succeeds.
- [ ] `spec-codex-websocket-fix.md` and `spec-websocket-test-gaps.md` are closed or have explicit follow-ups.
- [ ] README documents `codexproxy` next to `claudeproxy` and `copilotproxy`.

---

## Team Review — 2026-04-25

### Depends on Slice 04 P0

Same conditional CONNECT MITM dependency as Slice 05. Cannot validate "unrelated `api.openai.com` calls unaffected" until that lands.

### Codebase state

- `internal/provider/openai.go` exists; inherits `DefaultProvider.ParseRequest`. Domain list includes `api.openai.com` and `chatgpt.com` (✓).
- WebSocket support: `third_party/goproxywss` is in place; one basic round-trip test exists (`TestHandleWebSocket_*`); cross-frame surrogate split has no test.
- `spec-codex-websocket-fix.md` flags an unresolved decision: Option A (force HTTP/1.1 for upgrade) vs. Option B (RFC 8441 over HTTP/2). **Pick one and close the loop in this slice** — it's been an open spec for too long.

### Tasks added

10. **P0** Resolve `spec-codex-websocket-fix.md` Option A vs. B. Decision recorded in this slice; implement and close the upstream spec.
11. **P0** Capture real Codex traffic (OpenAI chat completions + streaming SSE; WebSocket if used). Sanitise and check into `tests/go/fixtures/codex/`.
12. **P1** Confirm `OpenAIProvider.ParseRequest` covers latest Codex payload shape (e.g. `messages[].content` array form, function call/tool use blocks).
13. **P1** Add WebSocket cross-frame surrogate split test (parallel to the SSE cross-event test in Slice 04).
14. **P1** `--no-proxy` flag — same uniform addition as Slices 04 and 05.
