# AgentProxy

A local MITM proxy that sits between AI coding agents (Claude, Codex, Copilot, etc.) and their remote APIs. It intercepts outbound traffic, detects secrets in prompts, masks them before they leave your machine, and restores them in the response — so the agent works normally and your secrets never hit the wire.

**Zero cloud dependency. Runs entirely on your machine. No code changes to your agents.**

---

## The problem

AI agents read your codebase. They see `.env` files, config files, shell history. If a secret ends up in a prompt — even by accident — it leaves your machine in plaintext and ends up in logs on someone else's server.

AgentProxy sits in the middle and makes that safe.

```
Before:  Claude → api.anthropic.com  (prompt contains sk-ant-api03-abc123...)
After:   Claude → AgentProxy → api.anthropic.com  (prompt contains sk-ant-api03-rdaijelaly...)
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
   ├── Replace secrets → shape-preserving vault surrogates  (restorable in response)
   │        sk-ant-api03-abc123  →  sk-ant-api03-rda120
   │
   ├── Re-encode body and forward request
   │
   ▼
AI API  (secrets never arrive as plaintext)
   │
   ▼
AgentProxy  (response)
   │
   └── Restore vault surrogates → original values
   │
   ▼
AI Agent  (sees its data back intact)
```

Secrets are replaced with random-looking, shape-preserving surrogate values. Responses are scanned for those exact surrogates and restored before the agent reads them. The agent's context is preserved end-to-end.

---

## Quick start

Two commands, start to finish:

```bash
# 1. Install (Go 1.23+ required; npm-based install is on the way)
task install

# 2. Trust the CA, start the proxy + dashboard, and open the dashboard
agentproxy setup --open
```

That's it. `agentproxy setup` is idempotent — re-running prints "already trusted ✓" and is safe.

Then run your agents through the proxy in another terminal:

```bash
claudeproxy  "summarise this codebase"
codexproxy   "fix the failing test"
copilotproxy suggest "list files by size"
```

The installer places the binary and wrapper commands in `~/.local/bin/`. If that directory is not yet in your `PATH`, the installer will print instructions to add it.

To uninstall:

```bash
./uninstall.sh              # macOS / Linux — remove binary, certs, runtime dirs, and OS trust
./uninstall.sh --keep-trust # keep OS trust, remove local files and commands only

uninstall.bat               # Windows — remove binary, certs, runtime dirs, and Root-store trust
uninstall.bat --keep-trust  # keep Root-store trust, remove local files and commands only
```

---

## Documentation

For codebase and workflow guidance:

- `AGENTS.md` — repo working contract and spec-first rules
- `CLAUDE.md` — contributor build/test/architecture notes
- `docs/index.md` — documentation map and where new docs belong
- `docs/specs/` — behavior specs to create or update before non-trivial implementation

---

## Installation detail

The installer runs four steps:

1. **Build** — `go build -o agentproxy ./cmd/agentproxy`
2. **CA setup** — `./agentproxy ca-setup`
   - Writes the proxy CA cert to `~/.agentproxy/certs/agentproxy-ca-cert.pem`
   - Creates `./certs/` (per-host TLS cert cache) and `./logs/`
   - `install.sh` / `install.bat` then try to trust the cert in the OS store automatically
3. **PATH install** — copies the binary and symlinks wrapper scripts to `~/.local/bin/`
   - `agentproxy`, `claudeproxy`, `codexproxy`, `copilotproxy`, `agentproxy-start`
   - Prints a reminder if `~/.local/bin` is not yet in your `PATH`
4. **Summary** — prints the commands and any remaining trust instructions for non-interactive installs

You can also run setup steps individually:

```bash
go build -o agentproxy ./cmd/agentproxy
./agentproxy ca-setup --cert-dir /custom/path   # custom CA cert output dir
./agentproxy validate                           # check config and file paths
./agentproxy config                             # print loaded config summary
./agentproxy start --check                      # dry-run startup validation
./agentproxy status                             # check if the proxy is listening
```

---

## Wrapper scripts

Each wrapper first checks `agentproxy status` and fails fast with a clear message if the proxy is down. When the proxy is running, it sets `HTTP_PROXY` / `HTTPS_PROXY` plus the TLS cert env vars required by that agent runtime, then execs the agent with all arguments forwarded.

