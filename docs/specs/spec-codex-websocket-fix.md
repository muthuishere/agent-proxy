# Spec: Codex WebSocket Fix (HTTP/2 → HTTP/1.1 Upstream for WebSocket)

**Status:** Closed — Option A shipped  
**Decision: Option A — DONE 2026-04-25** (force HTTP/1.1 on the upstream WebSocket
transport via empty `TLSNextProto`). See `internal/provider/openai.go`
(`NewOpenAIProvider` constructs `wsTransport` with
`TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}`) and
`internal/provider/provider_test.go::TestOpenAIWSTransportIsHTTP1Only`.
The proxy server selects `WSTransport()` for WebSocket-bearing requests via the
`WebSocketProvider` interface, so RFC 8441 (Option B) is unnecessary today.
Tracked separately if any upstream ever refuses HTTP/1.1 for WebSocket.

**Priority:** P0 (Codex completely non-functional)  
**Owner library:** `third_party/goproxywss` (vendored — we own this code)

---

## Problem

Codex connects to `chatgpt.com/backend-api/codex/responses` via WebSocket.

The live log shows two distinct failures:

```
WARN: Cannot read request from mitm'd client chatgpt.com:443  — connection reset by peer
WARN: Unable to use Websocket connection
```

### Root cause: HTTP/2 upstream breaks WebSocket tunneling

`chatgpt.com` negotiates HTTP/2 (h2) via ALPN during the TLS handshake. Go's standard `http.Transport` respects this and sends the request over an HTTP/2 stream.

In goproxywss `https.go:352`:

```go
if isWebSocketHandshake(resp.Header) {
    wsConn, ok := resp.Body.(io.ReadWriter)
    if !ok {
        ctx.Warnf("Unable to use Websocket connection")  // ← fires here
        return false
    }
    proxy.proxyWebsocket(ctx, wsConn, client)
}
```

Go's HTTP/1.1 transport exposes the underlying `net.Conn` via `resp.Body.(io.ReadWriter)` on a 101 response. HTTP/2 transport does **not** — the body is a stream abstraction that cannot be hijacked this way.

WebSocket over HTTP/2 uses RFC 8441 (Extended CONNECT), a fundamentally different handshake, which goproxywss does not implement.

### The fix

Since we **own** `third_party/goproxywss`, we have two options:

**Option A (targeted, low risk):** Force HTTP/1.1 for the upstream TLS connection when the request includes a `Connection: Upgrade` / `Upgrade: websocket` header. Disable HTTP/2 negotiation (empty `TLSNextProto`) on the transport used for that specific connection.

**Option B (proper, higher effort):** Implement RFC 8441 Extended CONNECT in goproxywss so WebSocket over HTTP/2 is natively tunneled.

**Decision: Option A first.** It unblocks Codex without a protocol implementation. Option B tracked separately.

**Status update (2026-04-25):** Option A is implemented. Rather than carrying a
per-server `WebSocketUpstreamTransport` field on goproxywss, the project
landed the override at the provider layer: a provider that implements
`provider.WebSocketProvider` returns an HTTP/1.1-only `*http.Transport` from
`WSTransport()`, and the proxy server dispatches WebSocket-bearing requests to
that transport. `OpenAIProvider` ships the empty `TLSNextProto` map. This is
simpler than the originally-sketched goproxywss change and keeps transport
policy co-located with the provider that owns the domain.

---

## Scope of Change

All changes are inside `third_party/goproxywss/`.

### `https.go` — detect WebSocket intent before dialing upstream

When handling a CONNECT tunnel and the first request from the MITM'd client is a WebSocket upgrade, force HTTP/1.1 on the upstream transport for that connection:

```go
// Before making the upstream request, check if this is a WebSocket upgrade.
// If yes, disable HTTP/2 on the upstream transport so resp.Body.(io.ReadWriter) works.
func isWebSocketUpgradeRequest(req *http.Request) bool {
    return headerContains(req.Header, "Connection", "Upgrade") &&
        headerContains(req.Header, "Upgrade", "websocket")
}
```

In the MITM request loop, when `isWebSocketUpgradeRequest(req)` is true, use a transport with `TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}` (empty map forces HTTP/1.1 only).

The existing transport used for regular HTTPS requests is unchanged.

### `proxy.go` — expose per-request transport override hook

Add a field:
```go
// WebSocketUpstreamTransport, if non-nil, is used instead of Tr for
// requests that contain a WebSocket upgrade header. Set this to a
// transport with TLSNextProto empty to force HTTP/1.1 for WebSocket
// upstream connections.
WebSocketUpstreamTransport http.RoundTripper
```

### `internal/proxy/server.go` — wire up the override

```go
handler := goproxy.NewProxyHttpServer()

// Force HTTP/1.1 for WebSocket upstream connections so goproxywss can
// hijack resp.Body for tunneling. chatgpt.com (used by Codex) negotiates
// HTTP/2 by default which breaks the io.ReadWriter cast on 101 responses.
handler.WebSocketUpstreamTransport = &http.Transport{
    TLSClientConfig: &tls.Config{},
    TLSNextProto:    make(map[string]func(string, *tls.Conn) http.RoundTripper), // empty = HTTP/1.1 only
}
```

---

## Test Plan

### Unit tests (`third_party/goproxywss/`)

| Test | Description |
|---|---|
| `TestIsWebSocketUpgradeRequest` | Detects upgrade headers correctly |
| `TestWebSocketUsesHTTP1Transport` | When upgrade detected, WebSocket transport selected |
| `TestRegularRequestUsesDefaultTransport` | Non-WebSocket requests unaffected |

### Acceptance tests (`tests/go/`)

| Test | Description |
|---|---|
| `TestHandleWebSocket_ChatGPTDomain` | `HandleWebSocket` intercepts on `chatgpt.com` |
| `TestHandleWebSocket_SecretInFrame` | Secret in WebSocket text frame is masked |
| `TestHandleWebSocket_FrameWithEscapedChars` | Frame containing `\"` does not break masking |
| `TestHandleWebSocket_MultiFrameSequence` | Three client→server frames all masked correctly |
| `TestHandleWebSocket_ServerFrameRestored` | Server→client frame with vault token is restored |

### E2E manual verification

```bash
codexproxy "explain what this function does"
# Expected: proxy intercepts WebSocket frames, secrets masked, Codex responds normally
```

---

## Acceptance Criteria

- [ ] `codexproxy` launches Codex and traffic flows through the proxy without "Unable to use Websocket connection"
- [ ] WebSocket frames to `chatgpt.com` are intercepted and secrets are masked
- [ ] Regular HTTPS (non-WebSocket) requests continue using HTTP/2 where available (no regression)
- [ ] All existing 45 tests pass
- [ ] New WebSocket tests above all pass
- [ ] `api.openai.com` SSE-based responses also continue to work (Codex may use both)
