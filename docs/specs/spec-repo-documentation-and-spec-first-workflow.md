# Spec: Repo Documentation Structure + Spec-First Workflow

**Status:** Proposed
**Priority:** High
**Depends on:** Current Go codebase structure under `cmd/`, `internal/`, `config/`, `tests/`, and existing docs in `docs/`

---

## Problem

The repo has useful documentation, but it lacks a single repo-local contract that tells future agents and contributors:

1. how the codebase is organized
2. where durable documentation belongs
3. when a change needs a spec before implementation
4. how specs, decisions, tests, and user-facing docs should stay aligned

Today that behavior is implied by conversation, not enforced by repository artifacts.

---

## Goals

- Add a repo-local `AGENTS.md` that defines how work should be done in this repository.
- Make spec-first delivery the default workflow for behavior changes, features, UX changes, protocol changes, and detection-rule changes.
- Add a durable docs index so a new contributor can understand where to read and where to write.
- Add a lightweight spec template so new work follows a consistent format.

---

## Non-Goals

- Rewriting every existing document to a new format
- Creating a heavy PRD or ADR process for every minor edit
- Blocking typo-only or comment-only changes on long documentation ceremony

---

## Functional Requirements

### DOCFLOW-1 — Repo-local `AGENTS.md`

Add `AGENTS.md` at the repo root with:

- a short repo summary
- the current top-level codebase map
- the documentation map
- a required workflow for spec-first changes
- clear guidance on when to create a new spec vs update an existing one
- a short verification checklist for implementation follow-through

The file should be written for coding agents and human contributors working in this repository.

### DOCFLOW-2 — Spec-first workflow

`AGENTS.md` must define this default flow:

1. understand the current code and existing docs
2. create or update a spec in `docs/specs/`
3. implement only after the spec exists
4. update user-facing docs and tests to match the shipped behavior

This applies to:

- CLI behavior changes
- install/uninstall UX
- masking or restore behavior
- provider parsing behavior
- config schema changes
- wrapper script behavior
- dashboard behavior
- default pattern changes

For tiny changes, the workflow may use a short scoped spec or extend an existing relevant spec instead of creating a brand new document.

### DOCFLOW-3 — Documentation index

Add a durable docs hub in `docs/index.md` that explains:

- what each docs area is for
- which docs describe shipped behavior vs historical decisions vs work-in-progress
- where contributors should add new docs

The index should point readers to:

- `README.md`
- `CLAUDE.md`
- `docs/specs/`
- `docs/decisions/`
- migration docs
- testing docs
- marketing docs

### DOCFLOW-4 — Spec template

Add a reusable spec template under `docs/specs/` for future work.

The template should include:

- title
- status
- problem
- goals
- non-goals
- functional requirements
- acceptance criteria
- implementation notes
- verification

The template should be minimal and consistent with the existing spec style already used in this repository.

---

## Behavioral Rules

### Rule 1 — Specs are the source of intent

For non-trivial behavior changes, the spec is the canonical statement of intended behavior before code is changed.

### Rule 2 — Existing specs should be reused when appropriate

If a request extends an existing area already covered by a current spec, update that spec instead of fragmenting the docs with duplicate documents.

### Rule 3 — Shipped behavior must be reflected in durable docs

When implementation changes user-visible behavior, contributors should update the appropriate durable docs, usually one or more of:

- `README.md`
- `CLAUDE.md`
- `docs/index.md`
- testing docs
- changelog

### Rule 4 — Decision docs remain separate from feature specs

`docs/decisions/` should remain for focused architecture/implementation decisions and spike outcomes, not for full feature behavior specs.

---

## Acceptance Criteria

- [ ] Repo root contains `AGENTS.md`
- [ ] `AGENTS.md` includes a codebase map and documentation map matching the current Go codebase
- [ ] `AGENTS.md` explicitly states the spec-first workflow
- [ ] `docs/index.md` exists and organizes the documentation surface
- [ ] `docs/specs/spec-template.md` exists and is usable for future work
- [ ] The new docs align with current repo structure and do not describe removed Python components as active architecture

---

## Implementation Notes

- Keep the repo map focused on the current Go codebase:
  - `cmd/agentproxy`
  - `internal/runtime`
  - `internal/proxy`
  - `internal/provider`
  - `internal/scanner`
  - `internal/patterns`
  - `internal/vault`
  - `internal/codec`
  - `internal/ui`
- Acknowledge that the repo is mid-migration from Python to Go, but document the Go code as the active implementation.
- Keep the template and index short enough that contributors will actually use them.

---

## Verification

- Read the new docs end-to-end and confirm they match the live repo layout.
- Verify all referenced files/directories exist.
- Ensure the new docs do not conflict with `README.md` or `CLAUDE.md`.
