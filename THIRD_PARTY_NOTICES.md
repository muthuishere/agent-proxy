# Third-Party Notices

This repository vendors third-party source code for internal modification and evaluation.

## `goproxywss`

- Local path: `third_party/goproxywss`
- Upstream module path: `github.com/elazarl/goproxy`
- Source repository used for vendoring:
  - local fork: `/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/goproxywss`
  - fork base commit used for vendoring: `ffdf0b284e35b1467352a39ee333743100f6b068`
- License: BSD 3-Clause

Why this is vendored:

- We need to spike and potentially modify WebSocket-related proxy behavior in-repo.
- We do not want the migration spike to depend on fetching a mutable external fork at build time.

Compliance notes:

- The vendored source remains subject to the upstream BSD 3-Clause license.
- Modifications inside this repository do not remove the original license obligations.
- The original license text is preserved in `third_party/goproxywss/LICENSE`.
