# Your AI Agent Reads Your Codebase. Does It Have to See Your Secrets?

AI coding assistants have become a genuine productivity multiplier. You hand them a task — "fix this bug", "refactor this service", "explain what this file does" — and they work through your codebase to do it.

The problem is they read *everything*. `.env` files. Config files with database passwords. Shell history with tokens you pasted in six months ago. API keys scattered across a dozen files that you keep meaning to rotate.

When the agent reads those files and builds its context, that entire context leaves your machine as the prompt. The connection is TLS-encrypted in transit, but the API provider decrypts it on their end — so the content, secrets and all, is visible to their servers and may land in their request logs.

You didn't intend to share those secrets. You probably didn't even know they were in the prompt. But they were.

---

## AgentProxy sits in the middle

AgentProxy is a local proxy that intercepts traffic between your AI agent and its remote API. Before any request leaves your machine, it scans the prompt for secrets, replaces them with placeholder tokens, and forwards the clean version. When the response comes back, it restores the original values — so the agent's context is intact and it can keep working normally.

```
Before:  Claude → api.anthropic.com
         prompt contains: "...the connection string is postgres://admin:hunter2@db.prod..."

After:   Claude → AgentProxy → api.anthropic.com
         prompt contains: "...the connection string is post****...****nter2[DB_CONN:a3f2]..."
```

Your secrets never hit the wire as plaintext. The agent never notices anything changed.

---

## Nothing leaves your machine

AgentProxy runs entirely on your laptop. There is no cloud component, no account to create, no data sent to a third party. It is a local binary listening on a port.

It intercepts HTTPS traffic using a local CA certificate that you install once. The cert lives at `~/.agentproxy/certs/` and is never shared with anyone. The proxy only listens on `127.0.0.1` — it is not reachable from the network.

---

## What gets detected

The proxy scans for patterns that match real secrets:

- API keys — Anthropic, OpenAI, GitHub, AWS, and more
- Database connection strings — `postgres://`, `mysql://`, Redis URLs
- Private key blocks — SSH, RSA, ECDSA, OpenSSH
- JWTs — any `eyJ...` token
- AWS credentials — access key IDs, secret access keys, session tokens

Secrets inside **encoded content** are also caught. If your prompt contains a base64-encoded config blob with a password inside, the proxy decodes it, finds the password, masks it, and re-encodes — so the agent receives a structurally valid blob with the secret replaced.

**PII masking** (email addresses, phone numbers, SSNs, credit card numbers) is available but disabled by default. You opt in.

---

## No code changes, no workflow changes

You don't modify your agents. You don't change how you use Claude or Codex. You just start the proxy and run your agent through a wrapper script:

```bash
# Start the proxy once
agentproxy-start

# Use your agent as normal — the wrapper sets the proxy env vars
claudeproxy  "summarise this codebase"
codexproxy   "fix the failing tests"
copilotproxy suggest "list the largest files"
```

The wrapper scripts set `HTTP_PROXY`, `HTTPS_PROXY`, and the CA cert environment variables that the agent runtimes expect. Everything else is transparent.

---

## Who this is for

**Individual developers** who use Claude Code, Codex, or GitHub Copilot on codebases that contain real credentials — which is most codebases.

**Teams** who want a lightweight guarantee that AI tooling won't accidentally exfiltrate secrets, without blocking the tools or adding friction.

**Security-conscious shops** who need evidence that secret material is not leaving the machine in plaintext, without building a custom solution.

If you have `.env` files in your repos and you use AI coding agents, AgentProxy is relevant to you.

---

## Getting started

**Requirements:** Go 1.23+

```bash
git clone https://github.com/your-org/agentproxy
cd agentproxy
./install.sh --trust
```

That builds the binary, exports the CA cert, trusts it in your OS keychain, and puts `claudeproxy`, `codexproxy`, and `copilotproxy` on your PATH. Run `agentproxy-start` in one terminal, use your agent normally in another.

The traffic log at `logs/traffic.jsonl` will show you exactly how many secrets were masked in each request — a quick sanity check that it is working.
