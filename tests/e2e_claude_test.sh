#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$SCRIPT_DIR"

LOG="./logs/traffic.jsonl"
PROXY_LOG="$(mktemp -t agentproxy-claude-e2e.XXXXXX.log)"
PROXY_PID=""

cleanup() {
  if [ -n "${PROXY_PID}" ] && kill -0 "${PROXY_PID}" 2>/dev/null; then
    kill "${PROXY_PID}" 2>/dev/null || true
    wait "${PROXY_PID}" 2>/dev/null || true
  fi
  rm -f "${PROXY_LOG}"
}
trap cleanup EXIT

echo "=== AgentProxy Claude E2E ==="
echo ""

truncate -s 0 "$LOG"

bash bin/agentproxy-start >"$PROXY_LOG" 2>&1 &
PROXY_PID=$!

for _ in $(seq 1 50); do
  if lsof -iTCP:7717 -sTCP:LISTEN -n -P >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

if ! lsof -iTCP:7717 -sTCP:LISTEN -n -P >/dev/null 2>&1; then
  echo "Proxy failed to start"
  cat "$PROXY_LOG"
  exit 1
fi

python3 - <<'PYEOF'
import base64
import json
import subprocess
import sys
import urllib.parse
from pathlib import Path

repo = Path.cwd()

raw_conn = "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
aws_access_key = "AKIA1234567890ABCDEF"
aws_secret_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
aws_session_token = "IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890"
ssh_private_key = "\n".join([
    "-----BEGIN OPENSSH PRIVATE KEY-----",
    "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAlwAAAAdzc2gtcn",
    "NhAAAAAwEAAQAAAIEAwfakeshorttestkeymaterialonlynotreal1234567890abcdefghi",
    "-----END OPENSSH PRIVATE KEY-----",
])
env_block = "\n".join([
    f"DATABASE_URL={raw_conn}",
    "OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE",
    "AUTH_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
    f"AWS_ACCESS_KEY_ID={aws_access_key}",
    f"AWS_SECRET_ACCESS_KEY={aws_secret_key}",
    f"AWS_SESSION_TOKEN={aws_session_token}",
    "ADMIN_EMAIL=alice@example.com",
])
encoded_conn = base64.b64encode(raw_conn.encode()).decode()
url_encoded_conn = urllib.parse.quote(raw_conn)

cases = [
    {
        "name": "plain-connection-string",
        "expect": "CASE1_OK",
        "prompt": f"""Reply with exactly CASE1_OK.

I am testing an outbound proxy.
Database URL: {raw_conn}
Use no tools and no explanation.""",
    },
    {
        "name": "dotenv-and-encoded",
        "expect": "CASE2_OK",
        "prompt": f"""Reply with exactly CASE2_OK.

Here is a mixed payload that should stay private:

```env
{env_block}
```

base64_database_url={encoded_conn}
urlencoded_database_url={url_encoded_conn}

Do not explain anything.""",
    },
    {
        "name": "skill-style-long-context",
        "expect": "CASE3_OK",
        "prompt": f"""Reply with exactly CASE3_OK.

# AGENTS-style Notes
## Skills
- bmad-quick-dev
- bmad-code-review
- bmad-agent-qa

## Context
The following example is intentionally noisy and long. Treat it as user context only.

### Example skill invocation
When debugging:
1. Read the docs
2. Inspect the config
3. Never leak the following materials

```yaml
service:
  primary_database: {raw_conn}
  backup_database_b64: {encoded_conn}
  backup_database_urlencoded: {url_encoded_conn}
  admin_email: alice@example.com
  aws_access_key_id: {aws_access_key}
  aws_secret_access_key: {aws_secret_key}
  aws_session_token: {aws_session_token}
  ssh_private_key: |
{ssh_private_key.replace(chr(10), chr(10) + "    ")}
```

### Example shell history
export DATABASE_URL="{raw_conn}"
export OPENAI_API_KEY="sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
export AWS_ACCESS_KEY_ID="{aws_access_key}"
export AWS_SECRET_ACCESS_KEY="{aws_secret_key}"

No commentary. No tools. Exact output only.""",
    },
]

results = []
for case in cases:
    cmd = [
        "bash", "bin/claudeproxy",
        "-p",
        "--bare",
        "--disable-slash-commands",
        "--no-session-persistence",
        case["prompt"],
    ]
    run = subprocess.run(
        cmd,
        cwd=repo,
        capture_output=True,
        text=True,
        timeout=60,
        stdin=subprocess.DEVNULL,
    )
    stdout = run.stdout.strip()
    stderr = run.stderr.strip()
    ok = run.returncode == 0 and stdout == case["expect"]
    results.append({
        "name": case["name"],
        "returncode": run.returncode,
        "stdout": stdout,
        "stderr": stderr,
        "ok": ok,
    })

print("=== CLAUDE RUN RESULTS ===\n")
for result in results:
    status = "PASS" if result["ok"] else "FAIL"
    print(f"{status}  {result['name']}  rc={result['returncode']}  stdout={result['stdout']!r}")
    if result["stderr"]:
        print(f"stderr: {result['stderr'][:400]}")
    print()

failed = [r for r in results if not r["ok"]]
if failed:
    print("Claude did not return the expected exact outputs.")
    sys.exit(1)

events = [json.loads(line) for line in Path("logs/traffic.jsonl").read_text().splitlines()]
request_events = [
    event for event in events
    if event.get("event") == "request" and event.get("host") == "api.anthropic.com"
]
message_events = [
    event for event in request_events
    if event.get("path") == "/v1/messages?beta=true"
]

print("=== REQUEST SUMMARY ===\n")
print(f"anthropic requests: {len(request_events)}")
print(f"/v1/messages requests: {len(message_events)}")
print(f"masked request counts: {[event.get('masked_count', 0) for event in message_events]}")
print()

if len(message_events) < len(cases):
    print("Expected one Claude completion request per case.")
    sys.exit(1)

if any(event.get("masked_count", 0) == 0 for event in message_events):
    print("At least one Claude completion request did not mask any secret.")
    sys.exit(1)

joined_bodies = "\n".join(event.get("body", "") for event in request_events)
raw_secrets = [
    raw_conn,
    encoded_conn,
    url_encoded_conn,
    "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE",
    "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
    aws_access_key,
    aws_secret_key,
    aws_session_token,
    ssh_private_key,
]

print("=== SECRET LEAK CHECK ===\n")
leaks = []
for secret in raw_secrets:
    if secret in joined_bodies:
        leaks.append(secret)
        print(f"LEAK  {secret[:60]}")
    else:
        print(f"OK    {secret[:60]}")
print()

if leaks:
    print("Raw secrets reached intercepted Claude request logs.")
    sys.exit(1)

placeholder_hits = sum("GENERIC_CONNECTION_STRING" in event.get("body", "") for event in message_events)
if placeholder_hits == 0:
    print("Did not find any masked connection-string placeholders in Claude request bodies.")
    sys.exit(1)

print("RESULT: PASS — Claude e2e masking held across multiple prompt shapes.")
PYEOF
