# SPIKE-3 Decision: Vault Lifetime and Scope

## Status

- Owner: Eng 2
- Status: **Decided**
- Blocks: Track B, vault interface

## Question

What vault lifetime model supports restoration without creating unbounded memory growth or cross-request contamination?

## Options Under Test

- Option C: TTL-based eviction
- Option D: session-scoped vault

## Required Inputs

- 1000-goroutine concurrency soak
- Restore correctness under long-lived streams
- Memory and cleanup behavior

## Decision

- Chosen option: **Option D — session-scoped vault**
  (implemented as `vault.Manager` + `vault.Session` in `internal/vault/session.go`)
- Why: session scope eliminates the TTL failure mode (restore fails if the stream outlives the
  TTL), cleanly isolates tokens by connection, and benchmarks ~27% faster than TTL eviction.
  Memory is bounded by connection count; `Manager.CloseSession` is called when the proxy
  tears down a connection, giving deterministic cleanup.

## Evidence

- Prototype path: `go-spikes/vaultscope/`
- Validation command: `go test ./go-spikes/vaultscope -v`
- Benchmark command: `go test ./go-spikes/vaultscope -bench=.`
- Production implementation: `internal/vault/session.go`
- Notes:
  - Both models passed 1000-goroutine concurrent put/get soak in the prototype.
  - TTL vault has an explicit failure mode when stream duration exceeds TTL — unacceptable for
    long-lived SSE streams or Codex WebSocket sessions.
  - Local benchmark on 2026-03-27:
    - TTL vault put/get: `2566 ns/op`
    - session vault put/get: `1822 ns/op`
  - Session vault is the active production path; TTL prototype lives in `go-spikes/vaultscope`
    for reference only.

## Follow-up Contract

- Vault constructor: `vault.NewManager(vault.Options{...})`
- Per-request session: `manager.Session(sessionID)` — creates or returns existing `*Session`
- Cleanup: `manager.CloseSession(sessionID)` — called by proxy on connection close
- Config: `masking.show_prefix_chars`, `masking.show_suffix_chars`, `masking.star_length`
