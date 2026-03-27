# AgentProxy — Product Specification

**Version:** 0.3-draft  
**Status:** Design / Pre-build  
**Scope:** Process-scoped HTTP interceptor for CLI AI agents with secret masking and sensitive file detection

---

## 1. What It Is

AgentProxy is a local, process-scoped network interceptor that sits between a CLI AI agent (Claude, Codex, opencode, Copilot, or any HTTP-based agent) and its remote API. It captures outbound requests and inbound responses, detects secrets, sensitive values, and sensitive file contents using a layered approach, masks them in transit, and restores them on return — without modifying the agent itself.

It works by launching the target agent through a wrapper script that routes all HTTP/HTTPS traffic through a local proxy. The agent has no knowledge this is happening.

---

## 2. Goals

- Prevent secrets, credentials, and PII from leaving the local machine inside AI prompt payloads
- Detect and mask contents of sensitive files that an agent reads and pastes into prompts (.env, key files, cert files, config files with credentials)
- Work with any CLI AI agent that honours HTTP proxy environment variables
- Require zero changes to the agent binary or source
- Run entirely locally — no cloud dependency, no data leaving the machine except the sanitised request
- Be fast enough to be invisible to the user during normal agent interaction
- Be extensible — org-specific patterns and filename rules added via config, not code changes

---

## 3. Non-Goals

- Does not intercept OS-level traffic (no kernel hooks, no eBPF)
- Does not modify agent internals or filesystem access
- Does not decrypt or inspect TLS auth headers (Authorization, x-api-key are never touched)
- Does not attempt full DLP across all traffic — only AI completion endpoints
- Does not store original secret values in plaintext logs
- Does not require a local LLM or GPU

---

## 4. Supported Agents

### Phase 1 — CLI agents

| Agent | Binary | API base | Proxy mechanism |
|---|---|---|---|
| Claude CLI | `claude` | api.anthropic.com | HTTP_PROXY env var |
| OpenAI Codex | `codex` | api.openai.com | HTTP_PROXY env var |
| opencode | `opencode` | configurable | HTTP_PROXY env var |
| GitHub Copilot CLI | `gh copilot` | api.githubcopilot.com | HTTP_PROXY env var |

### Phase 2 — UI agents (IDE extensions and Electron apps)

| Agent | Host | API base | Proxy mechanism |
|---|---|---|---|
| Claude Code (VS Code extension) | VS Code | api.anthropic.com | VS Code `http.proxy` setting — propagates to all extensions |
| GitHub Copilot (VS Code extension) | VS Code | api.githubcopilot.com | Same VS Code proxy setting — covered automatically |
| GitHub Copilot (JetBrains) | IntelliJ / PyCharm / WebStorm etc. | api.githubcopilot.com | JVM proxy args in `*.vmoptions` |
| Claude Code (standalone Electron) | Electron | api.anthropic.com | `--proxy-server` Chromium launch flag |

**Key insight for UI agents:** VS Code propagates its `http.proxy` setting to every extension running inside it. Patching one setting covers Claude Code and GitHub Copilot simultaneously — no per-extension configuration needed.

New agents can be added without core changes — CLI agents get a wrapper script, UI agents get an install command that patches their host application's proxy config.

---

## 5. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Developer Machine                                              │
│                                                                 │
│  ┌──────────────┐    ┌──────────────────────────────────────┐  │
│  │  Wrapper     │    │  AgentProxy (localhost:7717)          │  │
│  │  claudeproxy │───▶│                                      │  │
│  │  codexproxy  │    │  1. Endpoint detector                │  │
│  │  etc.        │    │  2. Body decoder (JSON / gzip)       │  │
│  └──────────────┘    │  3. Filename detector                 │  │
│         ▲            │  4. Pattern scanner (regex fast path) │  │
│         │            │  5. LLM Guard + Presidio scanner      │  │
│  ┌──────────────┐    │  6. Vault (mask / restore)           │  │
│  │  Agent       │    │  7. Logger                           │  │
│  │  Process     │◀───│                                      │  │
│  │  (claude,    │    └──────────────┬───────────────────────┘  │
│  │   codex...)  │                  │                           │
│  └──────────────┘                  ▼                           │
│                           Remote API                           │
│                           (Anthropic / OpenAI / etc.)          │
└─────────────────────────────────────────────────────────────────┘
```

---

## 6. File Structure

```
agentproxy/
│
├── install.sh                    # Unix installer (macOS + Linux)
├── install.cmd                   # Windows installer
│
├── bin/
│   ├── claudeproxy               # Wrapper for Claude CLI
│   ├── claudeproxy.cmd           # Windows wrapper for Claude CLI
│   ├── codexproxy                # Wrapper for OpenAI Codex
│   ├── codexproxy.cmd
│   ├── opencodeproxy             # Wrapper for opencode
│   ├── opencodeproxy.cmd
│   ├── copilotproxy              # Wrapper for GitHub Copilot CLI
│   ├── copilotproxy.cmd
│   └── agentproxy-start          # Start the proxy daemon
│
├── proxy/
│   ├── addon.py                  # mitmproxy addon — main pipeline
│   ├── detector.py               # Endpoint detection logic
│   ├── decoder.py                # Body decode / encode
│   ├── filename_detector.py      # Sensitive filename and content detection
│   ├── scanner.py                # Layer 1 + 2 + 3 scanner orchestrator
│   ├── vault.py                  # Mask / restore store
│   └── logger.py                 # Structured logger
│
├── bin/
│   └── agentproxy-detect         # Universal auto-discovery wrapper (additive)
│
├── config/
│   ├── patterns.yaml             # Regex pattern registry (built-in + org custom)
│   ├── filenames.yaml            # Sensitive filename rules
│   ├── domains.yaml              # Domain allowlist — only these hosts are intercepted
│   └── agentproxy.yaml           # Main config (full reference in section 23)
│
├── certs/
│   └── (generated at install time by mitmproxy)
│
└── logs/
    └── traffic.jsonl             # Append-only structured log
