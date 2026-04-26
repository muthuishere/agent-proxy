# Launch Slices — Index

Vertical slices for AgentProxy v1 launch. Each slice ships a user-visible outcome end-to-end. Each spec follows OpenSpec layout (Why / What Changes / Impact / Tasks / Done When) and includes a `Team Review — 2026-04-25` section with audit findings and elevated tasks.

| # | Slice | Spec | Hard Dependencies |
|---|-------|------|-------------------|
| 01 | Install via npm + GoReleaser | [spec-slice-01-install-via-npm-and-goreleaser.md](./spec-slice-01-install-via-npm-and-goreleaser.md) | `go.mod` fix (P0 inside slice) |
| 02 | One-click setup and start | [spec-slice-02-one-click-setup-and-start.md](./spec-slice-02-one-click-setup-and-start.md) | Slice 01 |
| 03 | Dashboard request/response diff | [spec-slice-03-dashboard-request-response-diff.md](./spec-slice-03-dashboard-request-response-diff.md) | Slice 02; request-id thread (P0 inside slice) |
| 04 | Claude end-to-end, non-disruptive | [spec-slice-04-claude-end-to-end.md](./spec-slice-04-claude-end-to-end.md) | Slice 02, Slice 03; **conditional CONNECT MITM (P0)** |
| 05 | Copilot end-to-end, non-disruptive | [spec-slice-05-copilot-end-to-end.md](./spec-slice-05-copilot-end-to-end.md) | Slice 04 P0 (conditional MITM) |
| 06 | Codex end-to-end, non-disruptive | [spec-slice-06-codex-end-to-end.md](./spec-slice-06-codex-end-to-end.md) | Slice 04 P0; resolve `spec-codex-websocket-fix.md` |
| 07 | Marketing & launch plan | [spec-slice-07-marketing-launch-plan.md](./spec-slice-07-marketing-launch-plan.md) | Slices 01–06 demo-able |

## Cross-cutting P0 fixes (do these first or nothing else lands)

These three landed as P0 tasks inside the slices but are listed here because they each unblock multiple slices:

1. ~~**`go.mod` module path fix**~~ **DONE 2026-04-25** — `module github.com/muthuishere/agentproxy`; build clean.
2. ~~**Conditional CONNECT MITM**~~ **DONE 2026-04-25** — `internal/proxy/server.go:32` now uses `HandleConnectFunc` with `service.ShouldIntercept`; regression test `TestConditionalConnectMITM` pins it. Firebase/Razorpay-class non-intercepted hosts now pass through TCP without MITM.
3. ~~**Request IDs end-to-end**~~ **DONE 2026-04-25** — `event.RequestID` added; minted in proxy server (`<session>-<atomic counter>`), threaded via `ctx.UserData` to response hook, persisted to `traffic.jsonl` as `id`. Pinned by `TestTrafficLoggerEmitsRequestID`.

## Suggested sequencing

```
[01 + go.mod fix] → [02] → [03 + request-id]
                                 ↓
              [04 + conditional MITM]
                ↙        ↘
             [05]       [06]
                ↘       ↙
                  [07]
```

Slices 05 and 06 can run in parallel after Slice 04's conditional-MITM fix lands.
