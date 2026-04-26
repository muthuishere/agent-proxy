# Spec: Shape-Preserving Secret Surrogates

**Status:** Implemented
**Priority:** High
**Depends on:** `internal/vault`, `internal/runtime`, `internal/codec`

---

## Problem

AgentProxy currently replaces detected secrets with visibly masked placeholders such as `post************************prod[GENERIC_CONNECTION_STRING:...]`.

This protects the secret, but the final LLM provider can infer that a password or token was intentionally masked. That changes model behavior: it may write different commands, preserve the mask, refuse to use the value, or generate placeholder-aware output instead of behaving as if the input contained a normal credential.

---

## Goals

- Replace visibly masked placeholders with random-looking surrogate values that keep the original value's basic length and shape.
- Keep exact-token reversible behavior: if the exact surrogate value appears in a response, restore the original secret.
- Preserve the existing `Session.Mask(value, name) -> replacement` and `Session.Restore(text)` contract.
- Preserve existing normal text, JSON-aware, SSE, WebSocket, and encoded blob restore flows.
- Avoid leaking the real secret body in the surrogate, while allowing selected non-secret anchors such as URI schemes and provider key prefixes.

---

## Non-Goals

- No fuzzy restore.
- No restore if the LLM edits, rewrites, truncates, or semantically transforms the surrogate.
- No provider-specific prompt engineering.
- No external secret manager integration.
- No attempt to make surrogate credentials live-valid.

---

## Functional Requirements

### SURROGATE-1 — Replace visible masks with shape-preserving surrogates

`internal/vault.Session.Mask(value, name)` returns a deterministic random-looking surrogate rather than a star-based mask.

Examples of intended shape:

- API key patterns keep provider-specific prefix anchors where useful and randomize the remaining body.
- URI/connection-string patterns preserve the scheme and structural punctuation, then randomize the credential/body characters.
- Password/secret assignments preserve the assignment key/operator/quotes and randomize the value.
- Private keys preserve PEM boundary lines and randomize the body.
- Other patterns use a deterministic same-length, character-class-preserving surrogate.

The surrogate must be unique enough for session restore, must not use visible labels such as `dummy` or `mask`, and must not include star runs or `[PATTERN:digest]` markers.

### SURROGATE-2 — Exact restore only

The session vault stores `surrogate -> original`.

`Session.Restore(text)` performs exact string replacement only. If a model changes the surrogate, AgentProxy does not restore it.

### SURROGATE-3 — Existing transport styles keep working

The change is scoped to vault replacement generation. Existing callers continue to use the same interfaces:

- request body masking through `runtime.Service`
- JSON-aware fallback through `codec.WalkJSONStrings`
- encoded blob rewrite/restore through `codec.RewriteEncodedBlobs` and `codec.RestoreEncodedBlobs`
- SSE restore through `runtime.sseRestoreReader`
- WebSocket request masking and response restore

### SURROGATE-4 — Mapping stability and overwrite safety

If the same secret is masked more than once in a session, it should produce the same surrogate and restore to the same original.

If a generated surrogate is later seen by the masker, it must not overwrite an existing `surrogate -> original` mapping.

---

## Acceptance Criteria

- [x] Vault masking returns surrogates with no `*` mask run and no `[PATTERN:digest]` marker.
- [x] Same session restores exact surrogate text back to the original secret.
- [x] Mutated surrogate text does not restore.
- [x] Same secret masked twice in one session returns the same surrogate and restores correctly.
- [x] Connection strings are replaced with same-length, shape-preserving surrogates and restored exactly.
- [x] API keys and password assignments are replaced with same-length, shape-preserving surrogates and restored exactly.
- [x] JSON request bodies remain valid after surrogate replacement.
- [x] Encoded blobs containing secrets are rewritten with encoded surrogates and restored by the existing encoded restore path.
- [x] SSE and WebSocket response restore continue to work with surrogates.
- [x] Session isolation remains intact.

---

## Implementation Notes

- Primary file: `internal/vault/session.go`.
- Add focused tests in `internal/vault/session_test.go`.
- Add runtime integration tests in `internal/runtime/service_test.go` for JSON and encoded restore.
- Keep config shape unchanged for the first pass; existing `masking.*` options become legacy/no-op for surrogate generation until a future config migration.

---

## Verification

- `go test ./internal/vault ./internal/runtime ./internal/codec ./tests/go`
- `go test ./...`