```

---

## 7. Configuration Files

### agentproxy.yaml

```yaml
proxy:
  port: 7717
  host: 127.0.0.1

tls:
  cert_dir: ./certs

detection:
  pattern_file: ./config/patterns.yaml
  filename_file: ./config/filenames.yaml
  structural_key_scan: true
  llmguard_enabled: true

masking:
  show_prefix_chars: 4
  show_suffix_chars: 4
  star_length: 24

logging:
  log_file: ./logs/traffic.jsonl
  log_request_body: true
  log_response_body: true
  log_originals: false        # never log unmasked values

org:
  name: ""
```

### patterns.yaml

```yaml
patterns:

  # --- API Keys ---
  - name: ANTHROPIC_API_KEY
    regex: 'sk-ant-api\d{2}-[A-Za-z0-9\-_]{20,}'
    type: apikey
    confidence: certain

  - name: OPENAI_API_KEY
    regex: 'sk-[A-Za-z0-9]{20,}'
    type: apikey
    confidence: certain

  - name: AWS_ACCESS_KEY
    regex: 'AKIA[0-9A-Z]{16}'
    type: apikey
    confidence: certain

  - name: AWS_SECRET_KEY
    regex: '(?i)aws_secret_access_key\s*=\s*[A-Za-z0-9/+=]{40}'
    type: apikey
    confidence: certain

  - name: GOOGLE_API_KEY
    regex: 'AIza[0-9A-Za-z\-_]{35}'
    type: apikey
    confidence: certain

  - name: GITHUB_TOKEN
    regex: 'gh[ps]_[A-Za-z0-9]{36,}'
    type: apikey
    confidence: certain

  - name: GITLAB_TOKEN
    regex: 'glpat-[A-Za-z0-9\-_]{20}'
    type: apikey
    confidence: certain

  - name: HUGGINGFACE_TOKEN
    regex: 'hf_[A-Za-z0-9]{34,}'
    type: apikey
    confidence: certain

  - name: STRIPE_KEY
    regex: 'sk_live_[A-Za-z0-9]{24,}'
    type: apikey
    confidence: certain

  - name: SENDGRID_KEY
    regex: 'SG\.[A-Za-z0-9\-_]{22}\.[A-Za-z0-9\-_]{43}'
    type: apikey
    confidence: certain

  - name: TWILIO_KEY
    regex: 'SK[0-9a-fA-F]{32}'
    type: apikey
    confidence: certain

  # --- Tokens ---
  - name: JWT_TOKEN
    regex: 'eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+'
    type: jwt
    confidence: certain

  - name: BEARER_TOKEN
    regex: '(?i)bearer\s+[A-Za-z0-9\-_\.]{20,}'
    type: jwt
    confidence: high

  # --- Private Keys / Certs ---
  - name: PRIVATE_KEY_BLOCK
    regex: '-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----'
    type: cert
    confidence: certain

  - name: CERTIFICATE_BLOCK
    regex: '-----BEGIN CERTIFICATE-----'
    type: cert
    confidence: certain

  # --- Connection Strings ---
  - name: GENERIC_CONNECTION_STRING
    regex: '(postgres|postgresql|mysql|mariadb|mongodb|redis|amqp|rabbitmq|kafka)://[^\s"'']{8,}'
    type: connstr
    confidence: certain

  - name: MSSQL_CONNECTION_STRING
    regex: '(?i)Server=.{1,50};Database=.{1,50};(User Id|UID)=.{1,50};Password='
    type: connstr
    confidence: certain

  - name: JDBC_CONNECTION_STRING
    regex: 'jdbc:[a-z]+://[^\s"'']{8,}'
    type: connstr
    confidence: certain

  # --- Env-style assignments ---
  - name: ENV_SECRET_ASSIGNMENT
    regex: '(?i)(password|passwd|secret|api_key|apikey|access_token|auth_token|private_key)\s*[=:]\s*[^\s"''\n]{6,}'
    type: password
    confidence: high

  - name: DOTENV_ASSIGNMENT
    regex: '^[A-Z_]{3,}_(KEY|SECRET|TOKEN|PASSWORD|PASS|PWD|CREDENTIAL)\s*=\s*.{6,}'
    type: password
    confidence: high

  # --- PII ---
  - name: EMAIL_ADDRESS
    regex: '[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}'
    type: pii_email
    confidence: high

  # --- Org Custom (examples — replace with your own) ---
  # - name: INTERNAL_SERVICE_TOKEN
  #   regex: 'ACME-[0-9]{8}-[A-Z]{4}'
  #   type: apikey
  #   confidence: certain
