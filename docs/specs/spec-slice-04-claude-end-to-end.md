# Slice 04 — Claude End-to-End, Non-Disruptive

**Status:** Proposed
**Owner:** _tbd_
**Depends on:** Slice 02, Slice 03
**Demo:** With AgentProxy installed and started, the user runs `claudeproxy <prompt>` and gets a normal Claude Code response. Simultaneously, an unrelated app on the same machine (e.g. browser hitting Firebase or a Razorpay redirect) keeps working without disruption.

---

## Why

Claude is the primary first-class target. We've verified basic masking works (`spec-claude-support-status.md`), but two things must hold for "v1 ready":

1. **End-to-end works** — real `claude` CLI usage via `claudeproxy` produces correct, masked, restored traffic against `api.anthropic.com`, including streaming SSE.
2. **Doesn't disrupt anything else** — the trusted CA's existence on the system must not break unrelated apps. The `HTTP_PROXY` env vars are scoped to the wrapper process only, so other apps don't go through MITM unless they explicitly opt in.

The 2026-04-08 huddle flagged a Firebase/Razorpay regression. This slice closes that loop with an explicit non-disruption test.

---

## What Changes

- Targeted gaps from `spec-claude-support-status.md` closed:
  - SSE cross-event-boundary surrogate restore: small lookback buffer in `internal/runtime/sse.go` + test.
  - `tool_use` / `tool_result` round-trip acceptance test.
  - Image-block (large base64) pass-through test confirming we don't corrupt `image/jpeg` / `image/png` payloads.
  - Error-response (`{"type":"error",…}`) pass-through test.
- Wrapper hardening for `bin/claudeproxy`:
  - Confirm `HTTP_PROXY` is unset on parent shell exit (already true via `exec`, but document and test).
  - Add `--no-proxy` flag passthrough so user can temporarily bypass (`claudeproxy --no-proxy …`).
- New non-disruption test harness under `tests/integration/non-disruption/`:
  - With proxy running and CA trusted, an unrelated `curl` to `https://www.googleapis.com/...` from a fresh shell (no `HTTP_PROXY`) must succeed unchanged (no MITM, no cert error).
  - Same harness exercises Firebase and Razorpay-style redirect URLs as smoke targets.

---

## Impact

- **Affected:** `internal/runtime/sse.go`, `tests/go/`, `tests/integration/`, `bin/claudeproxy`, `docs/specs/spec-claude-support-status.md` (rows graduate to ✅).
- **Not affected:** install flow, dashboard, other providers.

---

## Tasks

1. Implement lookback buffer in `sseRestoreReader` to handle a surrogate split across `\n\n` event boundaries.
2. ~~Add `TestSSESurrogateAcrossEventBoundary` in `tests/go/`.~~ **DONE 2026-04-25** — `TestOpenAIKey_SSE_SurrogateSplitAcrossEvents` + `TestOpenAIKey_SSE_SurrogateFragmentedIntoManyEvents` cover this.
3. ~~Add `TestClaudeToolUseRoundTrip`~~ **DONE 2026-04-25** — `TestClaudeShape_ToolUseInputRoundTrip` in `tests/go/claude_shapes_test.go`.
4. ~~Add `TestClaudeImageBlockPassThrough`~~ **DONE 2026-04-25** — same file, `TestClaudeShape_ImageBlockPassThrough`.
5. ~~Add `TestClaudeErrorResponsePassThrough`~~ **DONE 2026-04-25** — same file, plus `TestClaudeShape_SSEErrorEventPassThrough`.
6. ~~Wire `claudeproxy --no-proxy` to skip env-var setting and exec the agent directly.~~ **DONE 2026-04-25** — also added to `codexproxy` and `copilotproxy`.
7. Add `tests/integration/non-disruption/test-non-disruption.sh`:
   - Pre: proxy running, CA trusted.
   - Steps: from a fresh subshell with empty `HTTP_PROXY`, run `curl -fsS https://www.googleapis.com/discovery/v1/apis` and verify 200.
   - Steps: same for `https://api.razorpay.com/v1/` (expect non-200 but reachable, no TLS error).
8. Wire the non-disruption test into Taskfile as `task test-non-disruption` (manual, not in CI by default).
9. Update `spec-claude-support-status.md`: graduate the four ❌/⚠️ rows to ✅ once tests merge; record in "Recently Closed".
10. Add a "Claude verified" section in README pointing at `claudeproxy --version` plus a screenshot of the dashboard detail view from Slice 03.

---

## Done When

- [ ] `task test-acceptance` passes including the four new Claude tests.
- [ ] `task test-non-disruption` passes with the proxy running and CA trusted.
- [ ] Manual run: `claudeproxy 'list files in current dir'` succeeds; dashboard shows the call with masking applied; `tool_use` for `Bash` is visible and correctly restored.
- [ ] Manual run from a *separate* terminal (no `HTTP_PROXY`): a Firebase or Razorpay-style HTTPS request succeeds without TLS errors.
- [ ] All Claude rows in `spec-claude-support-status.md` are ✅.

---

## Team Review — 2026-04-25

### P0 — THE blocker for non-disruption (elevated to top)

**Verified:** `internal/proxy/server.go:32` does `handler.OnRequest().HandleConnect(goproxy.AlwaysMitm)`. CONNECT MITM is **unconditional**. The `ShouldIntercept(host)` check at line 86 only filters which traffic is *masked*; it does **not** prevent MITM. Once the agent's wrapper sets `HTTP_PROXY=http://127.0.0.1:7717`, *every* CONNECT — Firebase, Razorpay, Stripe, anything — gets MITM'd with our local CA. That's why the 2026-04-08 huddle saw Firebase/Razorpay redirects fail.

**This makes the "non-disruption" claim of slices 04/05/06 false today.** The fix lives here in Slice 04 because Claude is the first to need it.

### Tasks added (elevated to P0)

11. ~~**P0 — Conditional CONNECT MITM.** Replace `goproxy.AlwaysMitm` with a custom `HandleConnect` func that calls `service.ShouldIntercept(host)`. If false → return `goproxy.OkConnect` (or equivalent pass-through). If true → MITM as today. Add a unit test that drives a CONNECT to a non-intercepted host and asserts no MITM was attempted.~~ **DONE 2026-04-25** — `internal/proxy/server.go:32` now uses `HandleConnectFunc` with `service.ShouldIntercept`; regression test in `internal/proxy/server_test.go:TestConditionalConnectMITM` passes.
12. **P0** Verify with `tests/integration/non-disruption/` that with proxy running + `HTTP_PROXY` set, a CONNECT to `firebase.googleapis.com` and `api.razorpay.com` succeeds without TLS error.
13. **P1** `--no-proxy` flag does not exist in any wrapper today — confirmed. Implement uniformly.
14. **P1** Consider adding an Anthropic-specific `ParseRequest` that explicitly walks `messages[].content[]` (text + tool_use + image blocks). Today everything goes through generic JSON walking; works but no targeted tests.

### Spec corrections

- **Wrapper `exec` semantics:** confirmed correct — `HTTP_PROXY` is naturally scoped to the wrapper subprocess (and any child it spawns) because the wrapper `exec`s the agent. Other terminals/apps inherit nothing. The non-disruption test must therefore explicitly run *from a fresh shell* with no `HTTP_PROXY` set, otherwise it isn't testing what we claim.
- The non-disruption gap is purely caused by Task 11 above (unconditional MITM), not by env-var leakage.
