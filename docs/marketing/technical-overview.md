# AgentProxy: Technical Overview

This document is for engineers evaluating AgentProxy or integrating it into a toolchain. It covers the full request/response lifecycle, the masking and restore mechanism, the encoding pipeline, and the runtime architecture.

---

## Traffic interception

AgentProxy is an HTTP/HTTPS MITM proxy built on a vendored fork of [`elazarl/goproxy`](https://github.com/elazarl/goproxy) with WebSocket frame hook support (`goproxywss`). It listens on `127.0.0.1:7717` by default.

Agents route through it by setting `HTTP_PROXY`/`HTTPS_PROXY` environment variables. HTTPS connections are intercepted via a local CA certificate. goproxy issues per-host TLS certificates signed by this CA on the fly, caching them in `./certs/`.

Only domains listed under `detection.intercepted_domains` in `config/agentproxy.yaml` are intercepted. All other traffic is forwarded untouched.

---

## Request lifecycle

```
Agent sends POST /v1/messages
  │
  ├── Domain check
  │     Not in intercepted_domains? → forward as-is
  │
  ├── Body decode
  │     gzip / deflate → decompress
  │     Raw bytes → read body
  │
  ├── Encoded blob scan  (internal/codec)
  │     Detect base64, hex, URL-encoded, nested JSON string blobs
  │     Decode each blob up to depth 4
  │     Scan decoded content for secrets
  │     Re-encode blob with vault tokens embedded
  │
  ├── Top-level secret scan  (internal/scanner)
  │     Parallel regex scan across all loaded patterns
  │     Longest-match-first at each position (avoids partial masking)
  │
  ├── Mask matches → vault  (internal/vault)
  │     Each unique secret value → deterministic token
  │       sk-ant-api03-abc123...  →  sk-an****...****3[ANTHROPIC_API_KEY:a3f2]
  │     Token stored in session-scoped vault
  │
  ├── Re-encode body
  │     Masked body written back (gzip if original was gzip)
  │
  └── Forward to upstream API
```

---

## Response lifecycle

```
API response received
  │
  ├── Domain check  (same gate)
  │
  ├── SSE detection
  │     Content-Type: text/event-stream?
  │       Yes → SSE restore path (streaming, per-event)
  │       No  → full-buffer restore path
  │
  ├── [SSE path]  sseRestoreReader  (internal/runtime/sse.go)
  │     Wraps response body as io.ReadCloser
  │     Buffers until \n\n (SSE event boundary)
  │     Per event: Restore vault tokens → RestoreEncodedBlobs
  │     Streams each event back to agent immediately (no full-buffer wait)
  │
  ├── [Standard path]
  │     Read full body
  │     Restore vault tokens
  │     Restore encoded blobs
  │
  └── Return to agent
```

---

## Secret masking format

Masked secrets use a structured placeholder:

```
{prefix}****...****{suffix}[{PATTERN_NAME}:{short_hash}]
```

Example:

```
sk-ant-api03-abc123xyz...  →  sk-an****...****3xyz[ANTHROPIC_API_KEY:a3f2]
```

Fields:
- `prefix`: first N characters of the original value (configurable, default 4)
- `suffix`: last N characters of the original value (configurable, default 4)
- `****...****`: fixed-width star block (configurable, default 24 stars)
- `PATTERN_NAME`: the pattern that matched (e.g. `ANTHROPIC_API_KEY`, `PRIVATE_KEY_BLOCK`)
- `short_hash`: 4-character hex hash of the original value — the vault lookup key

The token is designed to be recognisable to humans in logs while being unambiguous for restoration. The hash makes restoration exact even if two secrets share the same prefix/suffix.

---

## Session-scoped vault

The vault (`internal/vault/session.go`) is keyed by session ID — one vault instance per TLS connection. This means:

- Tokens from one request cannot leak into another connection's restore pass
- Memory is freed deterministically when the session ends (no TTL required)
- Concurrent sessions are isolated without locking overhead between them

Session IDs are derived from the goproxy connection context. The vault manager holds a sync.Map of session → vault.

---

## Encoded blob detection and masking

Many AI API payloads embed encoded content: base64-encoded file contents, hex-encoded configs, JSON-stringified nested objects. Secrets inside these blobs would be invisible to a top-level regex scan.

The codec pipeline (`internal/codec/`) handles this:

1. **Detect encoding** — test each candidate substring for valid base64, hex, URL-encoding, or JSON string
2. **Decode** — recursively, up to depth 4
3. **Scan decoded content** — run the same secret scanner on the decoded bytes
4. **Mask in place** — replace secret values with vault tokens inside the decoded bytes
5. **Re-encode** — re-apply the original encoding so the blob remains structurally valid

Strategy: `EmbeddedTokens` — the vault tokens are embedded inside the re-encoded blob. When the response comes back containing that blob, the same decode → find token → restore → re-encode path runs in reverse.

---

## SSE streaming and the data-boundary split problem

Claude and OpenAI streaming APIs use Server-Sent Events. A naive implementation buffers the full response body before scanning, which defeats streaming and adds latency. A per-chunk scan misses secrets that span two chunks.

AgentProxy uses per-event buffering with semantic normalization:

1. Buffer until `\n\n` (the SSE event separator)
2. Within each event, concatenate all `data:` line payloads **without separator**
3. Scan the concatenated string

Step 2 solves the data-boundary split problem: if a secret spans two `data:` lines (e.g. line 1 ends with `sk-ant-api03-` and line 2 starts with `abc123...`), the concatenated string `sk-ant-api03-abc123...` is visible to the regex and gets masked.

The `sseRestoreReader` wraps the response body as an `io.ReadCloser` and streams each restored event back to the agent immediately — no full-body buffering. From the agent's perspective, it is receiving a normal streaming response.

---

## WebSocket support

WebSocket traffic is intercepted via the `WebSocketMessagePredicate` and `WebSocketMessageHandler` hooks in the vendored `goproxywss` library. Both text and binary frames are passed through the same scanner/vault/codec pipeline as HTTP request bodies.

WebSocket connections are session-scoped: the vault opened at the HTTP Upgrade request is reused for the lifetime of the WebSocket session and closed when the connection ends.

---

## Pattern matching

Patterns are loaded from `config/patterns.yaml`. Each pattern has:

```yaml
- name: ANTHROPIC_API_KEY
  regex: 'sk-ant-api[0-9]{2}-[A-Za-z0-9_\-]{86,}'
  type: api_key
  confidence: high
```

At startup, patterns are compiled to `*regexp.Regexp` and sorted by length (longest first). The scanner runs all patterns in parallel using a worker pool (`scan_workers` in config, default 4).

Match deduplication: if two patterns match overlapping ranges, the longer (containing) match wins. The `sortMatches` function enforces longest-match-first at the same start position — this prevents a short sub-pattern (e.g. `PRIVATE_KEY_HEADER` matching just the `-----BEGIN...` line) from firing before a long pattern (e.g. `PRIVATE_KEY_BLOCK` matching the full PEM block).

---

## PII masking

PII patterns are loaded from a separate file (`config/pii_patterns.yaml`) and run through the same scanner/vault/restore pipeline. PII masking is disabled by default and opt-in per entity type:

```yaml
pii:
  enabled: true
  entities:
    - email
    - phone
    - credit_card
```

PII tokens restore in responses alongside secret tokens — they use the same vault and the same structured placeholder format.

---

## Logging

Every intercepted request and response is appended to `logs/traffic.jsonl` as newline-delimited JSON:

```json
{"event":"request","method":"POST","host":"api.anthropic.com","path":"/v1/messages","masked_count":2,"ts":"2026-03-27T10:00:00Z"}
{"event":"response","host":"api.anthropic.com","path":"/v1/messages","status":200,"masked_count":0,"ts":"2026-03-27T10:00:01Z"}
```

`masked_count` on a request is the number of secrets masked. `masked_count: 0` on a request confirms the prompt was clean.

`log_originals: false` (default) — pre-masking bodies are never written to disk. Set to `true` only for debugging; treat the log as sensitive.

---

## Upstream proxy chaining

If your machine routes outbound traffic through a corporate proxy, configure it under `upstream_proxy`. AgentProxy chains through it:

```
Agent → AgentProxy → corporate proxy → internet → AI API
```

`auto_detect: true` (default) reads `HTTP_PROXY`/`HTTPS_PROXY` from environment and macOS/Windows system proxy settings automatically.

---

## Runtime configuration

All configuration lives in `config/agentproxy.yaml`. Key sections:

| Section | Purpose |
|---|---|
| `proxy.port` / `proxy.host` | Listening address (default `127.0.0.1:7717`) |
| `detection.intercepted_domains` | Domains to intercept; all others pass through |
| `detection.pattern_file` | Path to secret patterns YAML |
| `detection.scan_workers` | Parallel scanner goroutines |
| `masking.*` | Prefix/suffix char counts and star length |
| `pii.*` | PII masking toggle, pattern file, entity list |
| `logging.*` | Log file path, originals toggle, passthrough toggle |
| `upstream_proxy.*` | Corporate proxy chaining |

Run `agentproxy validate` to verify config and file path resolution on startup. Run `agentproxy config` to print the loaded config summary.

---

## Project layout

```
internal/
  config/     config loader, startup validation
  scanner/    parallel regex scanner, match deduplication, sortMatches
  codec/      body decode/encode, encoded blob scan/restore, EmbeddedTokens
  vault/      session-scoped vault, VaultManager, Restorer interface
  logger/     JSONL traffic logger
  runtime/    Service (wires scanner + vault + codec + logger), sseRestoreReader
  proxy/      goproxywss MITM server, OnRequest/OnResponse/WebSocket handlers

cmd/agentproxy/
  main.go     CLI dispatch (start, validate, config, ca-setup)
  casetup.go  CA cert export, OS trust (macOS: security, Windows: certutil)

third_party/goproxywss/
              vendored goproxy fork with WebSocket message hook API

go-spikes/
  sse/        SSE buffering proofs (NormalizeEventData, per-event vs full-buffer)
  encodedblob/ EmbeddedTokens prototype
  vault/      TTL vs session-scoped vault benchmarks
  ws/         WebSocket soak test

tests/go/
  masking_acceptance_test.go  25 CI-runnable tests, no live API required
```
