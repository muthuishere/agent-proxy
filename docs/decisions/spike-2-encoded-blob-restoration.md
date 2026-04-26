# SPIKE-2 Decision: Encoded Blob Restoration

## Status

- Owner: Eng 4
- Status: **Decided**
- Blocks: Track B, decoder design

## Question

How do we mask encoded secrets while preserving reversibility and valid JSON structure?

## Options Under Test

- Option B: re-encode blob with vault token inside
- Option C: replace full encoded blob with vault token

## Required Inputs

- Base64, hex, URL-encoded, nested, and JSON-preservation fixtures from `docs/testing-scenarios.md`
- Restore behavior validation for each fixture

## Decision

- Chosen option: **Option B — re-encode blob with vault tokens embedded**
  (implemented as `EmbeddedTokens` strategy in `internal/codec/encoded.go`)
- Why: Option B preserves the outer encoded shape and non-secret context, making it easier to
  debug and audit masked payloads. Restoration is handled by `RestoreEncodedBlobs` which
  recursively decodes, restores vault tokens, and re-encodes — the same depth-limited pass
  used for masking. Option C (replace entire blob with a single vault token) is simpler but
  loses all non-secret context inside the blob, which makes verification harder.

## Evidence

- Prototype path: `go-spikes/encodedblob/`
- Validation command: `go test ./go-spikes/encodedblob -v`
- Production implementation: `internal/codec/encoded.go` (`RewriteEncodedBlobs` /
  `RestoreEncodedBlobs` with `EmbeddedTokens` strategy)
- Test results confirmed:
  - base64 connection string
  - base64 dotenv block with multiple secrets
  - hex secret
  - URL-encoded secret
  - nested encoded content to depth 4
  - JSON structure preservation
- Notes:
  - Both strategies round-trip correctly in isolated tests.
  - `EmbeddedTokens` is the active production path; `WholeBlobTokens` remains available in the
    codec if a future use case requires it.
  - Recursive decode/restore/re-encode is bounded by `MaxDepth` (default 4) to prevent unbounded recursion.

## Follow-up Contract

- Decoder interface: `RewriteEncodedBlobs(text, scanner, replacer, EncodedBlobOptions{Strategy: EmbeddedTokens})`
  and `RestoreEncodedBlobs(text, restorer, EncodedBlobOptions{})` are the canonical interface.
- Vault interface: `Session.Mask` / `Session.Restore` work unchanged for embedded tokens.
- Remaining risk: partial-blob masking (a blob where only some bytes contain secrets) is
  handled correctly by `EmbeddedTokens` but has not been validated with very large mixed
  payloads; add a large-blob golden test before Track B is considered complete.