```

### filenames.yaml

This is the new section. When an agent reads a file and pastes its contents into a prompt, the filename often appears in the message (e.g. "here is the contents of `.env`"). AgentProxy detects these filename references and applies extra scrutiny to the content block that follows.

```yaml
sensitive_filenames:

  # --- Environment files ---
  - pattern: '.env'
    match: exact
    type: env_file
    action: scan_contents

  - pattern: '.env.*'
    match: glob
    type: env_file
    action: scan_contents
    examples:
      - .env.local
      - .env.production
      - .env.staging
      - .env.development
      - .env.test
      - .env.override

  # --- Secret / credential files ---
  - pattern: '*.pem'
    match: glob
    type: cert
    action: scan_contents

  - pattern: '*.key'
    match: glob
    type: cert
    action: scan_contents

  - pattern: '*.p12'
    match: glob
    type: cert
    action: flag_and_mask_all

  - pattern: '*.pfx'
    match: glob
    type: cert
    action: flag_and_mask_all

  - pattern: '*.p8'
    match: glob
    type: cert
    action: scan_contents

  - pattern: 'id_rsa'
    match: exact
    type: cert
    action: flag_and_mask_all

  - pattern: 'id_ed25519'
    match: exact
    type: cert
    action: flag_and_mask_all

  - pattern: 'id_ecdsa'
    match: exact
    type: cert
    action: flag_and_mask_all

  - pattern: '*.pub'
    match: glob
    type: pubkey
    action: log_only            # public keys — log but don't mask

  # --- Configuration files known to contain credentials ---
  - pattern: 'credentials'
    match: exact
    type: credentials_file
    action: scan_contents

  - pattern: 'credentials.json'
    match: exact
    type: credentials_file
    action: scan_contents

  - pattern: 'service-account.json'
    match: exact
    type: credentials_file
    action: scan_contents

  - pattern: 'service_account.json'
    match: exact
    type: credentials_file
    action: scan_contents

  - pattern: 'gcloud_credentials.json'
    match: exact
    type: credentials_file
    action: scan_contents

  - pattern: '*.json'
    match: glob
    type: json_file
    action: scan_contents       # JSON gets pattern scan — might have API keys inside

  # --- AWS ---
  - pattern: 'credentials'
    path_contains: '.aws'
    match: exact
    type: aws_credentials
    action: flag_and_mask_all

  - pattern: 'config'
    path_contains: '.aws'
    match: exact
    type: aws_config
    action: scan_contents

  # --- Kubernetes / Helm ---
  - pattern: 'kubeconfig'
    match: exact
    type: kubeconfig
    action: flag_and_mask_all

  - pattern: '*.kubeconfig'
    match: glob
    type: kubeconfig
    action: flag_and_mask_all

  - pattern: 'values.yaml'
    match: exact
    type: helm_values
    action: scan_contents

  - pattern: 'values-*.yaml'
    match: glob
    type: helm_values
    action: scan_contents
    examples:
      - values-production.yaml
      - values-staging.yaml

  # --- Docker ---
  - pattern: '.dockerconfigjson'
    match: exact
    type: docker_credentials
    action: flag_and_mask_all

  - pattern: 'config.json'
    path_contains: '.docker'
    match: exact
    type: docker_credentials
    action: flag_and_mask_all

  # --- Terraform ---
  - pattern: '*.tfvars'
    match: glob
    type: terraform_vars
    action: scan_contents
    examples:
      - terraform.tfvars
      - production.tfvars

  - pattern: 'terraform.tfstate'
    match: exact
    type: terraform_state
    action: scan_contents       # state files often contain resource secrets

  - pattern: '*.tfstate'
    match: glob
    type: terraform_state
    action: scan_contents

  # --- Ansible ---
  - pattern: 'vault.yml'
    match: exact
    type: ansible_vault
    action: scan_contents

  - pattern: 'vault.yaml'
    match: exact
    type: ansible_vault
    action: scan_contents

  - pattern: 'secrets.yml'
    match: exact
    type: secrets_file
    action: scan_contents

  - pattern: 'secrets.yaml'
    match: exact
    type: secrets_file
    action: scan_contents

  # --- Git ---
  - pattern: '.git-credentials'
    match: exact
    type: git_credentials
    action: flag_and_mask_all

  - pattern: '.netrc'
    match: exact
    type: netrc
    action: flag_and_mask_all

  # --- SSH ---
  - pattern: 'known_hosts'
    match: exact
    type: ssh_known_hosts
    action: log_only

  - pattern: 'authorized_keys'
    match: exact
    type: ssh_authorized_keys
    action: log_only

  - pattern: 'ssh_config'
    match: exact
    type: ssh_config
    action: scan_contents

  # --- Application config files ---
  - pattern: 'application.properties'
    match: exact
    type: spring_config
    action: scan_contents

  - pattern: 'application.yml'
    match: exact
    type: spring_config
    action: scan_contents

  - pattern: 'application-*.yml'
    match: glob
    type: spring_config
    action: scan_contents
    examples:
      - application-production.yml
      - application-staging.yml

  - pattern: 'appsettings.json'
    match: exact
    type: dotnet_config
    action: scan_contents

  - pattern: 'appsettings.*.json'
    match: glob
    type: dotnet_config
    action: scan_contents
    examples:
      - appsettings.Production.json
      - appsettings.Development.json

  - pattern: 'database.yml'
    match: exact
    type: db_config
    action: scan_contents       # Rails database config

  - pattern: 'database.yaml'
    match: exact
    type: db_config
    action: scan_contents

  - pattern: 'config.yml'
    match: exact
    type: generic_config
    action: scan_contents

  - pattern: 'config.yaml'
    match: exact
    type: generic_config
    action: scan_contents

  - pattern: 'settings.py'
    match: exact
    type: django_settings
    action: scan_contents       # Django settings often contains SECRET_KEY

  - pattern: 'local_settings.py'
    match: exact
    type: django_settings
    action: scan_contents

  # --- Package manager / CI tokens ---
  - pattern: '.npmrc'
    match: exact
    type: npm_config
    action: scan_contents       # often contains NPM_TOKEN

  - pattern: '.pypirc'
    match: exact
    type: pypi_config
    action: scan_contents

  - pattern: '.gemrc'
    match: exact
    type: gem_config
    action: scan_contents

  # --- CI/CD ---
  - pattern: '.travis.yml'
    match: exact
    type: ci_config
    action: scan_contents

  - pattern: '.circleci/config.yml'
    match: exact
    type: ci_config
    action: scan_contents

  # --- Org custom (add your own below) ---
  # - pattern: 'internal-config.json'
  #   match: exact
  #   type: org_config
  #   action: scan_contents
