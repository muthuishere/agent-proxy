# Changelog

All notable changes to AgentProxy are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Added
- Upstream proxy support — AgentProxy now chains through an existing corporate or enterprise proxy. Resolves from explicit config, PAC file, or auto-detected env/system settings (`HTTP_PROXY`, `HTTPS_PROXY`, macOS System Preferences, Windows registry).
- WebSocket frame scanning — client→server frames are masked, server→client frames are restored.
- Gzip body support — requests and responses with `Content-Encoding: gzip` are transparently decompressed, scanned, re-compressed.
- Base64 / hex / URL-encoded blob detection — secrets hidden inside encoded blobs are detected and masked, up to 4 levels of nesting.
- `log_passthrough` config flag — non-intercepted domain traffic can be optionally logged for debugging (default: off).
- SSE streaming support — secrets in server-sent event streams are restored per-event rather than buffering the full response.
- Web dashboard on port 7718 — live traffic viewer and metrics.
- `status` subcommand — checks whether the proxy TCP listener is up for wrapper liveness checks and scripting.

### Changed
- **Rewritten in Go** — replaced the Python/mitmproxy runtime with a single static binary (`agentproxy`). No Python, no uv, no mitmproxy dependency.
- **Single config file** — domains are now defined directly in `agentproxy.yaml` under `detection.intercepted_domains`. The separate `config/domains.yaml` file has been removed.
- Domain detection simplified to explicit allowlist only — no heuristics, no exclusion lists. Only domains listed in `intercepted_domains` are intercepted.
- Install scripts (`install.sh`, `install.bat`) now build the Go binary and export the CA cert; no virtualenv or pip steps.
- Installers now attempt CA trust automatically on interactive runs, and wrappers fail fast with a clear message when the proxy is down.
- Uninstallers now remove the AgentProxy CA from the OS trust store by default; use `--keep-trust` to leave trust installed intentionally.
- Wrapper env vars now use `AGENTPROXY_HOST` / `AGENTPROXY_PORT` for namespaced configuration.
- Vault is now session-scoped — per-request cleanup with no unbounded growth (fixed a known Python-era gap).
- Encoded-blob masking is now fully reversible via `EmbeddedTokens` strategy (fixed a known Python-era gap where `[MASKED:...]` markers were irreversible).
- Cross-chunk SSE secret splitting is now handled correctly (fixed a known Python-era gap).
- Visible star-mask placeholders have been replaced with deterministic, shape-preserving vault surrogates so LLM providers do not infer that credentials were manually redacted.

### Fixed
- Base64 blob preceded by `key=` assignment operator was skipped due to `=` being treated as a base64 token boundary character.
- JSON-escaped quoted password values (`password=\"value\"`) now detected in pass-1 scan, not silently skipped.

---

## [0.1.0] — 2026-03-25

### Added
- Initial release (Python/mitmproxy implementation).
- HTTP and HTTPS interception via mitmproxy addon.
- Secret masking with vault token pattern: `{prefix}****{suffix}[{name}:{digest}]`.
- Token restoration in responses.
- Regex pattern scanner with configurable `config/patterns.yaml`.
- Domain allowlist (`intercepted_domains` in `agentproxy.yaml`).
- Built-in default domains: Anthropic, OpenAI, ChatGPT, GitHub Copilot.
- Structured JSONL traffic log (`logs/traffic.jsonl`).
- Wrapper scripts: `claudeproxy`, `codexproxy`, `copilotproxy`.
- `task install` / `task uninstall` / `task start` / `task test`.
