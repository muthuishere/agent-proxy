# Changelog

All notable changes to AgentProxy are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Added
- Upstream proxy support — AgentProxy now chains through an existing corporate or enterprise proxy. Resolves from explicit config, PAC file, or auto-detected env/system settings (`HTTP_PROXY`, `HTTPS_PROXY`, macOS System Preferences, Windows registry). PAC file evaluation requires the optional `pypac` dependency.
- WebSocket frame scanning — client→server frames are masked, server→client frames are restored.
- Gzip body support — requests and responses with `Content-Encoding: gzip` are transparently decompressed, scanned, re-compressed.
- Base64 / hex / URL-encoded blob detection — secrets hidden inside encoded blobs are detected and masked, up to 4 levels of nesting.
- `log_passthrough` config flag — non-intercepted domain traffic can be optionally logged for debugging (default: off).

### Changed
- **Single config file** — domains are now defined directly in `agentproxy.yaml` under `detection.intercepted_domains`. The separate `config/domains.yaml` file has been removed.
- Domain detection simplified to explicit allowlist only — no heuristics, no exclusion lists. Only domains listed in `intercepted_domains` are intercepted.
- `proxy/detector.py` — `load_domains()` now reads from the config dict instead of a separate file; graceful fallback to built-in defaults if the list is absent or empty.
- `proxy/addon.py` — domains stored as instance variable (`self._domains`); removed global state.
- `proxy/scanner.py` — removed unused `filename_file` parameter and `filename_action()` method.
- `proxy/logger.py` — fixed attribute/method name collision (`_log_passthrough`); removed dead `log_discovered()` method; added `OSError` guard on file writes.
- `proxy/decoder.py` — removed unused `extract_text_fields()` and `_walk()` functions.

### Fixed
- `TypeError: 'bool' object is not callable` when calling `logger.log_passthrough()` — caused by instance attribute shadowing the method of the same name.

---

## [0.1.0] — 2026-03-25

### Added
- Initial release.
- HTTP and HTTPS interception via mitmproxy addon.
- Secret masking with vault token pattern: `{prefix}****{suffix}[{name}:{digest}]`.
- Token restoration in responses.
- Regex pattern scanner with configurable `config/patterns.yaml`.
- Domain allowlist (`intercepted_domains` in `agentproxy.yaml`).
- Built-in default domains: Anthropic, OpenAI, ChatGPT, GitHub Copilot.
- Structured JSONL traffic log (`logs/traffic.jsonl`).
- Wrapper scripts: `claudeproxy`, `codexproxy`, `copilotproxy`.
- `task install` / `task uninstall` / `task start` / `task test`.
- 68-test pytest suite covering addon pipeline, scanner, vault, decoder, detector, upstream proxy.
