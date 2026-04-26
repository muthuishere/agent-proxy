# Slice 03 — Dashboard Shows Request/Response with Mask & Restore Diff

**Status:** Proposed
**Owner:** _tbd_
**Depends on:** Slice 02 (dashboard reachable from one-click start)
**Demo:** A user opens `http://127.0.0.1:7718`, runs an agent, sees each intercepted call as a row, clicks it, and sees four side-by-side panes — original request, masked request, original response, restored response — with diffs highlighted.

---

## Why

The dashboard exists (`internal/ui/`) but its current view is shallow: it lists traffic but does not let the user *see what AgentProxy did*. The whole value proposition is "we mask secrets before they leave your machine and restore them on the way back." Users need to *see* that happening: which fields were touched, which surrogates replaced which originals, what came back from upstream, and what was restored before the agent saw it.

Without this, AgentProxy is a black box and trust is hard to earn.

---

## What Changes

- Dashboard request rows include a new "Modified" badge with the count of replacements per direction (e.g. `req: 3 / resp: 1`).
- Clicking a row opens a detail view with **four panes**:
  1. **Original request body** (what the agent sent — never logged to disk in plaintext, kept in memory only with a configurable retention window).
  2. **Masked request body** (what we forwarded upstream).
  3. **Upstream response body** (what came back, before restoration).
  4. **Restored response body** (what the agent received).
- Inline diff highlighting: every byte that differs between pane 1↔2 and pane 3↔4 is highlighted; hovering shows the surrogate ↔ original mapping (anonymised — display original truncated, e.g. `sk-ant-…12fA`).
- A "Replacements" panel lists the rows of the in-flight vault session: pattern name, surrogate (full), original (truncated), direction (request/response), and where it appeared (JSON path or byte offset).
- A small toolbar on the row list to filter by provider (Anthropic / OpenAI / Copilot / Default), by "had replacements" vs "passed through unchanged", and by status code.
- New backend endpoint(s) under `internal/ui/` to serve per-request detail + replacement metadata. Existing `logs/traffic.jsonl` already captures masked bodies; we extend it (or pair it with an in-memory ring buffer) to also retain originals for the retention window.

---

## Impact

- **Affected:** `internal/ui/` (frontend + handler), `internal/runtime/service.go` (capture original alongside masked), `internal/logger/traffic.go` (extended log shape), config (new `dashboard.retention` setting).
- **Privacy-sensitive:** originals contain secrets. Default retention = 10 minutes in memory only, never to disk. Configurable. Document this prominently.
- **Not affected:** install flow, provider parsers, masking patterns.

---

## Tasks

1. Extend `runtime.Service.HandleRequest` / `HandleResponse` to surface a structured `MaskingEvent` (original, masked, replacement list, direction, request id) to a sink.
2. Add an in-memory ring buffer in `internal/ui/` keyed by request id with TTL eviction. No disk persistence for originals.
3. Add `internal/logger/traffic.go` field for per-request `id` so frontend can correlate the on-disk masked log with the in-memory original.
4. Backend endpoints:
   - `GET /api/traffic` — list rows (existing, augmented with replacement counts). _Pending — list-side rendering currently uses the SSE stream + JSONL replay; a dedicated list endpoint is the next dashboard step._
   - ~~`GET /api/traffic/{id}` — full detail with all four panes + replacements.~~ **DONE 2026-04-25** — `internal/ui/handler.go` registers `/api/traffic/` and serves `MaskingEvent` JSON. Tests: `TestTrafficDetailEndpointReturnsRecordedEvent`, `TestTrafficDetailEndpoint404OnUnknownID`, `TestTrafficDetailEndpoint404WhenStoreNil`. cmd wiring (`cmd/agentproxy/main.go`) instantiates `EventStore` and calls `service.SetSink`.
5. ~~Frontend detail view: four-pane layout, syntax highlighting for JSON, byte-level diff highlighting for non-JSON.~~ **DONE 2026-04-25** — `internal/ui/dashboard.html` lazily fetches `/api/traffic/{id}` on row expand and renders the 4-pane (original req, masked req, upstream resp, restored resp) with `<mark>` highlighting on surrogate tokens.
6. ~~Replacement panel with full surrogate value but truncated original (first 4 + last 4 chars).~~ **DONE 2026-04-25** — Replacements panel under the panes; `truncateMid` keeps first 4 + last 4 chars of the original.
7. ~~Filter toolbar~~ **DONE 2026-04-25** — added "modified only" toggle alongside the existing "show clean" / "show passthrough" / "auto-scroll" controls. Provider filter and status-code filter are P2 and pending.
8. Config option `dashboard.retention_seconds` (default 600); also a `dashboard.disable_originals` boolean for users who want zero-original-retention mode.
9. Visual regression test: snapshot of the detail view with a known fixture.
10. Document privacy model in README + dashboard footer ("Originals kept in memory for X minutes; never written to disk").