```

**Actions defined:**

| Action | Behaviour |
|---|---|
| `scan_contents` | Run full detection pipeline (Layers 1-3) on the content block associated with this filename |
| `flag_and_mask_all` | Treat entire content block as sensitive — mask all non-trivial string values regardless of pattern match |
| `log_only` | Do not mask, but log that this filename appeared in a prompt |

---

## 8. Endpoint Detection

Does not hardcode endpoint paths. Pattern-matches the request path against known AI completion path fragments:

```
/chat
/completions
/messages
/generate
/stream
/inference
/invoke
/converse
/respond
/complete
```

If the path contains any of these fragments AND the content-type is `application/json`, the request body enters the detection pipeline. All other traffic is forwarded transparently.

---

## 9. Detection Pipeline

### Layer 1 — Pattern Registry (fast path, synchronous, microseconds)

Runs `patterns.yaml` regex set against all scan targets. First match on a value wins — no double-scanning.

### Layer 2 — Structural Key-Name Heuristic (synchronous, parallel, microseconds)

Walks the JSON tree. Any key whose name contains any of the following triggers scrutiny of its value regardless of format:

```
password, passwd, pwd, secret, token, apikey, api_key, access_key,
credential, auth, private_key, private, signing_key, encryption_key,
connection_string, conn_str, dsn, database_url
```

### Layer 3 — LLM Guard + Presidio (synchronous, milliseconds)

```
pip install llm-guard
```

Runs on content that Layers 1 and 2 did not flag. Uses LLM Guard's built-in scanners:

- `Secrets` scanner — catches API keys, tokens, connection strings not covered by Layer 1
- `Sensitive` scanner — PII: names, addresses, phone numbers, national ID numbers
- `BanSubstrings` scanner — configurable block list for org-specific terms
- Presidio NLP engine underneath for PII — runs locally, no network

No LLM model required. No GPU. No ollama. All rule-based and lightweight NLP.

### Layer 4 — Filename Detector (runs on full message content, parallel)

Scans all message content strings for filename references from `filenames.yaml`. When a match is found, the content block in the vicinity of that filename reference is tagged and the appropriate action applied.

Detection heuristics for filename references in prompts:
- Exact filename string present in content (`".env"`, `"id_rsa"`)
- Common phrasing: `"contents of"`, `"here is"`, `"file:"`, code fences with filename labels
- File path fragments: `"/home/user/.env"`, `"C:\Users\user\.env"`, `"./config/secrets.yaml"`

---

## 10. Scan Targets

Extracted from each request before scanning:

- `messages[*].content` — all message content strings
- `messages[*].content[*].text` — vision/tool content blocks
- `tool_calls[*].function.arguments` — tool call payloads (common secret leak point)
- `tool_results[*].content` — tool result payloads
- Any string value whose JSON key name matches the structural heuristic list
- Any content block tagged by the filename detector

---

## 11. Vault and Masking

### Masking Format

```
sk-ant-api03-ABCDEFabcdef12345678xxxxxxxxxxxxxxxxxxxx9999

becomes:

sk-a************************9999
```

Rule: first 4 chars + fixed 24 stars + last 4 chars. Always the same visual length regardless of secret length — prevents length-based inference. Values under 8 chars become `****`.

### Masking Examples

| Original | Masked |
|---|---|
| `sk-ant-api03-ABCDxxxxxxxxxxxx9999` | `sk-a************************9999` |
| `postgres://admin:hunter2@db.prod.local/mydb` | `post************************mydb` |
| `eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.abc123` | `eyJh************************c123` |
| `AKIAIOSFODNN7EXAMPLE` | `AKIA************************IPLE` |
| `hunter2` | `****` |
| `john.doe@company.com` | `john************************.com` |
| `-----BEGIN RSA PRIVATE KEY-----\nMIIE...` | `----************************----` |
| `ACME-20250101-ABCD` | `ACME************************ABCD` |

### Vault Entry

```
hash_key  : a3f9c2d1                (SHA256(original)[:8], used as lookup key)
masked    : sk-a************************9999
original  : sk-ant-api03-ABCD...
type      : apikey
source    : pattern:ANTHROPIC_API_KEY
first_seen: 2025-03-25T10:42:00Z
```

### Vault Scope

- Per agent process session (one wrapper invocation = one session)
- In-memory primary store
- Append-only file backup keyed by session UUID — survives proxy restart within a session
- Vault flushed on clean process exit

### Restore on Response

On every inbound response, the vault scans the full response body for any masked value it knows and replaces with the original. Handles cases where the model echoes or references a masked value back to the agent.

---

## 12. Wrappers

### claudeproxy (Unix)