| Script | Agent | TLS cert env var |
|--------|-------|-----------------|
| `bin/claudeproxy` | Claude CLI (`claude`) | `NODE_EXTRA_CA_CERTS` + `SSL_CERT_FILE` |
| `bin/codexproxy` | OpenAI Codex CLI (`codex`) | `NODE_EXTRA_CA_CERTS` + `SSL_CERT_FILE` |
| `bin/copilotproxy` | GitHub Copilot CLI (`gh copilot`) | `NODE_EXTRA_CA_CERTS` + `SSL_CERT_FILE` |

To route any other agent through the proxy, set these env vars manually before running it:

```bash
export AGENTPROXY_HOST=127.0.0.1
export AGENTPROXY_PORT=7717
export AGENTPROXY_CA_CERT="$HOME/.agentproxy/certs/agentproxy-ca-cert.pem"
export HTTP_PROXY="http://${AGENTPROXY_HOST}:${AGENTPROXY_PORT}"
export HTTPS_PROXY="http://${AGENTPROXY_HOST}:${AGENTPROXY_PORT}"
export NODE_EXTRA_CA_CERTS="$AGENTPROXY_CA_CERT"
export SSL_CERT_FILE="$AGENTPROXY_CA_CERT"
export REQUESTS_CA_BUNDLE="$AGENTPROXY_CA_CERT"
export CURL_CA_BUNDLE="$AGENTPROXY_CA_CERT"
```

---

## Configuration

Everything is in `config/agentproxy.yaml`.

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
  show_prefix_chars: 4    # legacy; surrogates now preserve shape instead of star masks
  show_suffix_chars: 4    # legacy; retained for config compatibility
  star_length: 24         # legacy; retained for config compatibility

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

If `auto_detect: true` (default), AgentProxy reads `HTTP_PROXY` / `HTTPS_PROXY` from the environment and macOS / Windows system proxy settings automatically — no manual config needed for most corporate setups.

---

## What gets detected

Patterns are defined in `config/patterns.yaml`. Built-in patterns cover:

| Pattern | Examples |
|---------|---------|
| Anthropic API key | `sk-ant-api03-...` |
| OpenAI API key | `sk-proj-...`, `sk-...` |
| GitHub token | `ghp_...`, `ghs_...` |
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
{"event": "request", "method": "POST", "host": "api.anthropic.com", "path": "/v1/messages", "masked_count": 2, "modified_request_body": "...", "ts": "2026-03-25T10:00:00+00:00"}
{"event": "response", "host": "api.anthropic.com", "path": "/v1/messages", "status": 200, "modified_response_body": "...", "ts": "2026-03-25T10:00:01+00:00"}
```

`masked_count` is the number of secrets masked in that event. A value of `0` on a request means the prompt was clean.

When `logging.log_originals: true`, request/response events also include `request_body` and `response_body` for before/after comparison in the dashboard. Keep this disabled unless you are intentionally debugging, because original bodies can contain secrets.

---

## Tasks

```bash
./install.sh             # macOS / Linux — build, CA setup, install to ~/.local/bin
./uninstall.sh           # macOS / Linux — remove everything, including OS CA trust
./uninstall.sh --keep-trust # keep OS CA trust, remove local files and commands only

install.bat              # Windows — build, CA setup, install to %USERPROFILE%\.local\bin
uninstall.bat            # Windows — remove everything, including Root-store CA trust

# After install, these commands are on your PATH:
agentproxy-start         # start the proxy (foreground)
claudeproxy  "..."       # run Claude through the proxy
codexproxy   "..."       # run Codex through the proxy
copilotproxy "..."       # run GitHub Copilot through the proxy

