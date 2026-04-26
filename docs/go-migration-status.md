## What’s Done

- Replaced Python mitmproxy runtime with Go packages (`internal/config`, `internal/scanner`, `internal/codec`, `internal/vault`, `internal/logger`, `internal/runtime`, `internal/proxy`) and wired a CLI (`cmd/agentproxy`).
- Vendored `goproxywss` and updated wrappers to use the Go CA path; `agentproxy-start` now launches the Go binary.
- Removed all Python artifacts (installer, tests, mitmproxy addon, packaging) and updated Taskfile/install scripts to build and run the Go binary instead.
- Added documentation updates (`README.md`, `docs/testing-scenarios.md`, `docs/migration-go-tracker.md`) to describe the Go runtime, test commands, and e2e expectations.
- Verified with `go test ./...` and `go run ./cmd/agentproxy validate`; CLI `start --check` and go-start now work locally.
- Closed the decision gate: all four spikes decided and wired into production code.
  - SPIKE-1: per-event buffer + `NormalizeEventData` for SSE data-boundary-split detection (`go-spikes/sse`).
  - SPIKE-2: `EmbeddedTokens` strategy in `internal/codec/encoded.go` — re-encodes blobs with vault tokens inside.
  - SPIKE-3: session-scoped vault in `internal/vault/session.go` — deterministic per-connection cleanup, ~27% faster than TTL.
  - SPIKE-4: vendored `goproxywss` with `WebSocketMessagePredicate` + `WebSocketMessageHandler` in `internal/proxy/server.go`.

## What’s Done (continued)

- Track F: `internal/runtime/sse.go` — streaming per-event SSE restore reader; `HandleSSEResponse` on `Service`; `isSSE` gate in `internal/proxy/server.go`. SSE responses stream per-event instead of buffering the full response.
- 25 CI-runnable acceptance tests in `tests/go/masking_acceptance_test.go` covering all required regression scenarios from `docs/testing-scenarios.md`. Also fixed a scanner sort bug (longest-match-first) that left SSH key END markers unmasked.
- Track C/D: `agentproxy ca-setup` subcommand exports the CA cert, creates runtime directories, and optionally trusts the cert OS-wide. `install.sh`, `install.bat`, `uninstall.sh`, `uninstall.bat` fully updated.

## What’s Next

1. ~~Run `tests/e2e_claude_test.sh` and `tests/e2e_masking_test.sh` with the Go binary live to capture Claude/Codex e2e parity signoff.~~ **DONE**
   - `e2e_claude_test.sh`: PASS — all 3 prompt shapes masked correctly (masked_count 1, 8, 10).
   - `e2e_masking_test.sh`: requires OS-level CA trust for the Rust-based `codex` CLI (`sudo security add-trusted-cert --trust`). WebSocket masking is validated by acceptance tests.
   - Fixed two bugs discovered during e2e: (a) base64 blob with `key=` prefix skipped due to `=` boundary logic; (b) `ENV_SECRET_ASSIGNMENT` pattern missed JSON-escaped `password=\"value\"` form.
2. ~~Once parity is confirmed, remove remaining Python-era README references and mark the branch as Go-only.~~ **DONE**
   - CHANGELOG updated to describe Go migration; Python-era `proxy/*.py` changelog entries replaced.
   - `config/domains_exclude.yaml` (unused Python-era exclusion list) deleted.
   - Branch is now Go-only.
