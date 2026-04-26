# Spec: Dashboard Traffic Body Pairs

**Status:** Implemented
**Priority:** High
**Depends on:** `internal/runtime`, `internal/logger`, `internal/ui`

---

## Problem

The dashboard currently shows aggregate request rows and pattern hits, but it does not let a developer inspect the proxy transformation itself. For debugging masking and restoration, developers need to compare:

- inbound request body
- outbound modified request body
- inbound upstream response body
- outbound modified response body returned to the agent

The current logger also writes synchronously on the request path, which is not acceptable for detailed body logging.

---

## Goals

- Add traffic event fields for original and modified request/response bodies.
- Keep raw original bodies gated by `logging.log_originals` because they can contain secrets.
- Keep transformed bodies available for dashboard/debugging by default.
- Move disk/event logging off the hot path through an asynchronous logger queue.
- Let the dashboard expand a row to inspect available body fields.

---

## Non-Goals

- No persistent request/response correlation store beyond the current JSONL event stream.
- No fuzzy diff view in this pass.
- No full SSE body buffering; streaming SSE responses may continue to use a stream marker.
- No removal of the existing metrics and pattern summary.

---

## Functional Requirements

### BODY-1 — Request body pairs

Request events include:

- `request_body`: decoded inbound body, only when `logging.log_originals` is true.
- `modified_request_body`: decoded body that AgentProxy forwards upstream after masking.

When no masking happens, `modified_request_body` may equal the inbound decoded body.

### BODY-2 — Response body pairs

Response events include:

- `response_body`: decoded upstream response body, only when `logging.log_originals` is true.
- `modified_response_body`: decoded body returned to the local agent after restoration.

When no restoration happens, `modified_response_body` may equal the upstream decoded body.

### BODY-3 — WebSocket frame pairs

WebSocket events use the same paired fields:

- client-to-server frames populate request fields.
- server-to-client frames populate response fields.

### BODY-4 — Asynchronous logging

Traffic logging enqueues events to a buffered background writer. Request/response processing must not block on normal disk writes or dashboard subscribers. If the queue is full, the logger may drop the event rather than blocking the proxy path.

### BODY-5 — Dashboard inspection

The dashboard row can be expanded to show available request/response body fields. Missing original fields should be labeled as disabled by `logging.log_originals=false`, not treated as errors.

---

## Acceptance Criteria

- [x] Request events contain `modified_request_body`.
- [x] Request events contain `request_body` only when `logging.log_originals=true`.
- [x] Response events contain `modified_response_body`.
- [x] Response events contain `response_body` only when `logging.log_originals=true`.
- [x] WebSocket events populate the corresponding paired fields by direction.
- [x] Logger writes events through an asynchronous queue.
- [x] Dashboard rows expand to show the available body pair fields.
- [x] Existing metrics still update for request and WebSocket events.
- [x] `go test ./internal/logger ./internal/runtime ./internal/ui ./tests/go` passes.
- [x] `go test ./...` passes.
