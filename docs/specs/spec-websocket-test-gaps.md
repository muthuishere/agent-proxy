# Spec: WebSocket Test Gaps

**Status:** Mostly closed — 2026-04-25  
**Priority:** Medium (testing debt, blocks confidence in WebSocket masking)

## Audit — 2026-04-25

All ten tests listed below have been implemented in
`tests/go/masking_acceptance_test.go` (under the "WebSocket masking" header).
Confirmed present and passing:

| Test | Status |
|---|---|
| `TestHandleWebSocket_SecretMaskedInClientFrame` | DONE 2026-04-25 (`masking_acceptance_test.go`) |
| `TestHandleWebSocket_FrameRemainsValidAfterMask` | DONE 2026-04-25 |
| `TestHandleWebSocket_ServerFrameRestoresVaultToken` | DONE 2026-04-25 |
| `TestHandleWebSocket_MultiFrameSequence` | DONE 2026-04-25 |
| `TestHandleWebSocket_BinaryLikeContentPassedThrough` | DONE 2026-04-25 |
| `TestHandleWebSocket_LargeFrame` | DONE 2026-04-25 |
| `TestHandleWebSocket_ChatGPTDomainIntercepted` | DONE 2026-04-25 |
| `TestHandleWebSocket_SecretWithEscapedQuoteInFrame` | DONE 2026-04-25 |
| `TestHandleWebSocket_NoSecretNoMasking` | DONE 2026-04-25 |
| `TestHandleWebSocket_SessionIsolation` | DONE 2026-04-25 |

Codex-specific WebSocket coverage on `api.openai.com` added in
`tests/go/codex_shapes_test.go::TestCodexWebSocketRoundTrip` (DONE 2026-04-25).

Remaining open: a cross-frame surrogate-split test (parallel to
`TestOpenAIKey_SSE_SurrogateSplitAcrossEvents`) is **tracked as open** —
needs a new spec for OpenAI-shaped delta-aware WebSocket frame parsing.

---

## Problem

`HandleWebSocket` in `internal/runtime/service.go` calls `maskText` with the same scanner as HTTP requests. The masking fix (adding `\` exclusion to `ENV_SECRET_ASSIGNMENT`) applies equally to WebSocket frames — but there are **zero tests** that exercise this path.

Specifically, these scenarios are untested:

1. WebSocket text frame containing a secret — is it masked?
2. WebSocket frame where the secret value contains `\"` (JSON-escaped quote) — does masking produce a valid frame (no `\[` corruption)?
3. Multi-frame sequence — are all frames independently masked?
4. Server-to-client frame with a vault token — is it restored?
5. `chatgpt.com` domain is in the intercepted list — does `ShouldIntercept` return true for it?
6. Binary frames (opcode != 0x1) — are they passed through unchanged?
7. Large frame (>64KB payload) — no truncation or panic?

---

## Scope

New tests in `tests/go/masking_acceptance_test.go` using `svc.HandleWebSocket` directly. No proxy/network required.

---

## Test Cases

```go
// TestHandleWebSocket_SecretMaskedInClientFrame verifies that a secret in a
// client-to-server WebSocket frame is masked and the result does not contain
// the original secret.
func TestHandleWebSocket_SecretMaskedInClientFrame(t *testing.T)

// TestHandleWebSocket_FrameRemainsValidAfterMask verifies that after masking,
// the frame text does not contain \[ (the invalid-JSON-escape bug).
func TestHandleWebSocket_FrameRemainsValidAfterMask(t *testing.T)

// TestHandleWebSocket_ServerFrameRestoresVaultToken verifies that a
// server-to-client frame containing a vault token (from a previous masked
// request in the same session) has the token restored.
func TestHandleWebSocket_ServerFrameRestoresVaultToken(t *testing.T)

// TestHandleWebSocket_MultiFrameSequence verifies that three consecutive
// client frames are each independently masked.
func TestHandleWebSocket_MultiFrameSequence(t *testing.T)

// TestHandleWebSocket_BinaryFramePassedThrough verifies that a call to
// HandleWebSocket with a non-text payload (simulating opcode 0x2) is not
// masked (service layer does not mask binary-like frames).
// Note: actual binary frame filtering is in proxy/server.go (opcode check);
// this test covers the service layer only passing through non-matching content.
func TestHandleWebSocket_BinaryLikeContentPassedThrough(t *testing.T)

// TestHandleWebSocket_LargeFrame verifies no panic or truncation for a
// frame with a 128KB payload.
func TestHandleWebSocket_LargeFrame(t *testing.T)

// TestHandleWebSocket_ChatGPTDomainIntercepted verifies ShouldIntercept returns
// true for chatgpt.com (the Codex WebSocket domain).
func TestHandleWebSocket_ChatGPTDomainIntercepted(t *testing.T)

// TestHandleWebSocket_SecretWithEscapedQuoteInFrame verifies that a frame
// containing password=\"value\" (JSON-style quoted assignment) does not
// produce \[ in the masked output.
func TestHandleWebSocket_SecretWithEscapedQuoteInFrame(t *testing.T)

// TestHandleWebSocket_NoSecretNoMasking verifies that a clean frame (no
// secrets) is returned unchanged with maskedCount == 0.
func TestHandleWebSocket_NoSecretNoMasking(t *testing.T)

// TestHandleWebSocket_SessionIsolation verifies that vault tokens from one
// session are not visible in another session's restore path.
func TestHandleWebSocket_SessionIsolation(t *testing.T)
```

---

## Implementation Notes

`HandleWebSocket(sessionID, host, path, text string, fromClient bool) (string, int)`

- `fromClient=true` → masking path (same as request)
- `fromClient=false` → restore path (same as response)

The test helper pattern mirrors `maskRequest` and `roundTrip` already in the test file:

```go
func maskWebSocketFrame(t *testing.T, svc *agentruntime.Service, sid, host, text string, secrets []string) string {
    t.Helper()
    masked, count := svc.HandleWebSocket(sid, host, "/backend-api/codex/responses", text, true)
    if count == 0 {
        t.Fatalf("expected at least one masked secret in WebSocket frame, got 0\ntext: %s", text)
    }
    for _, s := range secrets {
        if strings.Contains(masked, s) {
            t.Fatalf("secret still present in masked WebSocket frame: %q", s[:min(len(s), 60)])
        }
    }
    return masked
}

func roundTripWebSocket(t *testing.T, svc *agentruntime.Service, sid, host, text string, secrets []string) {
    t.Helper()
    masked := maskWebSocketFrame(t, svc, sid, host, text, secrets)
    restored, _ := svc.HandleWebSocket(sid, host, "/backend-api/codex/responses", masked, false)
    if restored != text {
        t.Fatalf("WebSocket round-trip mismatch\nwant: %s\ngot:  %s", text, restored)
    }
}
```

---

## Acceptance Criteria

- [ ] All 10 new WebSocket tests implemented and passing
- [ ] All existing 45 tests still pass
- [ ] `TestHandleWebSocket_FrameRemainsValidAfterMask` explicitly checks for absence of `\[`
- [ ] `TestHandleWebSocket_ServerFrameRestoresVaultToken` uses the same session ID across mask and restore calls
- [ ] `TestHandleWebSocket_ChatGPTDomainIntercepted` confirms `chatgpt.com` is in the intercepted domain set