```bash
#!/usr/bin/env bash
PROXY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLAUDE_BIN="$(which claude 2>/dev/null)"

if [ -z "$CLAUDE_BIN" ]; then
  echo "[agentproxy] ERROR: claude binary not found in PATH" && exit 1
fi

if ! nc -z 127.0.0.1 7717 2>/dev/null; then
  echo "[agentproxy] Starting proxy daemon..."
  "$PROXY_DIR/bin/agentproxy-start" &
  sleep 1
fi

export HTTP_PROXY=http://127.0.0.1:7717
export HTTPS_PROXY=http://127.0.0.1:7717
export REQUESTS_CA_BUNDLE="$PROXY_DIR/certs/mitmproxy-ca-cert.pem"
export SSL_CERT_FILE="$PROXY_DIR/certs/mitmproxy-ca-cert.pem"
export NODE_EXTRA_CA_CERTS="$PROXY_DIR/certs/mitmproxy-ca-cert.pem"
export AGENTPROXY_AGENT=claude
export AGENTPROXY_SESSION="$(uuidgen)"

exec "$CLAUDE_BIN" "$@"
```

### codexproxy, opencodeproxy, copilotproxy (Unix)

Same structure. Binary detection and `AGENTPROXY_AGENT` value change per agent.

### claudeproxy.cmd (Windows)

```cmd
@echo off
set PROXY_DIR=%~dp0..
set HTTP_PROXY=http://127.0.0.1:7717
set HTTPS_PROXY=http://127.0.0.1:7717
set REQUESTS_CA_BUNDLE=%PROXY_DIR%\certs\mitmproxy-ca-cert.pem
set SSL_CERT_FILE=%PROXY_DIR%\certs\mitmproxy-ca-cert.pem
set NODE_EXTRA_CA_CERTS=%PROXY_DIR%\certs\mitmproxy-ca-cert.pem
set AGENTPROXY_AGENT=claude

netstat -an | find "7717" >nul 2>&1
if errorlevel 1 (
  echo [agentproxy] Starting proxy daemon...
  start /B %PROXY_DIR%\bin\agentproxy-start.cmd
  timeout /t 1 >nul
)

claude %*
```

---

## 13. Installers

### install.sh — Unix (macOS + Linux)

Steps in order:

1. Check Python >= 3.10
2. Create local venv at `agentproxy/.venv`
3. `pip install mitmproxy llm-guard`
4. Generate mitmproxy CA cert (`mitmdump --generate-ca-cert`)
5. Install CA cert into system trust store
   - macOS: `security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain`
   - Linux (Debian/Ubuntu): copy to `/usr/local/share/ca-certificates/` + `update-ca-certificates`
   - Linux (RHEL/Fedora): copy to `/etc/pki/ca-trust/source/anchors/` + `update-ca-trust`
6. Detect agent binaries with `which`: `claude`, `codex`, `opencode`, `gh`
7. Write wrapper scripts into `agentproxy/bin/` for each found binary, skip those not found
8. `chmod +x` all wrapper scripts
9. Add `agentproxy/bin/` to PATH in `~/.zshrc` and `~/.bashrc`
10. Write default config files if not already present
11. Print summary

### install.cmd — Windows

Steps in order:

1. Check `py --version` for Python >= 3.10
2. Create venv, activate, pip install mitmproxy llm-guard
3. Generate mitmproxy CA cert
4. `certutil -addstore Root mitmproxy-ca-cert.pem`
5. Detect agent binaries with `where`: `claude.exe`, `codex.exe`, `opencode.exe`, `gh.exe`
6. Write `.cmd` wrapper files into `agentproxy\bin\` for each found binary
7. `setx PATH "%PATH%;agentproxy\bin"` to add wrappers to user PATH
8. Write default config files
9. Print summary

---

## 14. Logger

Append-only JSONL. One entry per request/response pair. Originals never logged.

```json
{
  "session_id": "uuid",
  "timestamp_req": "2025-03-25T10:42:00.123Z",
  "timestamp_resp": "2025-03-25T10:42:01.456Z",
  "agent": "claude",
  "host": "api.anthropic.com",
  "path": "/v1/messages",
  "endpoint_detected": true,
  "secrets_found": 3,
  "secret_types": ["apikey", "connstr", "env_file"],
  "filenames_detected": [".env", "service-account.json"],
  "filename_actions": ["scan_contents", "flag_and_mask_all"],
  "layers_triggered": ["pattern:ANTHROPIC_API_KEY", "filename:.env", "llmguard:Secrets"],
  "request_body_sanitised": {},
  "response_body_sanitised": {},
  "latency_proxy_ms": 38,
  "llmguard_latency_ms": 12
}
```

---

## 15. Dependencies

| Dependency | Purpose | Install method |
|---|---|---|
| Python >= 3.10 | Runtime | System |
| mitmproxy >= 10 | Proxy core | pip |
| llm-guard | Secret + PII detection (includes Presidio) | pip |
| PyYAML | Config parsing | pip |
| rich | Terminal output | pip |

No LLM model required. No GPU. No ollama. No cloud calls.

---

## 16. Detection Pipeline — Full Flow

```
Inbound request to proxy
        │
        ▼
Is host an AI API? ──No──▶ Forward transparently
        │
       Yes
        ▼
Does path match completion fragment? ──No──▶ Forward transparently
        │
       Yes
        ▼
Decode body (decompress if needed, parse JSON)
        │
        ▼
Extract scan targets (messages, tool args, tool results, suspicious keys)
        │
        ├──▶ Layer 1: Pattern registry (regex)          ──▶ secrets set A
        ├──▶ Layer 2: Structural key-name scan          ──▶ secrets set B
        ├──▶ Layer 3: LLM Guard + Presidio              ──▶ secrets set C
        └──▶ Layer 4: Filename detector                 ──▶ secrets set D (content-level)
                │
                ▼
        Union of A + B + C + D
                │
                ▼
        For each secret found:
          - Register in vault
          - Replace in body with masked form
                │
                ▼
        Re-encode body
                │
                ▼
        Forward sanitised request to remote API
                │
                ▼
        Receive response
                │
                ▼
        Decode response body
                │
                ▼
        Restore vault: scan for masked values, replace with originals
                │
                ▼
        (Async) LLM Guard scan response for new leaks → log warning if found
                │
                ▼
        Return response to agent process
