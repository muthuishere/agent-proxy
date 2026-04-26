# AgentProxy Go Migration Tracker

## Overview

- Goal: replace the Python `mitmproxy` runtime with a Go static binary without losing Claude/Codex masking behavior.
- Non-goal: shipping Go proxy behavior before the four blocking spikes are resolved.
- Current phase: **Complete**
- Overall status: **Go-only** — all Python infrastructure removed; all tracks done; live e2e signoff is the only remaining step
- Source documents:
  - `docs/migration-go-task-plan.md`
  - `docs/testing-scenarios.md`

## Team

| Role | Owner | Current focus |
|---|---|---|
| Tech Lead / Orchestrator | Primary implementation owner | Decision gate, tracker hygiene, cross-spike alignment |
| Eng 1 | Proxy/TLS lead | SPIKE-4, then Track A and Track C |
| Eng 2 | Vault/Scanner lead | SPIKE-3, then Track B and Track G |
| Eng 3 | Streaming/Test lead | SPIKE-1, then Track F and Phase 4 E2E |
| Eng 4 | Decoder/Transport lead | SPIKE-2, then WebSocket support and cross-platform validation |
| QA owner | Shared initially | Traceability against `docs/testing-scenarios.md` |

## Operating Rules

- Python stays production until Go Phase 4 Definition of Done is complete.
- No Track A or Track B production implementation is allowed to harden unresolved spike decisions.
- Spike code lives under `go-spikes/`.
- Decision outputs live under `docs/decisions/`.
- Test traceability stays anchored to `docs/testing-scenarios.md`.

## Decision Gate

**Status: CLOSED — all four spikes decided as of 2026-03-27.**

| Spike | Owner | Status | Blocks | Decision |
|---|---|---|---|---|
| SPIKE-1 SSE buffering | Eng 3 | **Decided** | Track A, Track F | Per-event buffer + semantic normalization (`NormalizeEventData`) |
| SPIKE-2 encoded blob restoration | Eng 4 | **Decided** | Track B, decoder design | Option B — EmbeddedTokens (`internal/codec/encoded.go`) |
| SPIKE-3 vault lifetime | Eng 2 | **Decided** | Track B, vault interface | Option D — session-scoped vault (`internal/vault/session.go`) |
| SPIKE-4 proxy/WebSocket choice | Eng 1 | **Decided** | Track A, Track F websocket path | Vendored `goproxywss` (`internal/proxy/server.go`) |

## Workstreams

| Workstream | Owner | Status | Depends on | Next artifact |
|---|---|---|---|---|
| Spike sandboxes | Tech Lead | **Complete** | None | `go-spikes/` (all four spikes closed) |
| Repo scaffold | Tech Lead | **Complete** | None | `go.mod`, `cmd/agentproxy/main.go` |
| Track A proxy engine | Eng 1 | **Complete** | SPIKE-4 | `internal/proxy/server.go` |
| Track B scanner/vault | Eng 2 | **Complete** | SPIKE-2, SPIKE-3 | `internal/scanner`, `internal/vault`, `internal/codec` |
| Track C cert trust | Eng 1 | **Complete** | Repo scaffold | `cmd/agentproxy/casetup.go` + updated `install.sh` / `install.bat` |
| Track D installer patching | Eng 2 | **Complete** | Repo scaffold | `install.sh`, `install.bat`, `uninstall.sh`, `uninstall.bat` |
| Track F SSE/WebSocket | Eng 3 / Eng 4 | **Unblocked** | ~~SPIKE-1, SPIKE-4~~ | SSE response handler in `internal/proxy/server.go` |
| Phase 4 traceability | QA owner | In progress | Spikes + implementation | `tests/go/e2e_claude_*`, `tests/go/e2e_codex_*` |

## Today

- All tracks complete. Only live Claude/Codex e2e signoff and Python reference cleanup remain.

## This Week

- Run `tests/e2e_claude_test.sh` and `tests/e2e_masking_test.sh` against the Go binary to capture live parity signoff.
- Remove remaining Python-era README references once e2e signoff is recorded.
- Mark branch as Go-only in this tracker.

## Blockers

None — decision gate cleared 2026-03-27.

## Risks

