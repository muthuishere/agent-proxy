# Go Spike Workspace

This directory is reserved for spike-only code during the Python to Go migration.

Rules:

- Put research prototypes here, not production implementation.
- Keep each spike isolated so failed experiments do not shape the final module layout.
- Decision documents live in `docs/decisions/`.
- Production Go code begins in `cmd/` and `internal/` only after the spike gate closes.

Planned layout:

- `go-spikes/sse/`
- `go-spikes/encodedblob/`
- `go-spikes/vaultscope/`
- `go-spikes/ws/`