```

---

## 17. Security Considerations

- CA cert installed in user trust store only by default — system-wide is an explicit opt-in flag
- Vault never writes original secret values to disk in plaintext
- Logs contain only masked values
- LLM Guard runs with no internet access
- Proxy binds only to 127.0.0.1 — not network-exposed
- Wrapper scripts set proxy env vars only for the child process — other processes on the machine are unaffected
- Filename scanning is pattern-based — does not read the filesystem itself, only inspects what the agent has already pasted into a prompt

---

## 18. Known Limitations (v0.1)

- Agents that use OS-level system proxy settings rather than env vars may need additional config
- SSE streaming responses are scanned async — a secret arriving in a streamed chunk may reach the agent before the background scan completes
- Vault is lost on proxy crash — masked values in any cached agent context become unrestorable until a new session rebuilds the vault from scratch
- Binary content (images, PDFs sent as base64 blobs) not scanned in v0.1
- Filename detection depends on the filename appearing as text in the prompt — if an agent sends file contents without mentioning the filename, filename-triggered actions will not fire (pattern and LLM Guard layers still run)

---

## 19. Phase Roadmap

### Phase 1 (this spec)
- Proxy wrapper and daemon
- Endpoint detection
- Body decode / encode
- Pattern registry — Layer 1
- Structural key scan — Layer 2
- LLM Guard + Presidio — Layer 3
- Filename detector — Layer 4
- Vault mask and restore
- Structured logger
- Install scripts for macOS, Linux, Windows
- Wrappers: claudeproxy, codexproxy, opencodeproxy, copilotproxy

### Phase 2
- Web UI dashboard — view traffic log, vault contents, secrets by session and agent
- SSE streaming scan improvement — chunk overlap window to catch cross-chunk secrets
- Alert mode — Slack / webhook notification on secret detection
- Policy enforcement — block request entirely instead of masking (per pattern, per filename rule)
- Per-org pattern sharing — export / import pattern YAML

### Phase 3
- Multi-machine — proxy runs on a gateway node, developer machines point to it
- Audit reports — weekly digest of secret types found, agents involved, files referenced
- Secret manager hints — when a secret is detected, suggest which vault entry it might correspond to

---

## 20. agentproxy-detect — Universal Auto-Discovery Wrapper

This is an **additive** wrapper — it does not replace any existing per-agent wrapper. It is the entry point for users who want a single command that figures out what is available and configures everything automatically.

### Purpose

- Discovers which AI agent CLIs and UI tools are installed on the machine
- Detects whether a global/system proxy is already set and whether it conflicts
- Determines the right mechanism for each found tool (env var, VS Code settings, JetBrains vmoptions, Electron flag)
- Starts the AgentProxy daemon if not already running
- Applies non-destructive config to each discovered tool
- Reports exactly what it did and what it skipped

### What it checks, in order

**Step 1 — Check for existing proxy settings**

Before doing anything, detect if a proxy is already configured at any level:

```
Check environment: HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, http_proxy, https_proxy
Check system proxy: macOS networksetup -getwebproxy, Linux gsettings, Windows registry
Check VS Code settings: read http.proxy from settings.json if present
Check JetBrains vmoptions: scan for -Dhttps.proxyHost entries
```

If a global or system proxy is already set:
- Do not override it silently
- Print a warning: what proxy was found, where it was found, what the conflict is
- Offer two options: chain through the existing proxy (upstream proxy config), or skip that tool
- Never silently replace a proxy the user or org may have intentionally set

**Step 2 — Discover CLI agents**

```
which claude        → found: configure claudeproxy env vars
which codex         → found: configure codexproxy env vars
which opencode      → found: configure opencodeproxy env vars
which gh            → check if copilot extension installed → configure copilotproxy env vars
pip show llm        → found: configure generic llmproxy env vars
which aider         → found: configure aiderproxy env vars (openai-compatible)
which continue      → found: note for manual config (uses VS Code extension model)
```

**Step 3 — Discover UI tools**

```
VS Code:
  macOS:   ls /Applications/Visual\ Studio\ Code.app
  Linux:   which code
  Windows: reg query for VS Code install path
  → if found: check for Claude Code extension (check extensions dir for anthropic.claude-code)
  → if found: check for GitHub Copilot extension (github.copilot)
  → patch settings.json if not already proxied

JetBrains:
  macOS:   ls ~/Library/Application\ Support/JetBrains/
  Linux:   ls ~/.config/JetBrains/
  Windows: ls %APPDATA%\JetBrains\
  → for each IDE found: check vmoptions file, patch if not already proxied

Claude standalone Electron:
  macOS:   ls /Applications/Claude.app
  Windows: check %LOCALAPPDATA%\AnthropicClaude
  → note found, provide launch command with --proxy-server flag
```

**Step 4 — Start proxy daemon**

Check if AgentProxy is already running on port 7717. If not, start it. If port 7717 is occupied by something else, report the conflict and suggest an alternate port.

**Step 5 — Report**

Print a structured summary of what was found, what was configured, what was skipped and why:

```
agentproxy-detect — discovery report
─────────────────────────────────────────────────
Existing proxies found:
  none detected

CLI agents configured:
  ✓ claude        → claudeproxy env vars applied
  ✓ codex         → codexproxy env vars applied
  ✗ opencode      → not installed, skipped
  ✗ gh copilot    → gh found but copilot extension not installed

