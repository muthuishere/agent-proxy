# Upstream Provenance

- Vendored from local fork: `/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/goproxywss`
- Fork HEAD at vendoring time: `ffdf0b284e35b1467352a39ee333743100f6b068`
- Upstream module path: `github.com/elazarl/goproxy`
- License: BSD 3-Clause

This copy is intentionally kept inside the repository so AgentProxy can:

- modify WebSocket-related proxy behavior locally
- run migration spikes against a stable in-repo source snapshot
- avoid coupling spike results to an external mutable dependency

If this vendored copy is updated, refresh this file and `THIRD_PARTY_NOTICES.md`.
