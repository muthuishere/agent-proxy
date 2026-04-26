# SPIKE-4 Decision: Proxy Library and WebSocket Handling

## Status

- Owner: Eng 1
- Status: **Decided**
- Blocks: Track A, Track F websocket path

## Question

Can `goproxy` handle MITM plus WebSocket upgrade and frame interception, or do we need a custom CONNECT/TLS/WebSocket path?

## Options Under Test

- `elazarl/goproxy`
- Custom `net/http` + `net.Conn` hijack

## Required Inputs

- Local TLS WebSocket echo server
- Codex-shaped frame fixtures
- Upgrade success, frame mutation, and connection survival checks

## Current Prototype

- Sandbox path: `go-spikes/ws/`
- Current test:
  - `go test ./go-spikes/ws -v`
  - `go test ./third_party/goproxywss -run WebSocket -bench BenchmarkCopyWebSocketFrameOnce -benchmem`
  - `go test ./go-spikes/ws -run TestGoproxyMITMHookPersistentSessionSoak -v`
  - `go test ./go-spikes/ws -run TestGoproxyMITMHookSoakMatrix -v`
  - `go test ./go-spikes/ws -run TestGoproxyMITMHookResourceProfile -v`
  - `AGENTPROXY_LONG_SOAK=1 AGENTPROXY_PPROF_DIR=... go test ./go-spikes/ws -run TestGoproxyMITMHookLongSoakProfile -v`
- What it proves so far:
  - a local `wss://` echo server can be reached through a Go HTTP proxy
  - WebSocket upgrade survives CONNECT proxying
  - Codex-shaped text frames survive round-trip transport
  - a custom framed probe can read a client frame, mutate a connection-string payload, and forward the mutated frame upstream
  - vendored `goproxywss` can now mutate text frames under MITM via an explicit hook
- What it does not prove yet:
  - MITM certificate handling on the upgraded path
  - frame mutation hooks inside `goproxy` itself
  - whether `goproxy` is production-viable for WebSocket masking after upgrade rather than simple pass-through

## Library Landscape

### `elazarl/goproxy`

- Strong fit for HTTP/HTTPS proxying and MITM.
- Active enough to have recent WebSocket work in releases.
- Current repo explicitly includes `websocket.go` and `websocket_test.go`.
- Release notes mention WebSocket handling refactoring around the standard library.
- Important implementation detail:
  - upstream `proxyWebsocket` was raw bidirectional copy after upgrade
  - in our vendored spike we added an explicit `WebSocketMessageHandler` hook to intercept framed traffic
- Risk:
  - hook path must stay optional so raw copy remains available for maximum throughput
  - historical vulnerability exists for invalid requests causing panic in MITM mode, so version discipline matters

### `AdguardTeam/gomitmproxy`

- Attractive for plain HTTPS MITM and request/response handlers.
- README positions it as a customizable MITM proxy.
- Not a strong fit for this project's WebSocket masking requirement.
- Risk:
  - README TODO still lists WebSocket support
  - latest public release shown is old, which is a bad sign for this specific transport problem
  - GPL-3.0 license is a worse fit than BSD/MIT if we want broad reuse flexibility

### `google/martian`

- Mature HTTP/S proxy library.
- Good reputation for request/response modification.
- Not a strong candidate for this spike.
- Risk:
  - no obvious WebSocket support surfaced in the current repo documentation
  - looks more like an HTTP modifier framework than a WebSocket-aware MITM base

### Custom CONNECT/TLS path + dedicated WebSocket library

- Strongest control surface for Codex-style frame inspection and mutation.
- Our local custom spike already proves frame-level read/mutate/forward feasibility.
- Best pairing if we go custom:
  - keep proxy/tunnel logic in stdlib
  - use a focused WebSocket library only for frame handling if needed
  - likely candidates: `github.com/coder/websocket` for a maintained high-level API or `github.com/gobwas/ws` for lower-level frame control
- Risk:
  - highest implementation cost
  - we own more of the handshake, tunnel, and backpressure behavior

## Early Findings

- Pass-through feasibility is good: the upgrade path itself is not the hard part.
- A custom connection-level frame parser is also feasible in isolation.
- The unresolved question is interception after upgrade inside `goproxy`, not whether WebSocket transport itself works in Go.
- If `goproxy` does not expose a reliable frame-level mutation point under MITM, a custom CONNECT/TLS/WebSocket path is still likely.
- Local result on 2026-03-27:
  - `go test ./go-spikes/ws -v`
  - passed
  - confirms `wss://` upgrade through `goproxy` CONNECT pass-through and round-trip of Codex-shaped text frames
  - confirms `goproxy` MITM mode also supports WebSocket round-trip in this repo
  - confirms the custom probe can inspect and mutate a WebSocket text frame payload before upstream delivery
  - confirms the vendored `goproxywss` hook can mutate a WebSocket text frame payload under MITM before upstream delivery
