# Spike: Vendored `goproxywss`

## Goal

Use our forked `goproxy` source from inside this repository instead of pulling it as an external dependency, while preserving upstream license obligations.

## Approach

- Copy the fork into `third_party/goproxywss`
- Keep the upstream module path unchanged inside the vendored copy
- Point the main module at the local copy with:

```go
replace github.com/elazarl/goproxy => ./third_party/goproxywss
```

## Why this shape

- Existing imports stay unchanged.
- We can patch the vendored code directly.
- The migration spike uses a stable local snapshot instead of a moving branch.

## License handling

- Preserve the original BSD 3-Clause `LICENSE` inside `third_party/goproxywss`
- Record provenance and usage in `THIRD_PARTY_NOTICES.md`
- Do not relabel upstream code as MIT or proprietary

## Validation

Expected validation commands:

```bash
go list -m -json github.com/elazarl/goproxy
go test ./go-spikes/ws -v
go test ./...
```
