# Spike 4 Sandbox

Purpose: compare `goproxy` against a custom CONNECT/WebSocket path.

Current contents:

- TLS echo server
- client/frame verification harness
- `goproxy` pass-through probe
- custom frame mutation probe

Current status:

- `go test ./go-spikes/ws -v` passes
- pass-through through `goproxy` is working
- custom frame parsing and mutation is working in isolation
- remaining open question is whether `goproxy` can provide a reliable post-upgrade frame mutation hook under MITM
