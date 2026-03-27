# AgentProxy

A local MITM proxy that sits between AI coding agents (Claude, Codex, Copilot, etc.) and their remote APIs. It intercepts outbound traffic, detects secrets in prompts, masks them before they leave your machine, and restores them in the response — so the agent works normally and your secrets never hit the wire.

**Zero cloud dependency. Runs entirely on your machine. No code changes to your agents.**

---

## The problem

AI agents read your codebase. They see `.env` files, config files, shell history. If a secret ends up in a prompt — even by accident — it leaves your machine in plaintext and ends up in logs on someone else's server.

AgentProxy sits in the middle and makes that safe.

```
Before:  Claude → api.anthropic.com  (prompt contains sk-ant-api03-abc123...)
After:   Claude → AgentProxy → api.anthropic.com  (prompt contains sk-an****[ANTHROPIC_API_KEY:a3f2...])
```

The response comes back with the original value restored, so the agent sees what it expects.

---

## How it works

```
AI Agent
   │  HTTP_PROXY=http://127.0.0.1:7717
   ▼
AgentProxy (port 7717)
   │
   ├── Domain check ──── not in intercepted_domains? ──► pass through untouched
   │
   ├── Decode body (gzip / base64 / hex / URL-encoded)
   │
   ├── Scan for secrets (regex patterns)
   │        API keys, connection strings, JWTs, .env contents, private keys
   │
   ├── Mask secrets → vault tokens  (restorable in response)
   │        sk-ant-api03-abc123  →  sk-an****...****3[ANTHROPIC_API_KEY:a3f2]
   │
   ├── Re-encode body and forward request
   │
   ▼
AI API  (secrets never arrive as plaintext)
   │
   ▼
AgentProxy  (response)
   │
   └── Restore vault tokens → original values
   │
   ▼
AI Agent  (sees its data back intact)
```

Secrets are replaced with structured placeholder tokens. Responses are scanned for those tokens and restored before the agent reads them. The agent's context is preserved end-to-end.

---

## Quick start

**Requirements:** Python 3.11+

**macOS / Linux:**
```bash
git clone https://github.com/your-org/agentproxy
cd agentproxy
./install.sh
```

**Windows** — double-click `install.bat`, or from Command Prompt:
```bat
install.bat
```