UI tools configured:
  ✓ VS Code       → settings.json patched (Claude Code + Copilot extensions found)
  ✓ IntelliJ IDEA → idea64.vmoptions patched
  ✗ PyCharm       → not installed
  ✗ Claude.app    → found but Electron proxy requires launch flag
                     run: open -a Claude --args --proxy-server=http://127.0.0.1:7717

Proxy daemon:
  ✓ Running on localhost:7717
  ✓ Domains config: ./config/domains.yaml (12 domains intercepted)

Logs: ./logs/traffic.jsonl
─────────────────────────────────────────────────
```

### Conflict handling for global proxy

If the machine already routes through a corporate proxy (common in enterprise):

```yaml
# agentproxy.yaml — upstream proxy chaining
proxy:
  port: 7717
  host: 127.0.0.1
  upstream_proxy: http://corporate-proxy.company.com:8080   # chain through existing proxy
  upstream_proxy_auth:
    username: ""
    password: ""
```

mitmproxy supports upstream proxy chaining natively. AgentProxy sits between the agent and the corporate proxy — the agent sees AgentProxy, AgentProxy forwards to the corporate proxy, which forwards to the API. Detection and masking happen before the corporate proxy ever sees the traffic.

---

## 21. domains.yaml — Domain Allowlist

AgentProxy only intercepts traffic to domains explicitly listed here. All other traffic — regardless of whether a global proxy is set — is forwarded transparently without inspection.

This is the single most important safety control. It prevents AgentProxy from accidentally scanning personal browsing, internal tooling, or unrelated HTTPS traffic if it is ever configured as a system-wide proxy.

```yaml
# domains.yaml
# Only these domains will be intercepted and scanned.
# All other traffic passes through untouched.
# Wildcards supported: *.example.com matches all subdomains.

intercept:

  # --- Anthropic ---
  - domain: api.anthropic.com
    description: Claude API (all versions)
    endpoints:                          # optional: further restrict to specific path prefixes
      - /v1/messages
      - /v1/complete

  # --- OpenAI and compatible ---
  - domain: api.openai.com
    description: OpenAI API
    endpoints:
      - /v1/chat/completions
      - /v1/completions

  - domain: api.openai.azure.com
    description: Azure OpenAI
    endpoints:
      - /openai/deployments

  # --- GitHub Copilot ---
  - domain: api.githubcopilot.com
    description: GitHub Copilot completions

  - domain: copilot-proxy.githubusercontent.com
    description: GitHub Copilot proxy endpoint

  # --- Google ---
  - domain: generativelanguage.googleapis.com
    description: Google Gemini API

  - domain: '*.aiplatform.googleapis.com'
    description: Google Vertex AI

  # --- AWS Bedrock ---
  - domain: bedrock-runtime.*.amazonaws.com
    description: AWS Bedrock runtime (all regions)

  # --- Mistral ---
  - domain: api.mistral.ai
    description: Mistral API

  # --- Cohere ---
  - domain: api.cohere.ai
    description: Cohere API

  # --- Groq ---
  - domain: api.groq.com
    description: Groq API

  # --- Together AI ---
  - domain: api.together.xyz
    description: Together AI

  # --- Perplexity ---
  - domain: api.perplexity.ai
    description: Perplexity API

  # --- Org custom: add internal LLM gateway if applicable ---
  # - domain: llm-gateway.corp.internal
  #   description: Internal LLM gateway

# Explicit passthrough — never intercept these even if a wildcard above would match.
# Use for health check endpoints, auth endpoints, CDN assets, etc.
passthrough:
  - domain: auth.anthropic.com
    reason: OAuth flow — do not intercept auth tokens in URL
  - domain: sentry.io
    reason: Error reporting — not AI traffic
  - domain: '*.cloudfront.net'
    reason: CDN assets
  - domain: '*.fastly.net'
    reason: CDN assets
```

### How domain filtering works in the proxy

```
Request arrives at proxy
        │
        ▼
Is the destination domain in domains.yaml intercept list?
        │
   No ──┤──▶ Is it in passthrough list?
        │           │
        │         Yes ──▶ Forward transparently, no log
        │           │
        │          No ──▶ Forward transparently, log as passthrough
        │
  Yes ──▶ Does the domain have an endpoint list?
                │
           No ──▶ Intercept all paths on this domain
                │
          Yes ──▶ Does the request path match any listed endpoint prefix?
                        │
                   No ──▶ Forward transparently
                        │
                  Yes ──▶ Enter detection pipeline
```

This means: even if AgentProxy is set as a system-wide or global proxy, it will only ever inspect traffic to the domains in this list. A developer's banking site, internal wiki, npm registry, or any other HTTPS traffic passes through without inspection.

---

## 22. SSE Streaming — Full Handling Spec

Server-Sent Events is the standard streaming format for all major AI APIs. Each SSE response is a sequence of frames:

```
data: {"id":"msg_01","type":"content_block_delta","delta":{"text":"Hello"}}

data: {"id":"msg_01","type":"content_block_delta","delta":{"text":", how"}}

data: {"id":"msg_01","type":"content_block_delta","delta":{"text":" can I help"}}

