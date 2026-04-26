# Spec: JSON-Aware Masking (Lazy Parse)

**Status:** Planned  
**Priority:** High  
**Depends on:** Current regex fix (merged)

---

## Problem

The proxy scans raw JSON bytes with regex. JSON encodes special characters as escape sequences (`\"`, `\\`, `\n`, `\uXXXX`). Regex patterns designed for plain text can:

- Match across escape boundaries (the `\[` bug, now patched defensively)
- Miss secrets whose values contain JSON-escaped chars (e.g. a password with `"` in it becomes `\"` in JSON — the pattern stops at `\` and may not capture the full value)
- Over-match across `\n` line separators, consuming multiple `.env` lines as one hit

The current regex fix adds `\` to exclusion sets. This is correct and safe, but it means any secret whose value contains a backslash-encoded character in JSON will be missed if the backslash appears early in the value.

---

## Proposed Approach: Scan-First, Decode-Only-If-Hit

**Do not unconditionally JSON-parse every request body.** Most requests are clean (no secrets). Parsing JSON for every request adds latency and complexity for no benefit.

### Algorithm

```
1. Run regex scanner on raw bytes (existing fast path)
2. If maskedCount == 0 → return original body unchanged (no JSON work)
3. If maskedCount > 0 AND Content-Type is application/json:
   a. JSON-decode the body
   b. Walk all string leaf values in the JSON tree
   c. Re-scan each decoded string value with the secret scanner
   d. For each new match found in the decoded string:
      - Mask the decoded value → get placeholder
      - JSON-encode the placeholder back into the string node
   e. Re-serialise the JSON tree → new body
4. Return body from step 3 (or step 1 if not JSON)
```

The first scan (step 1) catches most secrets. Step 3 only runs when the first scan already found something, meaning the request is "interesting." The incremental cost of JSON parsing only applies to requests that already needed work.

### Why This Is Safe

- If the body is not valid JSON, step 3 is skipped entirely (graceful fallback to current behaviour)
- If JSON parsing fails for any reason, return the step-1 result (already masked raw bytes)
- The two-pass approach means raw-byte patterns still run and catch API keys, tokens etc. that don't need JSON context

---

## Scope

### In scope
- `internal/runtime/service.go` `HandleRequest` — add JSON decode/re-scan/re-encode pass
- New `internal/codec/json.go` — JSON tree walker that yields and replaces string leaf values
- Unit tests: JSON body with escaped quotes, backslash values, unicode escapes, nested objects, arrays
- Benchmark: verify p99 latency does not regress for clean requests (no secrets)

### Out of scope
- SSE and WebSocket bodies (streamed, not full JSON objects) — handled separately
- gzip-encoded bodies — already decoded before this step (`DecodeBody`)
- Binary content types

---

## Interface Sketch

```go
// codec/json.go

// WalkJSONStrings calls fn for each string value in the JSON tree.
// fn receives the decoded string and returns the replacement.
// If fn returns the same string, the node is unchanged.
// Returns the re-serialised JSON and whether any node was modified.
func WalkJSONStrings(data []byte, fn func(s string) string) ([]byte, bool, error)
```

Usage in `service.go`:

```go
func (s *Service) HandleRequest(...) ([]byte, int, error) {
    text := codec.DecodeBody(body, headers.Get("Content-Encoding"))
    masked, maskedCount, patterns := s.maskText(sessionID, text)
    if maskedCount == 0 {
        return body, 0, nil
    }
    // First pass already found secrets. If JSON, do a second decoded-string pass.
    if isJSONContent(headers) {
        enriched, changed, err := codec.WalkJSONStrings([]byte(masked), func(s string) string {
            remasked, n, _ := s.maskTextDirect(sessionID, s)
            maskedCount += n
            return remasked
        })
        if err == nil && changed {
            return codec.EncodeBody(string(enriched), headers.Get("Content-Encoding")), maskedCount, nil
        }
    }
    return codec.EncodeBody(masked, headers.Get("Content-Encoding")), maskedCount, nil
}
```

---

## Test Cases Required

| Scenario | Expected |
|---|---|
| Clean request (no secrets) | Raw bytes returned unchanged, no JSON parsing |
| Secret in plain value (`password=abc123`) | Found in pass 1, body valid JSON after masking |
| Secret in value with escaped quote (`PASSWORD="abc123"`) | Missed by pass 1 (stops at `\`), found in pass 2 (decoded string `abc123`) |
| Secret in value with backslash (`PASS=C:\secret`) | Decoded string `C:\secret` scanned in pass 2 |
| Secret in nested JSON object inside string (`{"key":"sk-ant-..."}`) | Decoded string re-scanned, secret found |
| Non-JSON Content-Type with secrets | Pass 1 only, no JSON work |
| Invalid JSON body | Pass 1 result returned, no crash |
| Valid JSON, large body (>100KB) | Benchmark: p99 < 5ms overhead vs current |

---

## Acceptance Criteria

- [ ] All existing 45 tests still pass
- [ ] New tests for pass-2 scenarios above all pass
- [ ] Clean-request benchmark shows < 0.1ms overhead (no JSON parse triggered)
- [ ] `PASSWORD="value"` in JSON body: secret is masked in pass 2, body remains valid JSON
- [ ] Round-trip: masked body restored to original after `HandleResponse`
