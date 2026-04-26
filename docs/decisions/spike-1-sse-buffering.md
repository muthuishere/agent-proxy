# SPIKE-1 Decision: SSE / Chunked Streaming

## Status

- Owner: Eng 3
- Status: **Decided**
- Blocks: Track A, Track F

## Question

Which buffering strategy catches cross-chunk secrets and preserves SSE structure while staying below the latency budget?

## Options Under Test

- Option A: per-event buffer
- Option B: full-response buffer
- Option C: sliding window

## Required Inputs

- Split-secret SSE fixtures from `docs/testing-scenarios.md`
- Latency measurements per chunk
- Validation for `event:`, `data:`, and `id:` preservation

## Decision

- Chosen option: **per-event buffer with semantic normalization**
- Why: raw per-event, full-buffer, and sliding-window scans all miss a secret split across
  separate `data:` lines inside one logical SSE event. Semantic normalization — joining all
  `data:` line payloads within one event into a single string before scanning — closes that
  gap. The per-event strategy is fastest in benchmarks and correctly catches both chunk-split
  and data-line-split secrets once normalization is applied.

## Evidence

- Validation command: `go test ./go-spikes/sse -v`
- Benchmark command: `go test ./go-spikes/sse -bench=. -run '^$'`
- Test artifact: `go-spikes/sse/`
- New tests added on 2026-03-27:
  - `TestSemanticNormalizationCatchesDataBoundarySplit`: confirms `DetectWithSemanticNormalization`
    finds a secret split across two consecutive `data:` lines — all three raw strategies missed this.
  - `TestSemanticNormalizationIgnoresNonDataLines`: confirms non-data SSE fields (`id:`, `event:`)
    are ignored correctly and event count is preserved.
- Notes:
  - Chunk-split (secret split across network chunks within one `data:` line) is handled by the
    per-event buffer accumulating chunks until `\n\n`.
  - Data-line-split (secret split across two `data:` lines in the same event) requires
    `NormalizeEventData`, which concatenates the payloads without a separator so a secret
    spanning lines is visible as a contiguous substring.
  - Joining without separator is intentional for pattern matching; this differs from the SSE spec
    (which joins with `\n`) but is the correct behaviour for secret detection.
  - Local benchmark on 2026-03-27:
    - per-event buffer: `817.8 ns/op`
    - full buffer: `964.9 ns/op`
    - sliding window: `1524 ns/op`
  - Live network streaming and gzip/chunked body behavior are follow-up items for Track F.

## Follow-up Contract

- Production interface: `DetectWithSemanticNormalization` (or equivalent) should be the
  canonical SSE scan path in `internal/proxy` for response bodies with `Content-Type: text/event-stream`.
- Remaining risks:
  - Full buffering of SSE before forwarding adds latency; Track F must decide whether to stream
    events as they complete or hold the entire response.
  - gzip/chunked transfer-encoding interaction with SSE parsing is not yet validated.