data: [DONE]
```

Each frame arrives as a separate TCP segment. The secret `sk-ant-api03-xxxx` might be split across frames:

```
frame 12: ...and here is the key: sk-ant-
frame 13: api03-ABCDxxxx9999 which you...
```

A naive chunk-by-chunk scan misses this entirely.

### SSE Handling Strategy

**Outbound requests (agent → API):**
Requests are never streamed — they are always a complete JSON body sent in one shot. Full scan, no compromise, no buffering needed.

**Inbound responses (API → agent):**
Three components handle SSE correctly:

**Component 1 — SSE Frame Parser**

Parses the raw byte stream into discrete `data:` frames. Handles:
- Frames split across TCP packets — buffers until a complete `data: {...}\n\n` is found
- Multi-line data fields
- Comment lines (`: heartbeat`)
- `[DONE]` sentinel
- Non-JSON SSE frames (pass through as-is)

**Component 2 — Rolling Window Buffer**

Maintains a sliding window of the last N characters of assembled text across frames. Default window: 512 characters (configurable). Before releasing each frame to the agent, the scanner checks the window — not just the current frame.

```
Window state after frame 12:  "...and here is the key: sk-ant-"
Frame 13 arrives:              "api03-ABCDxxxx9999 which you..."
Window now:                    "...and here is the key: sk-ant-api03-ABCDxxxx9999 which you..."
Scanner runs on window ──────▶ MATCH: ANTHROPIC_API_KEY detected
Action: mask in frame 13, update window with masked value
Release frame 13 to agent with masked text
```

The window is large enough to catch any realistic secret (longest known API key format is ~110 chars). If a secret spans more than 512 chars of frames, the window size can be increased in config.

**Component 3 — Content-Length and Header Rewriting**

When AgentProxy modifies response content (masking a value), the byte length of the body changes. This must be corrected or the agent's HTTP client will hang waiting for bytes that never arrive, or discard bytes it received too many of.

Rules:

```
If response header contains Content-Length:
  → After full body reassembly and masking, recalculate byte length
  → Rewrite Content-Length header with new value before releasing to agent

If response uses Transfer-Encoding: chunked:
  → Each chunk has its own length prefix in the wire format
  → Rewrite each chunk's length prefix after masking that chunk's content
  → Do not set Content-Length (chunked and Content-Length are mutually exclusive)

If response is SSE (Content-Type: text/event-stream):
  → SSE must not use Content-Length — it is inherently open-ended
  → Strip Content-Length header if present (some proxies add it incorrectly)
  → Pass frames through as they are released from the rolling window buffer
  → Each released frame is written to the agent connection immediately — no full-body buffering

If masking changes a value mid-stream:
  → The masked form (first4****last4) is always the same byte length as the mask template
     (4 prefix chars + 24 stars + 4 suffix chars = 32 chars always)
  → This means masking never changes the byte count of the masked portion
  → Content-Length only needs rewriting if the original secret was shorter or longer than 32 chars
  → Log the length delta for every masking event
```

**Masking length invariant — important design note:**

The fixed 32-char mask format (`first4` + 24 stars + `last4`) means:
- A 40-char secret becomes 32 chars → body shrinks by 8 bytes → Content-Length decremented by 8
- A 20-char secret becomes 32 chars → body grows by 12 bytes → Content-Length incremented by 12

This delta is calculated per masking event and accumulated. Final Content-Length = original + sum of all deltas.

For SSE frames specifically: since each frame is released individually and SSE has no Content-Length, only the per-frame chunked length prefix (if chunked encoding is used inside SSE, which is rare) needs updating. In practice SSE over HTTP/2 uses DATA frames with automatic length — no manual rewriting needed.

### SSE Config

```yaml
# agentproxy.yaml additions
sse:
  rolling_window_chars: 512       # how many chars of assembled text to keep in window
  max_frame_buffer_ms: 100        # max time to hold a frame waiting for window to fill
  strip_content_length: true      # remove Content-Length on SSE responses
  rewrite_chunk_lengths: true     # fix chunked transfer encoding length prefixes
```

### What this looks like end to end for a streaming response

```
API sends SSE stream
        │
        ▼
SSE Frame Parser receives raw bytes
  → buffers until complete frame boundary found
  → emits complete data: frames one at a time
        │
        ▼
Rolling Window Buffer
  → appends frame text to window
  → window now contains last 512 chars of assembled stream
        │
        ▼
Scanner runs on window content
  → Layer 1 regex (fast)
  → Layer 2 key-name (fast)
  → LLM Guard (fast, sync)
        │
   No match ──▶ Release current frame to agent as-is
        │
   Match ──▶ Identify which portion of current frame contains the secret
             (may be whole frame, may be partial, may span into previous frame)
             Mask the matched portion
             Update vault
             Rewrite chunked length prefix if needed
             Release modified frame to agent
        │
        ▼
Agent receives frame — sees masked value
Next frame arrives — cycle repeats
        │
        ▼
[DONE] frame received
  → Flush window
  → Run final scan on any buffered remainder
  → Release [DONE] to agent
  → Log completed request/response pair
```

---

## 23. Updated agentproxy.yaml — Full Reference

```yaml
proxy:
  port: 7717
  host: 127.0.0.1
  upstream_proxy: ""              # set if chaining through corporate proxy
  upstream_proxy_auth:
    username: ""
    password: ""

tls:
  cert_dir: ./certs

domains:
  config_file: ./config/domains.yaml   # domain allowlist — only these are intercepted

detection:
  pattern_file: ./config/patterns.yaml
  filename_file: ./config/filenames.yaml
  structural_key_scan: true
  llmguard_enabled: true

masking:
  show_prefix_chars: 4
  show_suffix_chars: 4
  star_length: 24                 # total mask is always 4 + 24 + 4 = 32 chars

sse:
  rolling_window_chars: 512
  max_frame_buffer_ms: 100
  strip_content_length: true
  rewrite_chunk_lengths: true

logging:
  log_file: ./logs/traffic.jsonl
  log_request_body: true
  log_response_body: true
  log_originals: false

org:
  name: ""
```
