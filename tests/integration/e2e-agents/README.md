# E2E agent tests

Drives each supported AI coding agent (Claude, Codex, Copilot) through its
respective wrapper and verifies the proxy correctly intercepts the real
upstream call.

These are **integration** tests — they:

- Require AgentProxy to be running and the CA trusted in the OS store.
- Require each agent's CLI to be installed and authenticated.
- **Burn real API quota** on Anthropic / OpenAI / GitHub Copilot.

Therefore they are **not** in CI. Run on demand.

## What each script proves

| Script | Endpoint asserted | Notes |
|---|---|---|
| `test-claude-e2e.sh` | `api.anthropic.com /v1/messages` | SSE; multi-turn via `claude --continue -p` |
| `test-codex-e2e.sh` | `chatgpt.com /backend-api/codex/responses` | WebSocket upgrade (status 101); multi-turn via `codex exec resume --last` |
| `test-copilot-e2e.sh` | `api.individual.githubcopilot.com /responses` | Also asserts `api.github.com` is **not** intercepted (overbroad-scope regression) |

## Run locally

```bash
# Terminal A — start the proxy
task start

# Terminal B — run all three end-to-end
task test-e2e-agents

# Or one at a time
bash tests/integration/e2e-agents/test-claude-e2e.sh
bash tests/integration/e2e-agents/test-codex-e2e.sh
bash tests/integration/e2e-agents/test-copilot-e2e.sh
```

Each script prints `PASS:` / `FAIL:` lines as it goes. First failure exits
non-zero. Look at `logs/traffic.jsonl` for the full intercepted exchange.

## Multi-turn coverage

Each agent CLI implements multi-turn differently:

- **Claude** — `claude --continue -p "..."` continues the most-recent session.
- **Codex** — `codex exec resume --last "..."` resumes the most recent session.
- **Copilot** — currently invoked twice; per-session resume isn't a
  documented flag in the non-interactive `-p` path. The two invocations are
  independent sessions; what we verify is that the proxy handles
  back-to-back calls cleanly.

If you need stricter multi-turn assertions (e.g. session-id reuse), capture
the request bodies from `traffic.jsonl` and add a python check inside the
script.
