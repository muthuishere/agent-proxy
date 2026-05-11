# Seed adversarial payload corpus

Synthetic inputs only. Every value here is fake but shape-correct. Group by attack category. The skill iterates these and may extend them per run, but core payloads should remain stable so before/after scoring is meaningful.

## 1. boundary_chars — characters that defeat anchored / greedy regex

| Payload | Vector | Expected pattern |
|---|---|---|
| `sk-ant-api03-AAAA1111BBBB2222CCCC3333DDDD4444​` (trailing zero-width space) | body.messages[0].content | ANTHROPIC_API_KEY |
| `sk-proj-AAAA1111BBBB2222CCCC3333DDDD4444\nplease keep it secret` | body.messages[0].content | OPENAI_API_KEY |
| `  ghp_AAAAA11111BBBBB22222CCCCC33333DDDDD44444  ` (surrounded by 2 spaces — should still match) | header X-Custom | GITHUB_TOKEN |
| `prefix-sk-proj-AAAA1111BBBB2222CCCC3333DDDD4444-suffix` (embedded between dashes) | body.metadata.note | OPENAI_API_KEY |

## 2. encoded_blobs — secret hidden inside base64 / hex / URL-encoded blob

| Payload | Vector | Expected |
|---|---|---|
| base64 of `Authorization: Bearer sk-ant-api03-AAAA1111BBBB2222CCCC3333DDDD4444` | body.attachments[0].data | ANTHROPIC_API_KEY (via codec.RestoreEncodedBlobs path) |
| hex of `sk-proj-AAAA1111BBBB2222CCCC3333DDDD4444` | body.tool_input.payload | OPENAI_API_KEY |
| URL-encoded `sk-ant-api03-AAAA1111BBBB2222CCCC3333DDDD4444` (`%2D` for `-`) | query string | ANTHROPIC_API_KEY |
| double-base64 (b64 of b64 of secret) | body.attachments[0].data | leak expected — pin as known-gap |

## 3. sse_split — secret split across SSE deltas

Only meaningful against a fake upstream you control, but the proxy still applies restore on the response stream. Drive via `tests/go/openai_key_roundtrip_test.go` style: craft an SSE response that puts surrogate fragments across `content_block_delta` events and verify restore stitches them. The red-team check here is *forward direction*: a secret on the request side that spans a JSON string concatenation operator. Few real agents do this — low priority.

## 4. unknown_vendor_keys — patterns we don't have rules for yet

Pull from community catalogs (see blueteam-mask/sources.md) — do **not** invent. Examples that have appeared in Gitleaks and TruffleHog catalogs but may be missing here:

- `dop_v1_<64-hex>` (DigitalOcean Personal Access Token)
- `pul-<40-hex>` (Pulumi token)
- `dt0c01.<24>.<64>` (Dynatrace token)
- `EAAA<...>` (Square access token)
- `xkeysib-<64-hex>-<16-alnum>` (Sendinblue / Brevo)

For each, the redteam sends a synthetic but well-formed token through the proxy and checks whether *any* pattern matched. Zero matches → finding.

## 5. pii_edge_cases — when pii_patterns.yaml is enabled

| Payload | Why hard |
|---|---|
| `john.doe+filter@sub.example.co.in` | plus-tag + multi-level TLD |
| `+91 98765 43210` then `+91-98765-43210` then `9876543210` | format drift |
| `4111 1111 1111 1111` (Visa test) | spaces in card number |
| `4111-1111-1111-1111` | dashes in card number |
| `2401:4900:1c40:b67e:a4d2:1234:5678:9abc` (IPv6) | IPv6 patterns often missing |

## 6. json_key_vs_value — secret placed in JSON *key* not value

Some scanners only walk string values. Drop `sk-proj-AAAA1111BBBB2222CCCC3333DDDD4444` as the key of a map. Expected: still masked. If not, that's a `codec.json.go` walker bug.

## 7. duplicate_surrogate_collision — same value reused across keys

Not a leak per se, but a correctness probe: when the same `sk-ant-...` appears twice in one body, both must mask to the **same** surrogate so the upstream model sees a consistent token. Pinned by vault tests, but worth re-asserting end-to-end.

## Adding new categories

When you discover a new attack class:

1. Append it here with at least 3 concrete synthetic payloads.
2. Bump the version comment at top of the file.
3. Re-run the red-team probe so the baseline grows.

Community-sourced regexes for the *blueteam* side go in `../blueteam-mask/sources.md` — never inline them here.
