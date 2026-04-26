# Spec: SSE Delta-Aware Surrogate Restore

**Status:** Shipped 2026-04-25
**Priority:** P0 — production correctness blocker
**Depends on:** `internal/runtime/sse.go`, `internal/vault/session.go`
**Origin:** 2026-04-25 production transcript — a `sk-proj-...` OpenAI key was masked into the request to Anthropic, but the surrogate in Anthropic's streamed response did not get restored. The agent ran the resulting `curl -H "Authorization: Bearer SURROGATE"` and OpenAI rejected with 401 (`sk-ldme-...`). Logs in `logs/traffic.jsonl` confirm the surrogate hit `api.openai.com`.

---

## Problem

`session.Restore` is a string-replace from surrogate → original. It only works when the surrogate appears as a **contiguous substring** in the input.

Anthropic streams long content as many small `content_block_delta` events. A 100+ char surrogate is split across multiple events:

```
event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"sk-ldme-cNzlQ"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ES8YJNA1HVKhpXz_..."}}
```

In the byte stream the two halves are separated by JSON framing (`"}}` + newlines + `event: ...` + `data: {...text":"`). `session.Restore` looking for `sk-ldme-cNzlQES8YJNA1HVKhpXz_...` never finds it as a contiguous match. The bytes pass through unchanged, the client assembles the deltas, sees the surrogate, and uses it.

The byte-level holdback fix (already shipped — `internal/runtime/sse.go`) handles the case where the surrogate is split by a TCP/chunk boundary at a *non-event* point. It does **not** handle the case where the SSE framing itself splits the surrogate.

Pinned by failing-but-skipped tests:
- `TestOpenAIKey_SSE_SurrogateSplitAcrossEvents` — surrogate split across two events
- `TestOpenAIKey_SSE_SurrogateFragmentedIntoManyEvents` — surrogate fragmented into many ~10-char chunks (mimics real Anthropic delta sizes)

---

## Goals

- Restore a surrogate to its original even when it is split across multiple SSE `content_block_delta` events.
- Preserve SSE event count and ordering — clients that count events for progress UI must see the same number of events.
- Keep streaming latency low — emit each event as soon as it can be safely emitted (i.e. once we know it doesn't begin a partial surrogate that would need to be merged with later events).
- Pass the two skipped tests above without regressing any passing ones.

---

## Non-Goals

- Re-architecting the proxy → service → SSE reader path. The fix lives inside `sseRestoreReader`.
- Provider-specific JSON shapes beyond Anthropic's `content_block_delta` for v1. (Codex/Copilot SSE shapes can be added later.)
- Restoring surrogates split across **non-text** event types (e.g. tool_use input deltas). v1 covers `text_delta`.

---

## Approach

Replace the byte-level holdback with **event-aware buffering + virtual text reassembly**:

1. Parse complete SSE events from the raw stream (split on `\n\n` as today).
2. For each event, inspect the `data:` JSON. If `delta.type == "text_delta"`, extract `delta.text` and record an entry: `{eventIndex, textStart, textEnd}` mapped into a *virtual text buffer* (the concatenation of every `text_delta.text` seen so far).
3. After each new event arrives:
   - Run `session.Restore(virtualBuffer)`. If any surrogate matched, find which event(s) contributed bytes inside the matched span.
   - Rewrite those events: put the **full original** into the latest contributing event's `text` field; clear the `text` of the earlier contributing events to `""` so the assembled stream is byte-correct.
4. Emit events older than the longest possible in-flight surrogate (track `maxSurrogateLen` from `session.Surrogates()`). Buffer newer events until they pass that horizon or EOF arrives.
5. On EOF, flush all buffered events.

For events whose `data:` is not a JSON object or whose `delta` is not a text delta, pass through unchanged (subject to the same emit-when-safe rule).

---

## Functional Requirements

### SSE-1 — Per-event JSON parse

When an event's first non-blank line is `data: {…}`, parse the JSON. Tolerate parse failures (pass event through unchanged). Tolerate `data:` lines that are not JSON (e.g. `data: keepalive`).

### SSE-2 — Virtual buffer assembly

Maintain a single growing string of concatenated `text_delta.text` values across the lifetime of the SSE response. Track the byte range each event contributed.

### SSE-3 — Surrogate substitution and event rewrite

After every new contribution, search the virtual buffer for any known surrogate (using the same `session.Surrogates()` snapshot the holdback path uses). When found, identify the contributing events, rewrite their `text` fields to embed the original key in the latest one and zero out earlier contributions, then re-encode the events.

### SSE-4 — Bounded buffering / streaming output

Compute `H = max(len(s) for s in session.Surrogates())`. While the most recent event's contribution falls within the last `H` bytes of the virtual buffer, hold all events back. Emit events whose contribution lies entirely *before* `len(virtual) - H`. On EOF, emit all remaining events.

### SSE-5 — Encoded blobs (existing behaviour)

After SSE-3 substitution, also run `codec.RestoreEncodedBlobs` on each emitted event's `data:` line, as the current implementation does.

---

## Acceptance Criteria

- [x] ~~`TestOpenAIKey_SSE_SurrogateSplitAcrossEvents` passes~~ ✅ DONE 2026-04-25
- [x] ~~`TestOpenAIKey_SSE_SurrogateFragmentedIntoManyEvents` passes~~ ✅ DONE 2026-04-25
- [x] ~~All currently-passing SSE tests still pass~~ ✅ DONE — full suite green
- [ ] Manual smoke: replay the 2026-04-25 user transcript scenario (Claude paste OpenAI key + curl), confirm OpenAI returns 200 (or a real-key-related response), not `sk-ldme-***` 401. _(awaiting manual run)_
- [x] No event count change end-to-end ✅ — events emitted with original `\n\n` framing preserved
- [x] Streaming latency bounded by the holdback window ✅ — events outside the horizon emit on the same `Read()` that brings them in

---

## Implementation Notes

- ✅ New file: `internal/runtime/sse_delta.go` — `sseEvent` struct, `parseSSEEvent`, `virtualBuffer`, `rewriteSurrogates`.
- ✅ `internal/runtime/sse.go` — rewritten to use event-aware buffering with `maxSurrogateLen` holdback window; non-text events pass through unchanged.
- ✅ `vault.Session.Surrogates()` (added in earlier commit) is what the holdback queries.
- ✅ Standard lib only — `encoding/json`, `bytes`, `strings`, `io`.
- The earlier byte-level holdback (`holdbackBytes`) was removed when the new path landed.
- Future: add a regression test using a captured real Anthropic streaming fixture in `tests/go/fixtures/anthropic-sse/` (synthetic deltas in current tests already cover the algorithmic case).

---

## Verification

- `task test-acceptance` passes including the two now-unskipped tests.
- `task test-non-disruption` (when implemented per Slice 04) still passes.
- E2E: with proxy running, `claudeproxy 'use this key sk-proj-... and curl OpenAI'` works end-to-end.
