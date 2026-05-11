---
name: blueteam-mask
description: Defender / fix proposer for AgentProxy. Consumes the latest red-team findings JSON, looks up regex coverage in community catalogs (Gitleaks, TruffleHog, secretlint — see sources.md), proposes patches to config/patterns.yaml or config/pii_patterns.yaml with unit tests, and runs the test suite to prove the fix lands cleanly. Use when the user says "fix masking gaps", "apply blueteam fixes", "patch from findings", or after running redteam-mask.
---

# Blue-team mask fixer

You are the defender. Red-team has handed you a `findings-*.json` listing payloads that leaked. Your job is to close those gaps **using community-sourced regexes**, not LLM-invented ones.

## Inputs

- Latest `_redteam/findings-*.json` (most recent by mtime).
- `sources.md` (this skill folder) — list of community catalogs and how to fetch each one.
- `config/patterns.yaml`, `config/pii_patterns.yaml` — files you may patch.
- `internal/scanner/scanner_test.go` — pinning style for new patterns.

## Procedure

1. Load the findings file. Group by `expected_pattern` and by `category`.

2. For each unique gap:
   a. If `expected_pattern` is populated but the existing regex failed to match: classify as **anchor/boundary bug**. Open the existing rule in `config/patterns.yaml`, pull the current regex, and look up the same token type in `sources.md`. Pick the upstream regex that handles the boundary case. Cite the source URL in the YAML comment.
   b. If `expected_pattern` is empty (unknown vendor): look up the token shape in **Gitleaks** first (`config/gitleaks.toml`), then **TruffleHog** detectors (`pkg/detectors/<vendor>/`), then **secretlint** rules. Use the upstream regex verbatim or with a documented edit. Cite the source.
   c. If the finding is `category: encoded_blobs`: the fix is **not** a new regex — it's likely a `internal/codec/encoded.go` change. Flag this as a `codec-fix` finding and return it back to the user without patching YAML; codec changes need a spec update first per the working contract.
   d. If `category: pii_edge_cases` and `pii_patterns.yaml` is disabled by default: only patch the YAML and add a note that the user must opt in.

3. Before writing any patch, dry-run the proposed regex against:
   - The leaked payload from the finding (must match).
   - The whole `tests/go/fixtures/` corpus (must not produce new matches — verify with `go test ./internal/scanner/... -run TestScannerCoverage`).
   - Existing pinned tests in `internal/scanner/scanner_test.go`.

   Use `go run ./cmd/agentproxy validate -config ./config/agentproxy.yaml` to make sure the YAML is well-formed before continuing.

4. Apply the patch:
   - Edit `config/patterns.yaml` (or `pii_patterns.yaml`). Keep alphabetical-by-vendor where the file already is; otherwise append to the right `category` group.
   - Each new entry MUST include a YAML comment line above it: `# source: <URL> — <date fetched>`. This is non-negotiable; the user wants regex provenance.
   - Add a unit test in `internal/scanner/scanner_test.go` (or the nearest existing file) that pins the new pattern by matching the leaked payload and rejecting an obvious near-miss. Test name: `TestPattern_<NAME>_FromBlueteam_<YYYYMMDD>`.

5. Run:
   ```
   go test ./internal/scanner/... ./tests/go/...
   task test-acceptance
   ```
   Both must be green. If acceptance regresses, **revert the patch** and emit a `cannot-fix` entry to the findings rather than shipping a broken regex.

6. Emit a fix report at `_redteam/fixes-YYYY-MM-DD-HHMM.json`:

   ```json
   {
     "for_findings_file": "_redteam/findings-2026-05-11-1430.json",
     "fixes_applied": [
       {
         "finding_id": "boundary_chars#1",
         "rule_name": "OPENAI_API_KEY",
         "patch_kind": "regex_update",
         "old_pattern": "sk-(proj-)?[A-Za-z0-9\\-_]{20,}",
         "new_pattern": "sk-(?:proj-|svcacct-)?[A-Za-z0-9_\\-]{20,}",
         "source_url": "https://github.com/gitleaks/gitleaks/blob/master/config/gitleaks.toml#L###",
         "test_added": "internal/scanner/scanner_test.go:TestPattern_OPENAI_API_KEY_FromBlueteam_20260511"
       }
     ],
     "deferred": [
       {
         "finding_id": "encoded_blobs#2",
         "reason": "codec-fix needed — not a regex gap",
         "next_step": "open docs/specs/spec-codec-double-base64.md"
       }
     ],
     "test_results": {
       "scanner": "pass",
       "acceptance": "pass"
     }
   }
   ```

## Auto-grade handoff

When done, the user (or the orchestrator) re-runs `redteam-mask`. The score from that run measures your fix quality:

- `closed = leaked_before - leaked_after` should equal the count in `fixes_applied`.
- If `closed < fixes_applied`, your regex was too narrow.
- If acceptance/scanner tests started failing, that's a regression — your fix matched legit traffic.

## Hard constraints

- **No LLM-invented regexes.** Every new pattern must have a `source: <URL>` comment pointing at a community catalog. If no upstream covers it, return the finding as `deferred` with `reason: no-community-source`.
- **Never touch `internal/codec/` or `internal/scanner/scanner.go`** from this skill. Patterns are data; engine changes need a spec.
- **Never enable `pii_patterns.yaml` by default** — it's opt-in. Only add patterns inside it.
- **Never commit secrets** found in leaked payloads to the repo. The findings file is gitignored (or `_redteam/` is — verify, and add it to `.gitignore` if missing).

## When this skill is **not** the right tool

- The user wants a brand-new pattern category that no community source covers → use `bmad-create-architecture` for a spec, then implement.
- A finding is about SSE delta restore correctness → that's `internal/runtime/sse.go`, not a regex. Out of scope.
- The user wants to *remove* a pattern → ask for confirmation; this skill only adds and tightens.
