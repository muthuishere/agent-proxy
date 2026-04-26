#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$SCRIPT_DIR"

LOG="./logs/traffic.jsonl"

echo "=== AgentProxy E2E Masking Test ==="
echo ""

# Clear log
truncate -s 0 "$LOG"

# The prompt contains secrets that MUST be masked before reaching OpenAI
PROMPT='I have these two connection strings in my app config:

Postgres: postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb

JDBC: jdbc:postgresql://db.internal.company.com:5432/proddb?user=admin&password=SuperSecret123

How do I connect to this database from Python using psycopg2? Show a minimal code example.'

echo "--- PROMPT SENT ---"
echo "$PROMPT"
echo ""
echo "--- RUNNING THROUGH PROXY ---"
echo ""

HTTP_PROXY="http://127.0.0.1:7717" \
HTTPS_PROXY="http://127.0.0.1:7717" \
SSL_CERT_FILE="$HOME/.agentproxy/certs/agentproxy-ca-cert.pem" \
  codex exec "$PROMPT" 2>&1

echo ""
echo "--- ANALYSING PROXY LOG ---"
echo ""

python3 - <<'PYEOF'
import json, re, sys

log_file = "./logs/traffic.jsonl"
events = [json.loads(l) for l in open(log_file)]

secrets = [
    "SuperSecret123",
    "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb",
    "jdbc:postgresql://db.internal.company.com:5432/proddb?user=admin&password=SuperSecret123",
]

print("=== WS FRAMES SUMMARY ===\n")
for r in events:
    if r["event"] != "websocket":
        continue
    body = r.get("body", "")
    direction = r.get("direction", "")
    masked = r.get("masked_count", 0)
    try:
        msg_type = json.loads(body).get("type", "?")
    except Exception:
        msg_type = "?"
    print(f"  [{direction}] type={msg_type} masked={masked}")

print()
print("=== SECRET LEAK CHECK ===\n")

all_bodies = " ".join(r.get("body","") for r in events)

leak_found = False
for secret in secrets:
    if secret in all_bodies:
        print(f"  LEAK DETECTED: {secret[:40]}...")
        leak_found = True
    else:
        print(f"  OK - not in log: {secret[:40]}...")

print()
if leak_found:
    print("RESULT: FAIL — secrets found in intercepted traffic log")
else:
    print("RESULT: PASS — all secrets were masked before leaving the machine")

print()
print("=== MASKED OUTBOUND FRAMES ===\n")
for r in events:
    if r["event"] == "websocket" and r.get("masked_count", 0) > 0 and r.get("direction") == "client->server":
        body = r.get("body", "")
        placeholders = re.findall(r'\S{1,10}\*{6,}\S*\[[A-Z_]+:[a-f0-9]+\]', body)
        print(f"  masked_count={r['masked_count']}")
        for ph in placeholders:
            idx = body.find(ph)
            ctx_start = max(0, idx - 60)
            ctx_end = min(len(body), idx + len(ph) + 60)
            print(f"    placeholder: {ph}")
            print(f"    context   : ...{body[ctx_start:ctx_end]}...")
            print()

print()
print("=== INBOUND RESPONSE FRAMES ===\n")
for r in events:
    if r["event"] == "websocket" and r.get("direction") == "server->client":
        body = r.get("body", "")
        try:
            parsed = json.loads(body)
            if parsed.get("type") == "response.output_text.delta":
                print(f"  delta text: {parsed.get('delta','')}")
        except Exception:
            pass
PYEOF