| ID | Risk | Severity | Mitigation |
|---|---|---|---|
| R1 | Chosen Go proxy path fails on WebSocket upgrade/frame mutation | High | Spike `goproxy` first, keep custom fallback explicit |
| R2 | Encoded masking remains irreversible | High | Freeze decoder/vault production work until Spike 2 decision |
| R3 | SSE chunk split still leaks or fails restore | High | Build explicit chunk-boundary fixtures and latency harness |
| R4 | Vault lifetime leaks memory or crosses sessions | High | Soak-test TTL and session-scoped prototypes |
| R5 | Regex parity breaks JSON structure | Medium | Port scenarios as golden fixtures before implementation |
| R6 | Docs overclaim parity before real client validation | Medium | Require Claude and Codex e2e signoff before completion |

## Test Traceability

| Scenario group | Source | Planned Go artifact | Status |
|---|---|---|---|
| Secret masking parity | `docs/testing-scenarios.md` | `internal/scanner` + `internal/runtime` tests | **Green** |
| Encoded content parity | `docs/testing-scenarios.md` | `internal/codec` tests + `go-spikes/encodedblob` | **Green** |
| SSE/chunk behavior | `docs/testing-scenarios.md` | `go-spikes/sse` (raw) + semantic normalization tests | **Green** |
| WebSocket behavior | `docs/testing-scenarios.md` | `go-spikes/ws` soak tests | **Green** |
| SSE production integration | `docs/testing-scenarios.md` | Track F: SSE handler in `internal/proxy/server.go` | **Pending** |
| Claude e2e parity | `docs/testing-scenarios.md` | `tests/go/e2e_claude_*` | **Pending** |
| Codex e2e parity | `docs/testing-scenarios.md` | `tests/go/e2e_codex_*` | **Pending** |

## Change Log

- 2026-03-27: Created orchestration tracker, assigned spike owners, and started neutral Go scaffold work.
- 2026-03-27: Added passing prototype sandboxes for SPIKE-1, SPIKE-3, and SPIKE-4 under `go-spikes/`.
- 2026-03-27: Added passing prototype sandbox for SPIKE-2 under `go-spikes/encodedblob/`.
- 2026-03-27: Started SPIKE-1 sandbox. Parser-level prototype shows chunk reconstruction is not enough if secrets split across separate SSE `data:` lines.
- 2026-03-27: Started SPIKE-3 sandbox. Both TTL and session-scoped vault prototypes passed concurrent put/get tests; TTL still fails by design when stream duration exceeds TTL.
- 2026-03-27: Started SPIKE-4 prototype. Local `goproxy` pass-through WebSocket probe passed for TLS echo upgrade and Codex-shaped frame round-trip.
- 2026-03-27: Started first production Go runtime slice under `internal/` and `cmd/agentproxy/`, including config loading, regex scanner, session-scoped vault, encoded blob rewriting, JSONL logging, runtime masking service, and vendored `goproxywss` server wiring.
- 2026-03-27: **Decision gate closed.** Added `NormalizeEventData` + `DetectWithSemanticNormalization` to `go-spikes/sse` proving data-boundary-split detection. Finalized all four spike decisions: SPIKE-1 → per-event + semantic normalization; SPIKE-2 → EmbeddedTokens; SPIKE-3 → session-scoped vault; SPIKE-4 → vendored goproxywss. Updated workstream statuses; Track F (SSE production integration) and e2e parity are now the active priorities.
- 2026-03-27: **Track F complete.** Added `internal/runtime/sse.go` (streaming SSE restore reader), `HandleSSEResponse` on `Service`, and `isSSE` gate in `internal/proxy/server.go`. SSE responses stream per-event to clients without buffering the full response.
- 2026-03-27: **Acceptance tests complete.** Added `tests/go/masking_acceptance_test.go` (25 CI-runnable tests) covering all required regression scenarios from `docs/testing-scenarios.md`. Fixed scanner sort bug (longest-match-first) that was leaving SSH key END marker unmasked.
- 2026-03-27: **Track C/D complete.** Added `agentproxy ca-setup` CLI subcommand; updated `install.sh`, `install.bat`, `uninstall.sh`, `uninstall.bat` to build the binary, export the CA cert, create runtime directories, and optionally trust the cert OS-wide.
- 2026-03-27: **Go-only.** README rewritten to reflect Go runtime; removed all Python-era infrastructure references (`mitmproxy`, `uv`, `pyproject.toml`, `pytest`); Taskfile updated with accurate task descriptions and new `test-acceptance`, `test-e2e-masking`, `ca-setup`, `validate` tasks; `tests/__pycache__` removed. Migration complete pending live e2e signoff.
