# Documentation Index

This repository uses a spec-first workflow. If you are changing behavior, start in `docs/specs/`, not in the code.

## Start Here

- [README.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/README.md)
  User-facing overview, install flow, wrappers, config, and quick start.
- [CLAUDE.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/CLAUDE.md)
  Contributor-oriented build, test, and architecture notes.
- [AGENTS.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/AGENTS.md)
  Repo working contract: codebase map, doc map, and spec-first workflow.

## Specs

`docs/specs/` is for feature behavior, workflow changes, UX changes, parser/masking changes, and implementation plans.

Use this area for:

- new features
- behavior changes
- config schema changes
- install and wrapper UX changes
- provider/parser work
- masking, restoration, and pattern coverage changes

Useful entries:

- [agentproxy-spec-v03.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/specs/agentproxy-spec-v03.md)
- [spec-install-ux-and-liveness.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/specs/spec-install-ux-and-liveness.md)
- [spec-pattern-matchers-and-default-secret-coverage.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/specs/spec-pattern-matchers-and-default-secret-coverage.md)
- [spec-json-aware-masking.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/specs/spec-json-aware-masking.md)
- [spec-template.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/specs/spec-template.md)

## Decisions and Spikes

`docs/decisions/` and focused spike docs are for narrower technical decisions and investigation outcomes.

Use this area for:

- ADR-like decision capture
- spike findings
- tradeoff records
- implementation constraints discovered during migration

Examples:

- [spike-1-sse-buffering.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/decisions/spike-1-sse-buffering.md)
- [spike-2-encoded-blob-restoration.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/decisions/spike-2-encoded-blob-restoration.md)
- [spike-3-vault-lifetime.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/decisions/spike-3-vault-lifetime.md)
- [spike-4-proxy-library-websocket.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/decisions/spike-4-proxy-library-websocket.md)

## Migration and Project State

These docs explain where the codebase is in the Python-to-Go migration and what remains.

- [go-migration-status.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/go-migration-status.md)
- [migration-go-task-plan.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/migration-go-task-plan.md)
- [migration-go-tracker.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/migration-go-tracker.md)

## Testing and Validation

- [testing-scenarios.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/testing-scenarios.md)

Use tests in:

- `internal/*/*_test.go` for unit/subsystem coverage
- `tests/go/` for acceptance-style masking coverage
- `tests/*.sh` for shell-driven end-to-end checks

## Marketing and External Explanation

Use `docs/marketing/` for external-facing explanation, not implementation planning.

- [what-is-agentproxy.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/marketing/what-is-agentproxy.md)
- [technical-overview.md](/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/docs/marketing/technical-overview.md)

## Where To Put New Docs

- behavior intent before implementation: `docs/specs/`
- narrow architecture decision or spike: `docs/decisions/`
- long-lived contributor guidance: repo root docs or `docs/index.md`
- user-facing usage changes: `README.md`
- build/test/architecture contributor notes: `CLAUDE.md`
- external product messaging: `docs/marketing/`
