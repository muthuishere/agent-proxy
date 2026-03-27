import json
import sys
from datetime import datetime, timezone
from pathlib import Path


class TrafficLogger:
    def __init__(self, log_file: str, log_originals: bool = False, log_passthrough: bool = False):
        self.log_file = log_file
        self.log_originals = log_originals
        self._log_passthrough = log_passthrough
        Path(log_file).parent.mkdir(parents=True, exist_ok=True)

    def log_request(self, host: str, path: str, method: str, headers: dict,
                    body: str, masked_count: int):
        self._write({
            "event": "request",
            "method": method,
            "host": host,
            "path": path,
            "headers": headers,
            "body": body,
            "masked_count": masked_count,
        })

    def log_response(self, host: str, path: str, status: int, headers: dict,
                     body: str, masked_count: int):
        self._write({
            "event": "response",
            "host": host,
            "path": path,
            "status": status,
            "headers": headers,
            "body": body,
            "masked_count": masked_count,
        })

    def log_websocket(self, host: str, path: str, direction: str, body: str, masked_count: int):
        self._write({
            "event": "websocket",
            "host": host,
            "path": path,
            "direction": direction,
            "body": body[:8000],
            "masked_count": masked_count,
        })

    def log_passthrough(self, host: str, path: str, method: str):
        if not self._log_passthrough:
            return
        self._write({
            "event": "passthrough",
            "method": method,
            "host": host,
            "path": path,
        })

    def _write(self, record: dict):
        record["ts"] = datetime.now(timezone.utc).isoformat()
        line = json.dumps(record)
        try:
            with open(self.log_file, "a") as f:
                f.write(line + "\n")
        except OSError as e:
            print(f"[agentproxy] WARNING: could not write log: {e}", flush=True, file=sys.stderr)
        if record["event"] == "websocket":
            direction = record.get("direction", "")
            snippet = record.get("body", "")[:120].replace("\n", " ")
            print(f"[agentproxy] WS {direction} {record.get('host','')}{record.get('path','')} "
                  f"masked={record.get('masked_count',0)} | {snippet}",
                  flush=True, file=sys.stderr)
        else:
            print(f"[agentproxy] {record['event'].upper()} {record.get('method','')}"
                  f" {record.get('host','')}{record.get('path','')} "
                  f"status={record.get('status','-')} masked={record.get('masked_count',0)}",
                  flush=True, file=sys.stderr)