# If you have Task installed (https://taskfile.dev):
task install             # same as ./install.sh
task uninstall           # same as ./uninstall.sh
task start               # start the proxy (foreground)
task test                # run all Go tests
task test-acceptance     # run acceptance tests (tests/go/)
task test-e2e-claude     # run live Claude e2e test (needs proxy running + API key)
task test-e2e-masking    # run live Codex/masking e2e test (needs proxy running)
task test-cov            # run tests with coverage report
task build               # build the Go binary
task clean               # remove build artifacts
```

---

## Project structure

```
agentproxy/
├── cmd/agentproxy/       # CLI entry point (start, validate, config, ca-setup)
├── internal/
│   ├── config/           # config loader and startup validation
│   ├── scanner/          # regex secret scanner (worker-pool parallel)
│   ├── codec/            # body decode/encode + encoded blob scanning/restoration
│   ├── vault/            # session-scoped mask ↔ restore token store
│   ├── logger/           # JSONL traffic logger
│   ├── runtime/          # masking service (wires scanner + vault + codec + logger)
│   └── proxy/            # goproxy-based MITM server + WebSocket hook
├── config/
│   ├── agentproxy.yaml   # main config (domains, masking, logging, upstream proxy)
│   ├── patterns.yaml     # secret detection regex patterns
│   └── pii_patterns.yaml # optional PII patterns (email, phone, SSN, ...)
├── bin/
│   ├── agentproxy-start  # startup wrapper
│   ├── claudeproxy       # Claude CLI wrapper
│   ├── codexproxy        # Codex CLI wrapper
│   └── copilotproxy      # GitHub Copilot CLI wrapper
├── tests/
│   ├── go/               # CI-runnable acceptance tests (no live API needed)
│   ├── e2e_claude_test.sh   # live Claude end-to-end verification
│   └── e2e_masking_test.sh  # live Codex/masking end-to-end verification
├── third_party/goproxywss/  # vendored goproxy with WebSocket frame hook
├── go-spikes/            # spike sandboxes for SSE, vault, WebSocket, encoded blobs
├── docs/
│   ├── decisions/        # spike decision records
│   └── testing-scenarios.md
└── logs/                 # traffic.jsonl written here at runtime
```

---

## Security notes

- **Local only.** The proxy listens on `127.0.0.1` by default. It is not exposed to the network.
- **No cloud.** Traffic is processed in memory on your machine. Nothing is sent to a third party.
- **Secrets in logs.** By default `log_originals: false` — the pre-masking body is never written to disk. Set to `true` only for debugging, and treat the log file as sensitive.
- **CA cert.** The proxy uses a local CA cert at `~/.agentproxy/certs/agentproxy-ca-cert.pem` to sign intercepted HTTPS connections. Trusting it allows the proxy to decrypt HTTPS traffic on your machine. Only do this on a machine you control.
- **Replace, never block.** AgentProxy replaces secrets with exact-match, shape-preserving vault surrogates and restores them in responses. It never drops or blocks requests. Your agent always gets a response.

---

## Limitations

- **Non-HTTP transports.** Only HTTP/HTTPS and WebSocket traffic is intercepted. Direct TCP or gRPC connections bypass the proxy.
- **PAC file.** The `pac_file` config key is parsed but PAC evaluation is not yet implemented; use an explicit `url` instead.
- **System CA trust.** Some agents use system certificate stores that require OS-level trust (e.g. Rust agents using `rustls-native-certs`). The installers now attempt that automatically on interactive installs; non-interactive installs print the manual trust command instead.
- **Uninstall removes trust.** `./uninstall.sh` and `uninstall.bat` remove the AgentProxy CA from the OS trust store by default, then delete the local CA files. Use `--keep-trust` only if you intentionally want the CA to remain trusted.

---

## Contributing

1. Fork and clone.
2. `go test ./...` to run the unit and acceptance test suite.
3. Add tests for any new behaviour before opening a PR.

All secret-detection patterns live in `config/patterns.yaml` — contributions for new patterns (cloud provider keys, SaaS tokens, etc.) are especially welcome.

---

## Acknowledgements

- **[Gitleaks](https://github.com/gitleaks/gitleaks)** — the default secret-detection rules in `config/patterns.yaml` are bulk-imported from the Gitleaks community catalog (MIT). Each imported rule carries a `# source:` comment.
- **[elazarl/goproxy](https://github.com/elazarl/goproxy)** — MITM proxy engine; vendored fork with WebSocket support lives in `third_party/goproxywss`.

---

## License

MIT License — see [LICENSE](LICENSE).