That's it. The installer:
- Installs [uv](https://docs.astral.sh/uv/) if not already present
- Installs Python dependencies
- Generates the mitmproxy CA certificate
- Trusts it in your system keychain (macOS / Linux / Windows)
- Installs wrapper scripts (`claudeproxy`, `codexproxy`, `copilotproxy`) to `~/.local/bin`

```bash
# Start the proxy
agentproxy-start

# Run your agents through the proxy (in another terminal)
claudeproxy  claude   "summarise this codebase"
codexproxy   codex    "fix the failing test"
copilotproxy gh copilot suggest "list files by size"
```

To uninstall:

```bash
./uninstall.sh        # macOS / Linux
uninstall.bat         # Windows (double-click or run from terminal)
```

---

## Installation

```bash
python install.py
```

The installer handles everything in order:

| Step | What it does |
|------|-------------|
| 1 | Checks Python ≥ 3.11 |
| 2 | Installs `uv` if missing (the Python package manager) |
| 3 | Runs `uv sync` to install mitmproxy and other deps |
| 4 | Starts mitmproxy briefly to generate the CA certificate at `~/.mitmproxy/` |
| 5 | Trusts the certificate in your system keychain (macOS / Linux / Windows) |
| 6 | Copies wrapper scripts to `~/.local/bin` (or generates `.cmd` files on Windows) |

After install, make sure `~/.local/bin` is in your `PATH`:

```bash
# Add to ~/.zshrc or ~/.bashrc if needed:
export PATH="$HOME/.local/bin:$PATH"
```

To remove everything:

```bash
python install.py uninstall
```

This removes the wrapper scripts and untrusts the certificate. The `~/.mitmproxy/` cert files are left in place (remove manually with `rm -rf ~/.mitmproxy` if desired).

---

## Wrapper scripts

Each script sets `HTTP_PROXY` / `HTTPS_PROXY` and the TLS cert env var required by that agent's runtime, then execs the agent with all arguments forwarded.

| Script | Agent | TLS cert env var |
|--------|-------|-----------------|
| `claudeproxy` | Claude CLI (`claude`) | `NODE_EXTRA_CA_CERTS` + `SSL_CERT_FILE` |
| `codexproxy` | OpenAI Codex CLI (`codex`) | `NODE_EXTRA_CA_CERTS` + `SSL_CERT_FILE` |
| `copilotproxy` | GitHub Copilot CLI (`gh copilot`) | `NODE_EXTRA_CA_CERTS` + `SSL_CERT_FILE` |

To route any other agent through the proxy, set these env vars manually before running it:

```bash
export HTTP_PROXY=http://127.0.0.1:7717
export HTTPS_PROXY=http://127.0.0.1:7717
export NODE_EXTRA_CA_CERTS="$HOME/.mitmproxy/mitmproxy-ca-cert.pem"
export SSL_CERT_FILE="$HOME/.mitmproxy/mitmproxy-ca-cert.pem"
export REQUESTS_CA_BUNDLE="$HOME/.mitmproxy/mitmproxy-ca-cert.pem"
export CURL_CA_BUNDLE="$HOME/.mitmproxy/mitmproxy-ca-cert.pem"
```

---

## Configuration

Everything is in `config/agentproxy.yaml`. No other config files.

```yaml
proxy:
  port: 7717
  host: 127.0.0.1

detection:
  pattern_file: ./config/patterns.yaml
  scan_workers: 4
  intercepted_domains:
    - api.anthropic.com
    - api.openai.com
    - chatgpt.com
    - api.githubcopilot.com
    - api.individual.githubcopilot.com
    - copilot-proxy.githubusercontent.com
    - api.github.com
    # Add any custom gateway:
    # - localhost:4000        # LiteLLM or local model proxy
    # - my.ollama.host:11434

pii:
  enabled: false
  pattern_file: ./config/pii_patterns.yaml
  scan_workers: 4
  entities:
    - email
    - phone
    - ssn
    - credit_card
    - ip_address

masking:
  show_prefix_chars: 4    # characters shown before the mask
  show_suffix_chars: 4    # characters shown after the mask
  star_length: 24         # number of * in the placeholder

logging:
  log_file: ./logs/traffic.jsonl
  log_originals: false    # log pre-masking body (caution: logs secrets)
  log_passthrough: false  # log non-intercepted domains (debug only)

upstream_proxy:
  auto_detect: true       # read HTTP_PROXY/HTTPS_PROXY from env + OS settings
  url: ""                 # explicit: http://corp.proxy:8080
  username: ""            # or embed: http://user:pass@proxy:8080
  password: ""
  pac_file: ""            # /etc/proxy.pac or http://wpad/wpad.dat
```

### Adding domains

Only domains listed under `intercepted_domains` are scanned. Traffic to any other host passes through untouched.

```yaml
detection:
  intercepted_domains:
    - api.anthropic.com
    - localhost:4000        # your local LiteLLM gateway
    - internal.llm.corp     # internal model endpoint
```

### Corporate / enterprise proxy

If your machine routes outbound traffic through a corporate proxy, configure it under `upstream_proxy`. AgentProxy will chain through it:

```
Agent → AgentProxy → corporate proxy → internet → AI API
```

```yaml
upstream_proxy:
  url: http://corp.proxy:8080
  username: alice
  password: hunter2
```

For PAC file support, install the optional dependency first:

```bash
uv add pypac
```

Then set `pac_file` to the PAC file path or URL.

If `auto_detect: true` (default), AgentProxy reads `HTTP_PROXY` / `HTTPS_PROXY` from the environment and macOS / Windows system proxy settings automatically — no manual config needed for most corporate setups.

---

## What gets detected

Patterns are defined in `config/patterns.yaml`. Built-in patterns cover:

| Pattern | Examples |
|---------|---------|
| Anthropic API key | `sk-ant-api03-...` |
| OpenAI API key | `sk-proj-...`, `sk-...` |
| Generic bearer token | `Bearer eyJ...` |
| JWT | `eyJhbGciOi...` |
| Database connection string | `postgres://user:pass@host/db` |
| AWS credentials | `AKIA...`, `ASIA...`, `aws_secret_access_key=...`, `aws_session_token=...` |
| Private key blocks | `-----BEGIN OPENSSH PRIVATE KEY----- ... -----END ...-----` |

Secrets inside **base64, hex, or URL-encoded blobs** are also detected. The proxy decodes the blob, scans the contents, re-encodes with the secret masked — up to 4 levels of nesting.

PII masking is configured separately and is **disabled by default**. When enabled, patterns from `config/pii_patterns.yaml` are masked through the same vault/restore flow, and you can choose only the entities you want:

| PII entity | Examples |
|---------|---------|
| Email | `alice@example.com` |
| Phone | `415-555-2671` |
| SSN | `123-45-6789` |
| Credit card | `4111 1111 1111 1111` |
| IPv4 address | `10.20.30.40` |

To add custom patterns, append to `config/patterns.yaml`:

```yaml
patterns:
  - name: MY_INTERNAL_TOKEN
    regex: 'myco-[a-f0-9]{32}'
    type: api_key
    confidence: high
```

---

## Traffic log

Every intercepted request and response is logged to `logs/traffic.jsonl` as newline-delimited JSON:

```json
{"event": "request", "method": "POST", "host": "api.anthropic.com", "path": "/v1/messages", "masked_count": 2, "body": "...", "ts": "2026-03-25T10:00:00+00:00"}
{"event": "response", "host": "api.anthropic.com", "path": "/v1/messages", "status": 200, "masked_count": 0, "body": "...", "ts": "2026-03-25T10:00:01+00:00"}
```

`masked_count` is the number of secrets masked in that event. A value of `0` on a request means the prompt was clean.

---

## Tasks

```bash
./install.sh             # macOS / Linux — install everything
./uninstall.sh           # macOS / Linux — remove everything

install.bat              # Windows — install  (double-click or terminal)
uninstall.bat            # Windows — remove   (double-click or terminal)

# If you have Task installed (https://taskfile.dev):
task install      # same as ./install.sh
task uninstall    # same as ./uninstall.sh
task start        # start the proxy (foreground)
task test         # run test suite
task test-cov     # run tests with coverage report
task build        # sync dependencies only (uv sync)
```

---

## Project structure

```
agentproxy/
├── proxy/
│   ├── addon.py        # mitmproxy addon — main request/response/websocket pipeline
│   ├── detector.py     # domain allowlist loader
│   ├── scanner.py      # regex secret scanner
│   ├── vault.py        # mask ↔ restore token store
│   ├── decoder.py      # body decode/encode + encoded blob scanning
│   ├── logger.py       # JSONL traffic logger
│   └── upstream.py     # upstream proxy resolver (env / PAC / explicit)
├── config/
│   ├── agentproxy.yaml # main config (domains, masking, logging, upstream proxy)
│   ├── patterns.yaml   # secret detection regex patterns
│   └── filenames.yaml  # sensitive filename patterns
├── bin/
│   ├── agentproxy-start  # startup script
│   ├── claudeproxy       # Claude CLI wrapper
│   ├── codexproxy        # Codex CLI wrapper
│   └── copilotproxy      # GitHub Copilot CLI wrapper
├── tests/              # pytest test suite
├── logs/               # traffic.jsonl written here at runtime
└── Taskfile.yml
```

---

## Security notes

- **Local only.** The proxy listens on `127.0.0.1` by default. It is not exposed to the network.
- **No cloud.** Traffic is processed in memory on your machine. Nothing is sent to a third party.
- **Secrets in logs.** By default `log_originals: false` — the pre-masking body is never written to disk. Set to `true` only for debugging, and treat the log file as sensitive.
- **CA cert.** mitmproxy generates a local CA cert at `~/.mitmproxy/`. Trusting it allows the proxy to decrypt HTTPS traffic on your machine. Only do this on a machine you control.
- **Mask, never block.** AgentProxy replaces secrets with placeholder tokens and restores them in responses. It never drops or blocks requests. Your agent always gets a response.

---

## Limitations

- **SSE / streaming responses.** Server-sent event streams are processed per-chunk. If a vault token is split across two chunks, the restore will miss it. Full streaming support is a known open item.
- **Non-HTTP transports.** Only HTTP/HTTPS and WebSocket traffic is intercepted. Direct TCP or gRPC connections bypass the proxy.
- **mitmproxy CA trust.** Some agents use system certificate stores that require OS-level trust (e.g. Rust agents using `rustls-native-certs`). The `SSL_CERT_FILE` env var covers most cases.
- **PAC file support** requires `pypac`: `uv add pypac`.

---

## Contributing

1. Fork and clone.
2. `uv sync` to install dependencies.
3. `uv run pytest` to run the test suite.
4. Add tests for any new behaviour before opening a PR.

All secret-detection patterns live in `config/patterns.yaml` — contributions for new patterns (cloud provider keys, SaaS tokens, etc.) are especially welcome.

---

## License

MIT License — see [LICENSE](LICENSE).
