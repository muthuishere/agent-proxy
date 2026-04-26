# AgentProxy — Product Specification

## What it is

AgentProxy is a **local MITM (man-in-the-middle) HTTPS proxy** that sits between AI coding agents (Claude Code, Codex, GitHub Copilot) and their remote APIs. It intercepts every outbound request, scans the body for secrets and PII, masks them with reversible vault tokens before they leave the machine, and restores original values in responses so the AI agent never notices.

Zero cloud dependency. No code changes to agents. Runs as a single static binary.

---

## Why we built it

AI coding agents have access to the full codebase, including:
- `.env` files with API keys, database passwords, connection strings
- Source files referencing credentials
- Shell histories, config files, secrets in comments

When a developer asks an agent to "explain this .env file" or "debug this database issue", the entire secret-containing content is sent verbatim to the AI provider's API (Anthropic, OpenAI, GitHub). Those secrets leave the machine in plaintext.

**The risk:** Secrets accumulate in AI provider logs, training datasets, and telemetry — permanently outside the developer's control.

---

## How it works

```
AI Agent (Claude/Codex/Copilot)
    │  sets HTTP_PROXY=http://127.0.0.1:7717
    ▼
AgentProxy (port 7717)
    ├─ TLS MITM: per-host cert signed by local CA
    ├─ Decode body (gzip)
    ├─ Scan for secrets (22 regex patterns, worker pool)
    ├─ Scan for secrets inside base64/hex/URL-encoded blobs (depth 4)
    ├─ Mask matches → reversible vault token per session
    ├─ Forward masked request to real API
    │
    ▼
Remote AI API (api.anthropic.com / api.openai.com / etc.)
    │
    ▼
AgentProxy (response path)
    ├─ Detect SSE stream → per-event token restoration
    ├─ Detect WebSocket frame → per-frame restoration
    ├─ Restore vault tokens → original secrets
    └─ Forward restored response to agent
    ▼
AI Agent sees normal response
```

**Vault token format:**  
`{prefix 4 chars}{"*" × 24}{suffix 4 chars}[{PATTERN_NAME}:{sha256 4-byte hex}]`  
Example: `sk-an************************3x7f[ANTHROPIC_API_KEY:a3f2d1c0]`

Tokens are session-scoped (one per HTTP connection), deterministic (same secret → same token in session), and reversible (restoration replaces token with original in responses).

---

## Supported providers

| Provider | Domains intercepted | Transport |
|---|---|---|
| Anthropic / Claude Code | `api.anthropic.com` | HTTPS + SSE |
| OpenAI / Codex | `api.openai.com`, `chatgpt.com` | HTTPS + SSE + WebSocket |
| GitHub Copilot | `api.githubcopilot.com`, `api.individual.githubcopilot.com`, `copilot-proxy.githubusercontent.com`, `api.github.com` | HTTPS |

Wrapper scripts (`claudeproxy`, `codexproxy`, `copilotproxy`) set `HTTP_PROXY`, `HTTPS_PROXY`, and CA bundle env vars, then exec the agent binary with all arguments forwarded.

---

## Secret patterns (22 built-in)

| Pattern | Example | Notes |
|---|---|---|
| `ANTHROPIC_API_KEY` | `sk-ant-api03-...` | |
| `OPENAI_API_KEY` | `sk-proj-...` | |
| `AWS_ACCESS_KEY` | `AKIA...` | bare key |
| `AWS_ACCESS_KEY_ASSIGNMENT` | `aws_access_key_id=AKIA...` | quoted or unquoted |
| `AWS_SECRET_KEY` | `aws_secret_access_key=wJalr...` | 40-char base64 |
| `AWS_SESSION_TOKEN` | `aws_session_token=IQoJ...` | variable length |
| `GOOGLE_API_KEY` | `AIzaSy...` | |
| `GITHUB_TOKEN` | `ghp_...` / `ghs_...` | |
| `GITLAB_TOKEN` | `glpat-...` | |
| `HUGGINGFACE_TOKEN` | `hf_...` | |
| `STRIPE_KEY` | `sk_live_...` | |
| `SENDGRID_KEY` | `SG....` | |
| `JWT_TOKEN` | `eyJ...` | |
| `PRIVATE_KEY_BLOCK` | `-----BEGIN ... PRIVATE KEY-----` | full block |
| `PRIVATE_KEY_HEADER` | `-----BEGIN ... PRIVATE KEY-----` | header only |
| `GENERIC_CONNECTION_STRING` | `postgres://user:pass@host/db` | 7 protocols |
| `ENV_SECRET_ASSIGNMENT` | `password=...` | keyword=value pairs |
| `DOTENV_ASSIGNMENT` | `MY_SECRET=...` | `^KEY_PATTERN=value` |

