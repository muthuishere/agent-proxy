# AgentProxy Testing Scenarios

## Purpose

This document captures the current validated behavior, regression history, and scenario backlog for AgentProxy so future agents can resume testing without rediscovering prior work.

## Project Scope

AgentProxy is a local `mitmproxy`-based proxy that intercepts AI client traffic, masks secrets before they leave the machine, and restores masked placeholders on the way back so the client still behaves normally.

Primary code paths:

- `proxy/addon.py`: request, response, and websocket masking pipeline
- `proxy/scanner.py`: regex scanner with configurable worker count
- `proxy/decoder.py`: base64, hex, and URL-encoded decoding and re-encoding
- `proxy/vault.py`: reversible placeholder store
- `config/patterns.yaml`: default secret patterns
- `config/pii_patterns.yaml`: optional PII patterns
- `tests/e2e_claude_test.sh`: live Claude end-to-end verification

## Current Known-Good State

Validated locally as of `2026-03-27`.

### Working

- Claude request/response flow through `bin/claudeproxy`
- Codex request/response flow through `bin/codexproxy`
- Codex websocket path with proper CA trust
- Claude end-to-end masking for:
  - plain connection strings
  - base64-encoded secrets
  - URL-encoded secrets
  - OpenAI tokens
  - GitHub tokens
  - AWS access key ids
  - AWS secret access keys
  - AWS session tokens
  - OpenSSH private key blocks
- Optional backend-controlled PII masking for:
  - email
  - phone
  - SSN
  - credit card
  - IPv4 address

### Fixed During This Pass

- Removed insecure TLS bypass from CLI wrappers and switched to CA-based trust
- Fixed URL-encoded secret masking corrupting JSON payloads
- Fixed connection-string regex overmatching into JSON escape sequences
- Fixed OpenAI `sk-proj-...` detection
- Added stronger AWS credential detection, including quoted and JSON-escaped env/export forms
- Added multiline OpenSSH private key block detection
- Added configurable threaded scanning
- Split PII into a separate config-gated scanner instead of treating it as default secret masking

## Test Commands

### Core unit/integration

```bash
uv run pytest tests/ -v
```

### Focused scanner/addon checks

```bash
uv run pytest tests/test_scanner.py tests/test_addon.py tests/test_decoder_encoded.py -v
```

### Claude end-to-end

```bash
bash tests/e2e_claude_test.sh
```

or:

```bash
task test-e2e-claude
```

## Required Regression Scenarios

These scenarios should remain green before testing new clients or changing masking logic.

### Secret masking

- Anthropic API key in JSON request body
- OpenAI API key in JSON request body
- OpenAI `sk-proj-...` key
- GitHub token in prompt body
- AWS access key id in plain text
- AWS access key id in assignment form
- AWS secret access key in assignment form
- AWS secret access key in quoted export form
- AWS secret access key in JSON-escaped export form
- AWS session token in assignment form
- database connection string in plain text
- database connection string inside JSON without leaking into escape sequences
- OpenSSH private key multiline block

### Encoded content

- base64-encoded connection string
- base64-encoded dotenv block
- hex-encoded secret
- URL-encoded secret
- nested encoded content up to configured decode depth
- encoded masking must preserve valid JSON body structure

### Transport behavior

- plain HTTP request body
- gzip-compressed request body
- intercepted response restoring vault placeholders
- websocket client-to-server masking
- websocket server-to-client restoration

### Domain interception

- listed AI domains are intercepted
- unlisted domains pass through untouched
- local gateway host with port only works if implementation supports port-sensitive interception

### PII behavior

- PII disabled: email/phone/SSN remain untouched
- PII enabled: only selected entities are masked
- PII and secret masking can coexist in the same body
- encoded blobs containing enabled PII entities are masked if supported

## Claude Scenarios To Re-Run

These are the current Claude-first acceptance scenarios.

1. Plain prompt with a raw Postgres URL
2. Prompt with dotenv block containing:
   - `DATABASE_URL`
   - `OPENAI_API_KEY`
   - `AUTH_TOKEN`
   - `AWS_ACCESS_KEY_ID`
   - `AWS_SECRET_ACCESS_KEY`
   - `AWS_SESSION_TOKEN`
3. Prompt containing base64 and URL-encoded copies of the Postgres URL
4. Long AGENTS-style prompt with noisy YAML, shell exports, and an OpenSSH private key block
5. Verify Claude still returns the exact expected response text
6. Verify `logs/traffic.jsonl` contains masked placeholders and no raw secrets

## Codex Scenarios To Re-Run

1. HTTPS request flow through `bin/codexproxy`
2. Websocket upgrade flow to `chatgpt.com/backend-api/codex/responses`
3. Prompt containing a raw Postgres URL
4. Prompt containing AWS credentials
5. Prompt containing an OpenSSH private key block
6. Verify no `UnknownIssuer` or TLS fallback behavior appears
7. Verify masked websocket client frames in `logs/traffic.jsonl`

## Pending Client Coverage

These were intentionally deferred and should be treated as future work:

- plain Copilot real completion host coverage
- additional local gateways
- other CLI wrappers or SDK clients

## High-Risk Areas

These areas need extra care when modifying the implementation:

- encoded blob masking in `proxy/decoder.py`
- regex broadening that can break JSON structure
- reversible restore behavior in `proxy/vault.py`
- websocket frame handling
- long-lived vault retention and cross-request secret lifetime
- mismatch between documented behavior and what is actually implemented

## Open Gaps

These are not solved just by the current green tests:

- encoded-secret masking still uses inline `[MASKED:...]` markers instead of full vault restoration semantics
- vault lifetime is process-wide and not request-scoped
- Copilot coverage is not complete
- PII is regex-only today; contextual PII detection is not implemented
- streaming/SSE chunk-split placeholder restoration is still a known limitation

## Recommended Next Steps

1. Add Claude e2e variants for PII `enabled: false` and `enabled: true`
2. Add Codex e2e with AWS and OpenSSH payloads
3. Add request-scoped or session-scoped vault eviction rules
4. Decide whether encoded-secret masking should become fully reversible
5. Add Copilot real-host interception and e2e once Claude and Codex are stable

