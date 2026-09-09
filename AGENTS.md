# AGENTS.md

This repository is run as a spec-first codebase. Understand the current implementation, capture intended behavior in a spec, then implement.

## Repo Summary

AgentProxy is a local MITM proxy for AI coding agents. It intercepts outbound traffic, masks secrets before requests leave the machine, and restores them in responses so agent behavior remains intact.

The active implementation is Go. Legacy Python files may still appear in history or migration docs, but the Go code under `cmd/` and `internal/` is the source of truth.

## Codebase Map

- `cmd/agentproxy/`
  Main CLI entry point. Commands include `start`, `validate`, `config`, `status`, `ca-setup`, and `version`.
- `cmd/wsdebug/`
  Small debug helper for WebSocket experimentation.
- `internal/runtime/`
  End-to-end masking and restoration pipeline for HTTP, SSE, and WebSocket flows.
- `internal/proxy/`
  goproxy-based MITM server wiring request/response/session hooks.
- `internal/provider/`
  Provider-specific request/response parsers for Anthropic, OpenAI/Codex, Copilot, and default passthrough behavior.
- `internal/scanner/`
  Parallel matcher execution for `regex`, `prefix`, `contains`, and `contains_between`.
- `internal/patterns/`
  Pattern file loading and validation for default secret and PII rules.
- `internal/codec/`
  Body decoding/encoding, JSON string walking, and encoded-blob masking/restore.
- `internal/vault/`
  Session-scoped reversible placeholder mapping.
- `internal/logger/`
  Traffic logging and dashboard event fanout.
- `internal/ui/`
  Embedded dashboard server and HTML asset.
- `config/`
  Runtime config, secret patterns, PII patterns, filename/domain support files.
- `tests/go/`
  Acceptance-style Go tests for masking behavior.
- `tests/*.sh`
  Shell-based end-to-end test helpers.
- `docs/`
  Product specs, migration notes, decisions, testing guidance, and marketing docs.

## Documentation Map

- `README.md`
  User-facing overview, install flow, CLI entrypoints, and configuration basics.
- `CLAUDE.md`
  Repo working notes for coding agents and contributors. Useful for build/test/architecture orientation.
- `docs/index.md`
  Documentation hub. Start there if you need to know where a doc belongs.
- `docs/specs/`
  Behavior specs and implementation plans. New feature/change work starts here.
- `docs/decisions/`
  Narrow architectural decisions, spikes, and tradeoff records.
- `docs/testing-scenarios.md`
  Manual and scenario-based validation reference.
- `docs/go-migration-status.md`
  Current migration state from the older Python implementation to Go.
- `docs/marketing/`
  External-facing product explanation material.

## Required Workflow

For any non-trivial change, follow this sequence:

1. Read the relevant code and existing docs first.
2. Create a new spec in `docs/specs/` or update the existing spec that already owns that behavior.
3. Implement only after the spec exists.
4. Update durable docs if shipped behavior changed.
5. Verify the implementation with the most relevant tests or validation commands available.

Default to a spec for:

- feature work
- CLI behavior changes
- install/uninstall UX changes
- provider/parser changes
- masking and restoration behavior
- pattern/schema changes
- dashboard behavior
- test strategy changes

For very small changes, keep the ceremony small:

- typo-only changes may skip a new spec
- tightly scoped bug fixes should still update an existing spec or add a short new one

## Spec Rules

- Prefer updating an existing spec when the behavior already has a home in `docs/specs/`.
- Create a new spec when the change introduces a distinct behavior, workflow, or subsystem concern.
- Keep specs concrete. Describe user-visible behavior, config shape, edge cases, and acceptance criteria.
- Specs should reflect intended behavior before implementation, not just summarize the code after the fact.

Use `docs/specs/spec-template.md` when creating a new spec.

## Implementation Follow-Through

After code changes, check whether these also need updates:

- `README.md`
- `CLAUDE.md`
- `docs/index.md`
- `docs/testing-scenarios.md`
- `CHANGELOG.md`

If user-visible behavior changed and the docs were not updated, the work is incomplete.

## Verification Expectations

Before closing work:

- run the narrowest useful tests first
- run broader validation if the change crosses subsystem boundaries
- call out anything you could not verify

Avoid claiming behavior that is not reflected in code, tests, or current docs.

<!-- ctx-optimize:begin -->
<ctx-optimize>
  <precondition>Run `command -v ctx-optimize` first. If it is NOT installed, IGNORE this entire
  block and answer by reading the code normally — the store is an optimization, not a requirement
  (install later with `npm install -g @muthuishere/ctx-optimize`, or download the binary). Everything
  below applies ONLY when the command exists.</precondition>
  <store>Pre-built knowledge store at `~/ctxoptimize/agent-proxy/` (config in `.ctxoptimize/` here).</store>
  <use>Use it INSTEAD of grep-and-read chains — PICK BY INTENT: find → `ctx-optimize query "<terms>"` ·
  inspect a symbol → `card <symbol>` · about to EDIT → `change-plan <symbol>` (callers+impact+tests, one
  call) · blast radius → `affected <symbol>` · connection → `path <a> <b>` ·
  list/filter (no jq): `nodes --kind K` / `edges --relation R` / `deps`.
  Output is parsed fact with exact file:line — cite it directly, do
  NOT re-verify in source; open a file only for a body the store didn't show. Exhaustive literal-string
  sweeps stay grep's job.</use>
  <deep-doc>The FULL usage card — verify discipline, store-vs-grep ladder, sources (databases/
  buckets/queues/APIs by env-var name), remote push/pull, `up` — is committed at
  `.ctxoptimize/instructions.md`. Read it before deeper store work.</deep-doc>
  <no-local-store>Fresh clone with nothing at `~/ctxoptimize/agent-proxy/`? Run `ctx-optimize up` —
  it pulls the team's prebuilt store when the config declares one, otherwise rebuilds in seconds.</no-local-store>
</ctx-optimize>
<!-- ctx-optimize:end -->