- External research result on 2026-03-27:
  - `goproxy` remains the best off-the-shelf starting point for HTTP/HTTPS MITM
  - `gomitmproxy` is not attractive for this use case because WebSocket support is still listed as TODO
  - `martian` does not present itself as a WebSocket-oriented solution
  - inference: if `goproxy` cannot give us a clean post-upgrade frame interception point, the fallback should be custom tunnel code, not a lateral move to another proxy library

## Decision

- Chosen option: **vendored `goproxywss`** (finalized)
  (wired in `internal/proxy/server.go` via `WebSocketMessagePredicate` + `WebSocketMessageHandler`)
- Why:
  - no clearly better Go proxy library surfaced for the combination of MITM plus programmable
    WebSocket frame interception
  - the vendored hook gives the required control surface without abandoning a mature proxy base
  - optional framed interception is the right performance shape: unhooked traffic still uses raw
    copy; the benchmark overhead of the hook path is small (~5-6% in ns/op)
  - the custom transport spike is retained in `go-spikes/ws/` as a documented fallback but is
    no longer the preferred path

## Evidence

- Prototype path: `go-spikes/ws/`
- Test results:
  - pass-through probe passes
  - MITM round-trip probe passes
  - vendored MITM hook mutation probe passes
  - path-gated selective interception probe passes
  - micro-benchmark added for raw copy vs mutate-on-hook path
  - persistent-session soak test added for steady-state upgraded-session cost
  - larger mixed-route soak matrix passes with bidirectional masked/restore checks
  - resource-profile soak passes with goroutines returning to baseline after load
  - long-soak profile passes and writes heap/goroutine profiles to disk
- Current benchmark snapshot on 2026-03-27:
  - raw copy: `5401 ns/op`, `5128 B/op`, `18 allocs/op`
  - mutate text frame: `5719 ns/op`, `5352 B/op`, `19 allocs/op`
  - raw copy parallel: `5117 ns/op`, `5128 B/op`, `18 allocs/op`
  - mutate text frame parallel: `5924 ns/op`, `5352 B/op`, `19 allocs/op`
- Current soak snapshot on 2026-03-27:
  - persistent sessions: `8` concurrent clients
  - total frames: `800`
  - direct soak elapsed: `3.435685942s`
  - direct average per message: `4.294607ms`
  - mixed-route matrix: `12` concurrent clients, `600` total messages
  - matrix elapsed: `3.987359496s`
  - matrix average per message: `6.645599ms`
  - resource profile: `12` concurrent clients, `1440` total messages
  - resource profile elapsed: `4.872282669s`
  - heap before: `408720`
  - heap after GC: `440856`
  - heap delta: `32136`
  - goroutines base: `4`
  - goroutines peak: `68`
  - goroutines final: `4`
  - long soak profile: `18` concurrent clients, `4500` total messages
  - long soak elapsed: `28.507469814s`
  - long soak average per message: `6.334993ms`
  - long soak heap before: `277696`
  - long soak heap after GC: `411584`
  - long soak heap delta: `133888`
  - long soak goroutines base: `4`
  - long soak goroutines peak: `101`
  - long soak goroutines final: `4`
  - profile artifacts written to `logs/pprof-ws/`
- Source references:
  - `goproxy` repo: https://github.com/elazarl/goproxy
  - `goproxy` releases: https://github.com/elazarl/goproxy/releases
  - `goproxy` vulnerability note: https://developers.golang.nutanix.com/vuln/GO-2023-1941
  - `gomitmproxy` repo: https://github.com/AdguardTeam/gomitmproxy
  - `martian` repo: https://github.com/google/martian
  - `coder/websocket` repo: https://github.com/coder/websocket
  - `gobwas/ws` repo: https://github.com/gobwas/ws
- Notes:
  - overhead of the hook path is small in the current micro-benchmark
  - selective enablement by request path is now proven in the spike
  - the earlier connect-per-iteration benchmark was dominated by connection lifecycle cost and was less representative
  - the new persistent-session soak is the better signal for Codex-like usage because it keeps upgraded sessions open and pushes repeated masked frames
  - the larger matrix now covers:
    - larger Codex-shaped payloads
    - gated and ungated routes in the same run
    - client-to-server masking plus server-to-client restoration on the gated path
  - resource-profile soak shows no obvious goroutine leak in the current hook design on this workload
  - long-soak pprof capture is now in place and artifacted
  - next proof step is inspecting the captured heap/goroutine profiles and then wiring this gated hook into the first real Go proxy integration path

## Follow-up Contract

- Proxy hook shape: TBD
- Additional implementation complexity: TBD
