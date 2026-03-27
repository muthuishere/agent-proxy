"""mitmproxy addon — main pipeline.

Request flow:
  HTTP_PROXY → mitmproxy → request()
                              │
                  _domains check ──No──→ (optional passthrough log) → done
                              │Yes
                  decode_body() → _mask_body() → encode_body()
                  log_request()

Response flow:
  AI response → response()
                    │
        _domains check ──No──→ return
                    │Yes
        decode_body() → vault.restore() → encode_body()
        log_response()
"""
import os

import yaml
from mitmproxy import http

from proxy.decoder import decode_body, decode_encoded_blobs, encode_body
from proxy.detector import load_domains, _DEFAULT_DOMAINS
from proxy.logger import TrafficLogger
from proxy.scanner import Scanner
from proxy.vault import Vault


def _load_config(path: str) -> dict:
    with open(path) as f:
        return yaml.safe_load(f)


class AgentProxyAddon:
    def __init__(self):
        config_path = os.getenv("CONFIG_FILE", "./config/agentproxy.yaml")
        self.cfg = _load_config(config_path)

        masking = self.cfg.get("masking", {})
        self.vault = Vault(
            prefix_chars=masking.get("show_prefix_chars", 4),
            suffix_chars=masking.get("show_suffix_chars", 4),
            star_length=masking.get("star_length", 24),
        )

        detection = self.cfg.get("detection", {})
        self.scanner = Scanner(
            pattern_file=detection.get("pattern_file", "./config/patterns.yaml"),
            workers=detection.get("scan_workers", 4),
        )
        pii_cfg = self.cfg.get("pii", {})
        self.pii_enabled = pii_cfg.get("enabled", False)
        self.pii_scanner = None
        if self.pii_enabled:
            entity_map = {
                "email": "EMAIL_ADDRESS",
                "phone": "PHONE_NUMBER",
                "ssn": "US_SSN",
                "credit_card": "CREDIT_CARD",
                "ip_address": "IPV4_ADDRESS",
            }
            include_names = [entity_map[e] for e in pii_cfg.get("entities", []) if e in entity_map]
            if include_names:
                self.pii_scanner = Scanner(
                    pattern_file=pii_cfg.get("pattern_file", "./config/pii_patterns.yaml"),
                    workers=pii_cfg.get("scan_workers", detection.get("scan_workers", 4)),
                    include_names=include_names,
                )

        logging_cfg = self.cfg.get("logging", {})
        self.logger = TrafficLogger(
            log_file=logging_cfg.get("log_file", "./logs/traffic.jsonl"),
            log_originals=logging_cfg.get("log_originals", False),
            log_passthrough=logging_cfg.get("log_passthrough", False),
        )

        self._domains = load_domains(self.cfg)

    def _is_intercepted(self, host: str) -> bool:
        return host.lower().split(":")[0] in self._domains

    def _mask_body(self, body: str) -> tuple[str, int]:
        # Pass 1: scan raw text — secrets replaced with vault tokens (restorable)
        masked_count = 0
        for scanner in filter(None, [self.scanner, self.pii_scanner]):
            matches = scanner.scan_text(body)
            for match in matches:
                if match.value not in body:
                    continue
                placeholder = self.vault.mask(match.value, match.name)
                body = body.replace(match.value, placeholder)
                masked_count += 1

        # Pass 2: decode encoded blobs (base64/hex/url) — uses [MASKED:name] inline
        for scanner in filter(None, [self.scanner, self.pii_scanner]):
            body, encoded_masked = decode_encoded_blobs(body, scanner)
            masked_count += encoded_masked

        return body, masked_count

    def request(self, flow: http.HTTPFlow):
        host = flow.request.pretty_host
        path = flow.request.path
        method = flow.request.method

        if not self._is_intercepted(host):
            self.logger.log_passthrough(host, path, method)
            return

        content_encoding = flow.request.headers.get("content-encoding", "")
        content_type = flow.request.headers.get("content-type", "")

        body = decode_body(flow.request.content, content_encoding, content_type)
        body, masked_count = self._mask_body(body)

        if masked_count:
            flow.request.content = encode_body(body, content_encoding)

        safe_headers = {k: v for k, v in flow.request.headers.items()
                        if k.lower() not in ("authorization", "x-api-key")}

        self.logger.log_request(
            host=host, path=path, method=method,
            headers=safe_headers, body=body, masked_count=masked_count,
        )

    def response(self, flow: http.HTTPFlow):
        host = flow.request.pretty_host
        path = flow.request.path

        if not self._is_intercepted(host):
            return

        content_encoding = flow.response.headers.get("content-encoding", "")
        content_type = flow.response.headers.get("content-type", "")

        body = decode_body(flow.response.content, content_encoding, content_type)
        restored = self.vault.restore(body)
        if restored != body:
            flow.response.content = encode_body(restored, content_encoding)

        self.logger.log_response(
            host=host, path=path, status=flow.response.status_code,
            headers=dict(flow.response.headers), body=body[:2000], masked_count=0,
        )

    def websocket_message(self, flow: http.HTTPFlow):
        host = flow.request.pretty_host
        path = flow.request.path

        if not self._is_intercepted(host):
            return

        msg = flow.websocket.messages[-1]
        direction = "client->server" if msg.from_client else "server->client"

        if isinstance(msg.content, bytes):
            text = msg.content.decode("utf-8", errors="replace")
        else:
            text = str(msg.content)

        masked_text = text
        masked_count = 0

        if msg.from_client:
            masked_text, masked_count = self._mask_body(text)
            if masked_count:
                msg.content = masked_text.encode("utf-8")
        else:
            restored = self.vault.restore(text)
            if restored != text:
                msg.content = restored.encode("utf-8")
            masked_text = restored

        self.logger.log_websocket(
            host=host, path=path, direction=direction,
            body=masked_text, masked_count=masked_count,
        )


addons = [AgentProxyAddon()]