---

## Done When

- [ ] User opens dashboard, runs `claudeproxy 'echo hi'`, sees the call appear in real time.
- [ ] Clicking the row opens the four-pane detail view; secrets visibly masked in pane 2, restored in pane 4.
- [ ] Replacement panel shows the surrogate↔original mapping for that call.
- [ ] After `dashboard.retention_seconds` elapses, originals are gone from the detail view (only masked remains, served from `traffic.jsonl`).
- [ ] Filter "modified-only" hides pass-through traffic.
- [ ] No originals appear anywhere in `logs/traffic.jsonl`.
- [ ] Privacy note is visible in the dashboard footer and in README.

---

## Team Review — 2026-04-25

### P0 — Verified Blockers

- **Request IDs do not flow end-to-end.** `internal/logger/traffic.go` event struct has no `id` field. `service.go HandleRequest`/`HandleResponse` accept a `sessionID` but don't pass it to the logger. **Until this is fixed, the dashboard cannot correlate a row in `traffic.jsonl` with the in-memory original — this is a hard blocker for the entire slice.**
- **Originals are never stored in memory.** Today `LogRequest(originalBody, modifiedBody, …)` writes both straight to JSONL when `LogOriginals` is enabled. There's no in-memory buffer; there's also a *privacy regression risk* — the existing `LogOriginals=true` config writes plaintext secrets to disk. Slice 03 must (a) introduce the in-memory ring buffer and (b) **disable any path that writes originals to JSONL**.
- **Vault session does not expose its replacements.** `internal/vault/session.go` tracks mask/restore internally but has no method like `Replacements() []Replacement`. Add it before the dashboard can render the Replacements panel.

### Codebase state

- **Dashboard is a vanilla-JS embedded SPA** (`internal/ui/dashboard.html`, ~314 lines) currently showing 2 panes (orig req / masked req side-by-side, then orig resp / restored resp). Slice 03 is an *extension* to 4 panes + diff + replacement panel, **not a rewrite**.
- **UI handler currently exposes `/api/metrics` and `/api/logs/stream`.** New endpoints `/api/traffic` (list with replacement counts) and `/api/traffic/{id}` (full detail) are net-new.
- **CA-trust idempotency** (irrelevant here — that's slice 02) is fully feasible.

### Tasks added (elevated)

11. ~~**P0** Add `RequestID string` to `traffic.go` event struct; thread it from `service.HandleRequest` → `logger.LogRequest`.~~ **DONE 2026-04-25** — `event.RequestID` added; `LogRequest`/`LogResponse`/`LogWebSocket` take a trailing `requestID string`; `Service.Handle{Request,Response,SSEResponse,WebSocket}` thread it through.
12. ~~**P0** Generate request IDs at the proxy boundary (e.g. ULID) so client + server share the same id.~~ **DONE 2026-04-25** — `internal/proxy/server.go` mints `<session>-<atomic counter>` per request; stored in `ctx.UserData` so the response hook reads the same id. Test: `TestTrafficLoggerEmitsRequestID` proves request and response events share the id in `traffic.jsonl`.
13. **P0** Remove or gate the `LogOriginals` JSONL path; originals must live only in the in-memory buffer.
14. ~~**P0** Add `Session.Replacements()` returning `[]{Pattern, Surrogate, Original, Direction, Path}`; wire into the per-request detail.~~ **DONE 2026-04-25** — `vault.Session.Replacements()` returns `[]vault.Replacement{Pattern, Surrogate, Original}`. Pattern name is now tracked alongside the surrogate in a parallel `patterns` map. Direction/Path can be added later if needed (request vs response is already inferable from the sink call). Test: `TestSessionReplacementsExposesPatternAndOriginal`.
15. ~~**P1** Introduce `internal/ui/store.go` — TTL ring buffer keyed by request id; default 600s, configurable.~~ **DONE 2026-04-25** — `internal/ui/store.go` provides `EventStore` with `RecordRequest`/`RecordResponse`/`Get`, default 10-min TTL, janitor goroutine sweeps every TTL/4. `runtime.MaskingSink` interface added; `Service.SetSink()` accepts any implementer (UI store satisfies it without import cycle). Tests: `TestEventStoreRequestThenResponseSameID`, `TestEventStoreTTLEvicts`, `TestServiceEmitsToSinkWithOriginalsAndReplacements`.
16. **P1** Add `dashboard.retention_seconds` and `dashboard.disable_originals` to `internal/config/config.go`.
17. **P1** Frontend: add diff lib (lightweight, e.g. `jsdiff`) + JSON syntax highlighter; keep total dashboard JS budget tight (it's embedded).
