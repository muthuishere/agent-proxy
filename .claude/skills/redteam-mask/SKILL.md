---
name: redteam-mask
description: Adversarial tester for AgentProxy's regex-based masking. Crafts payloads designed to slip past current patterns in config/patterns.yaml and config/pii_patterns.yaml, drives them through the running proxy, then reads /api/traffic to score which leaked. Outputs findings JSON for the blueteam-mask skill to consume. Use when the user says "redteam patterns", "find masking gaps", "break the proxy", "probe coverage", or before any launch / release cut.
---

# Red-team mask probe

You are the adversary. Your job is to find inputs that contain a real secret or PII value yet pass through AgentProxy unmasked. **Do not invent regexes** — you probe with concrete payload strings.

## Inputs

- `config/patterns.yaml` — current secret patterns
- `config/pii_patterns.yaml` — current PII patterns (may be disabled)
- `payloads.md` (this skill folder) — seed adversarial corpus, grouped by category
- The running proxy on `127.0.0.1:7717` and the dashboard on `127.0.0.1:7718`

## Pre-flight

1. `./agentproxy status` — must report running. If not, start it: `./agentproxy start -config ./config/agentproxy.yaml &` and wait ~3s.
2. Confirm CA is trusted (otherwise `curl` will reject MITM): `curl -sS --cacert ~/.agentproxy/certs/agentproxy-ca.pem -x http://127.0.0.1:7717 https://api.anthropic.com/v1/messages -o /dev/null -w '%{http_code}\n'` — expect a 4xx, not a TLS error.

## Procedure

For each category in `payloads.md`:

1. Pick the payload corpus for that category (e.g. `boundary_chars`, `encoded_blobs`, `sse_split`, `unknown_vendor_keys`, `pii_edge_cases`).
2. For each payload P:
   - Send a request to an intercepted host carrying P in a place a real agent would put it (request body JSON, header, query string — see `payloads.md` for the right vector per category).
   - Capture the `requestID` from `logs/traffic.jsonl` (`tail -n 1 logs/traffic.jsonl | jq -r .id`) — or list via `curl -sS http://127.0.0.1:7718/api/traffic | jq -r '.[0].id'`.
   - Fetch detail: `curl -sS http://127.0.0.1:7718/api/traffic/$ID | jq '.replacements'`.
   - Verdict:
     - **MASKED** — replacements contain a surrogate for the original P.
     - **LEAKED** — no replacement; the outbound body still contains P verbatim (re-check from the on-disk `traffic.jsonl` request body).
3. Record each LEAKED case to the findings file.

## Output

Write findings to `_redteam/findings-YYYY-MM-DD-HHMM.json` (create `_redteam/` if missing; it's already covered by `.gitignore` patterns like `_bmad-output/`-style local dirs — verify and add if not).

Schema:

```json
{
  "run_id": "2026-05-11-1430",
  "proxy_version": "<output of ./agentproxy version>",
  "patterns_sha": "<sha1 of config/patterns.yaml>",
  "findings": [
    {
      "category": "boundary_chars",
      "payload": "the-actual-input-string",
      "vector": "body.messages[0].content",
      "expected_pattern": "OPENAI_API_KEY",
      "actual": "leaked",
      "evidence": {
        "request_id": "1-7",
        "traffic_url": "http://127.0.0.1:7718/api/traffic/1-7"
      },
      "notes": "trailing zero-width space prevented [A-Za-z0-9_-]{20,} from matching"
    }
  ]
}
```

## Stop conditions

- Cap each category to **10 payloads** per run to keep the loop cheap.
- Cap total findings to 30 per run — beyond that, blueteam can't triage in one pass.
- If 0 findings after exhausting `payloads.md`, the run is **clean** — write an empty findings file with `"status": "clean"`.

## Auto-grade loop

After blueteam-mask lands fixes, re-run this skill with the same payload set and compare:

- `closed = leaked_before - leaked_after`
- `regressions = newly_masked_inputs_that_now_break_legit_calls` (run `task test` and `task test-acceptance` — anything failing post-fix counts as a regression)
- Score: `closed / leaked_before`, gated by `regressions == 0`

Print the score back to the user.

## Constraints

- **Never** invent or guess new regex patterns here. That's blueteam's job, and the regexes come from community sources.
- **Never** send payloads with the user's real personal data. Use synthetic values that match shape but are clearly fake (e.g. `4111-1111-1111-1111` for credit cards, `john.doe@example.com` for emails).
- **Never** modify `config/patterns.yaml` from this skill. Findings only.