---

## Known issues being debugged

### 1. Codex (OpenAI) WebSocket not working

**Symptoms (from live log):**
```
WARN: Cannot read request from mitm'd client chatgpt.com:443 — connection reset by peer
WARN: Unable to use Websocket connection
```

**Root cause analysis:**
Codex connects to `chatgpt.com/backend-api/codex/responses` via WebSocket. The goproxywss library handles WebSocket upgrades by checking if `resp.Body` implements `io.ReadWriter` after a 101 Switching Protocols response. This only works for HTTP/1.1 upstream connections. If the upstream negotiates HTTP/2 (which `chatgpt.com` does), the `resp.Body` does not implement `io.Writer`, and the proxy logs "Unable to use Websocket connection".

**Also:** TLS handshake failures (`EOF`) suggest the CA cert may not be trusted by Codex's Node.js runtime in some configurations. Check `NODE_EXTRA_CA_CERTS` is set before launching Codex.

**Status:** Unresolved. Needs investigation into forcing HTTP/1.1 for WebSocket upstream connections in goproxywss.

### 2. Masking creates invalid JSON — fixed ✅

**Symptoms:** After proxy masks a request containing `.env` file content, Anthropic API returns:
```
400 invalid_request_error: The request body is not valid JSON: 
invalid escaped character in string: line 1 column 50827
```

**Root causes (two bugs):**

**Bug A — `\[` in output:**  
`ENV_SECRET_ASSIGNMENT` regex `[^\s"'\n]{6,}` did not exclude backslash. In JSON, a value like `PASSWORD=myvalue"next` is encoded as `PASSWORD=myvalue\"next`. The regex matched `myvalue\` (including the `\` before stopping at `"`). The placeholder suffix ended with `\`, producing `\[PATTERN:hash]` — an invalid JSON escape sequence.

**Bug B — orphaned `"` closes string:**  
AWS patterns used `(?:\\?["''])?` (optional backslash + quote). In a raw JSON body, the final `"` of the JSON string value is a bare `"`. The optional group matched and consumed it. The placeholder then appeared where the closing `"` should be, breaking the JSON string boundary.

**Fix applied:**
1. `ENV_SECRET_ASSIGNMENT` regex: `[^\s"''\n]{6,}` → `[^\s"''\\]{6,}` (exclude `\`)
2. `DOTENV_ASSIGNMENT` regex: `.{6,}` → `[^\n\\]{6,}` (exclude `\`)
3. AWS patterns: `(?:\\?["''])?` → `(?:\\["''])?` (require backslash before quote)
4. `makePlaceholder`: strip `\` from prefix/suffix as defense-in-depth

45 tests pass, including 20 new JSON-validity regression tests.

---

## Architecture decisions

- **Pure Go, single binary** — no Python/mitmproxy dependency, no runtime installation
- **Session-scoped vault** — memory bounded by concurrent connections, not request volume
- **SSE per-event restoration** — streaming latency preserved; events buffered at `\n\n` boundary
- **WebSocket per-frame handling** — opcode 0x1 (text frames) only
- **Encoded blob scanning** — base64/hex/URL up to depth 4, EmbeddedTokens strategy (preserves non-secret parts)
- **Domain allowlist** — only listed domains are intercepted; everything else passes through untouched

---

## Configuration

`config/agentproxy.yaml` — key fields:

```yaml
proxy:
  port: 7717
  host: 127.0.0.1
detection:
  intercepted_domains:
    - api.anthropic.com
    - api.openai.com
    # add localhost:4000 for LiteLLM, etc.
masking:
  show_prefix_chars: 4
  show_suffix_chars: 4
  star_length: 24
logging:
  log_file: ./logs/traffic.jsonl
  log_originals: false   # NEVER set true in production
```

Dashboard: `http://127.0.0.1:7718` (live log, metrics, pattern breakdown)

---

## Quick start

```bash
# One-time setup
agentproxy ca-setup --trust

# Run proxy
agentproxy-start

# Use with Claude Code
claudeproxy "explain this .env file"

# Use with Codex (WebSocket issue under investigation)
codexproxy "refactor this function"
```
