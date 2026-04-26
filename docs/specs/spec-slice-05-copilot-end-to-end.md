# Slice 05 — Copilot End-to-End, Non-Disruptive

**Status:** Proposed
**Owner:** _tbd_
**Depends on:** Slice 02, Slice 03
**Demo:** With AgentProxy installed and started, the user runs `copilotproxy <prompt>` (which execs `gh copilot suggest` / `gh copilot explain`) and gets a normal Copilot response. Other apps and other `gh` commands are unaffected.

---

## Why

Copilot is the second supported agent. There's an existing `internal/provider/copilot.go` and `spec-copilot-fix.md`, plus a `copilotproxy` wrapper. Reaching launch-quality requires:

1. End-to-end with the real `gh copilot` CLI (verified manually, ideally automated).
2. Provider parser correctness — Copilot's request shape differs from Anthropic/OpenAI; confirm secret detection and surrogate replacement work on its actual payloads.
3. Non-disruption: trusted CA + proxy running must not break unrelated `gh` commands (`gh pr create`, `gh repo clone`) when those are run from a shell *without* `HTTP_PROXY`.

---

## What Changes

- Acceptance tests in `tests/go/` covering Copilot request/response shapes (suggest, explain, chat).
- Verify and, if needed, extend `internal/provider/copilot.go` to handle current API shape (latest as of release date).
- Wrapper hardening for `bin/copilotproxy`:
  - Liveness check (already present).
  - `--no-proxy` flag for bypass.
  - Confirm `gh` auth flow (which makes its own HTTPS calls to `github.com` and `api.github.com`) is included in `intercepted_domains` only if we want to mask there — otherwise must pass through cleanly.
- Decision recorded: do we intercept `api.github.com` (broader scope, catches gh tokens in transit) or only `api.githubcopilot.com` (narrow, won't disrupt regular `gh` usage). Default = narrow.
- Non-disruption test: with proxy + trusted CA, run `gh repo view <some public repo>` from a fresh shell — must work unchanged.

---

## Impact

- **Affected:** `internal/provider/copilot.go`, `tests/go/`, `bin/copilotproxy`, `config/agentproxy.yaml` (intercepted_domains decision), `docs/specs/spec-copilot-fix.md` (mark complete).
- **Not affected:** Claude/Codex paths, install flow, dashboard.

---

## Tasks

1. **PARTIAL (2026-04-25)** — synthetic-fixture stand-ins added in
   `tests/go/copilot_shapes_test.go` (OpenAI-chat-shaped). Real `gh copilot
   suggest` / `gh copilot explain` capture against `api.githubcopilot.com`
   still needed; no fixtures in `tests/go/fixtures/copilot/` yet.
2. Add `TestCopilotSuggestRoundTrip` and `TestCopilotExplainRoundTrip` against fixtures.
3. Add `TestCopilotChatStream` if Copilot uses SSE/chunked responses.
4. **DONE (2026-04-25)** — `internal/provider/copilot.go` confirmed to
   inherit `DefaultProvider.ParseRequest` (generic JSON walker). Decision
   recorded: fine for v1 since masking is text-based and the walker covers
   nested fields. Domain list narrowed to Copilot-only hosts.
5. **DONE (2026-04-25)** — `config/agentproxy.yaml` updated to drop
   `api.github.com` with an inline comment explaining the rationale;
   `CopilotProvider.Domains()` and `provider_test.go` updated to match.
6. Wire `copilotproxy --no-proxy`.
7. **DONE (2026-04-25)** — Go-level non-disruption coverage in
   `TestCopilotApiGitHubComPassesThrough` asserts `ShouldIntercept("api.github.com") == false`
   and that bodies pass through byte-for-byte. Live `gh repo view cli/cli`
   verification still requires manual e2e against a running proxy.
8. **DONE (2026-04-25)** — `docs/specs/spec-copilot-fix.md` Status section
   updated with what's resolved and what still needs real traffic.
9. Add Copilot screenshot/snippet to README "Verified Agents" section.

---

## Done When

- [ ] `task test-acceptance` passes Copilot tests.
- [ ] Manual: `copilotproxy 'how do I rebase'` returns a Copilot suggestion; dashboard shows the call masked + restored.
- [ ] Manual: `gh repo view cli/cli` from a separate terminal works unchanged.
- [ ] `spec-copilot-fix.md` is marked complete or has a clear remaining-work tail.
- [ ] README documents `copilotproxy` next to `claudeproxy`.

---

## Team Review — 2026-04-25

### Depends on Slice 04 P0

The **conditional CONNECT MITM** fix (Slice 04, Task 11) is a hard prerequisite for this slice's "non-disruption" claim. Don't try to land Slice 05's non-disruption test before that fix is in.

### P0 — Domain scope correction

- **Verified:** Copilot's intercept list currently includes `api.github.com` (overbroad). This means *every* `gh` command — `gh repo view`, `gh pr list`, etc. — gets its body scanned and surrogate-replaced when run via `copilotproxy`. **Decision should be:** intercept only `api.githubcopilot.com` (and the actual chat/completion endpoint Copilot uses). Update `config/agentproxy.yaml` and document the choice in-line.

### Tasks added

10. **P0** Narrow `intercepted_domains` for Copilot to `api.githubcopilot.com` (+ Copilot-specific subdomains as discovered from real captures). Remove `api.github.com` from the default list. Add inline comment explaining why.
11. **P1** Resolve unresolved items from `spec-copilot-fix.md` (TLS handshake / cert pinning / ALPN issues flagged but not fully diagnosed). Audit whether Copilot uses ALPN-required HTTP/2 — if so, our `Transport()` must allow it.
12. **P1** No fixtures exist for Copilot today — capture must happen as the first task, otherwise test scaffolding has nothing to assert against.

### Notes

- `internal/provider/copilot.go` exists but inherits `DefaultProvider.ParseRequest` — same generic-walker situation as Claude. Probably fine for v1, but record the decision.
- `--no-proxy` flag still doesn't exist on `bin/copilotproxy`. Same uniform addition as Slice 04.
