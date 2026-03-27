"""Integration tests for the main addon pipeline."""
import base64
import gzip
from unittest.mock import MagicMock

import pytest
import yaml

from proxy.addon import AgentProxyAddon


def _make_request_flow(host, path, method="POST", body=b"",
                       content_type="application/json", content_encoding=""):
    flow = MagicMock()
    flow.request.pretty_host = host
    flow.request.path = path
    flow.request.method = method
    flow.request.content = body
    flow.request.headers = {
        "content-type": content_type,
        "content-encoding": content_encoding,
    }
    return flow


def _make_response_flow(host, path, body=b"", status=200,
                        content_type="application/json", content_encoding=""):
    flow = MagicMock()
    flow.request.pretty_host = host
    flow.request.path = path
    flow.request.headers = {"content-type": content_type}
    flow.response.content = body
    flow.response.status_code = status
    flow.response.headers = {"content-type": content_type, "content-encoding": content_encoding}
    return flow


def _make_ws_flow(host, path, text, from_client=True):
    flow = MagicMock()
    flow.request.pretty_host = host
    flow.request.path = path
    msg = MagicMock()
    msg.content = text.encode("utf-8") if isinstance(text, str) else text
    msg.from_client = from_client
    flow.websocket.messages = [msg]
    return flow, msg


@pytest.fixture
def addon():
    return AgentProxyAddon()


def _write_config(tmp_path, pii_enabled=False, entities=None):
    cfg = {
        "proxy": {"port": 7717, "host": "127.0.0.1"},
        "tls": {"cert_dir": "./certs"},
        "detection": {
            "pattern_file": "./config/patterns.yaml",
            "scan_workers": 4,
            "intercepted_domains": ["api.anthropic.com", "api.openai.com", "chatgpt.com"],
        },
        "pii": {
            "enabled": pii_enabled,
            "pattern_file": "./config/pii_patterns.yaml",
            "scan_workers": 2,
            "entities": entities or ["email", "phone", "ssn", "credit_card", "ip_address"],
        },
        "masking": {"show_prefix_chars": 4, "show_suffix_chars": 4, "star_length": 24},
        "logging": {"log_file": str(tmp_path / "traffic.jsonl"), "log_originals": False, "log_passthrough": False},
    }
    path = tmp_path / "agentproxy.yaml"
    path.write_text(yaml.safe_dump(cfg))
    return path


# --- request masking ---

def test_request_masks_api_key_in_prompt(addon):
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    body = f'{{"model": "claude-3", "messages": [{{"role": "user", "content": "key={secret}"}}]}}'
    flow = _make_request_flow("api.anthropic.com", "/v1/messages", body=body.encode())
    addon.request(flow)
    assert secret.encode() not in flow.request.content


def test_request_masks_openai_key(addon):
    secret = "sk-abcdefghijklmnopqrstuvwxyz123456"
    body = f'{{"model": "gpt-4", "messages": [{{"role": "user", "content": "token={secret}"}}]}}'
    flow = _make_request_flow("api.openai.com", "/v1/chat/completions", body=body.encode())
    addon.request(flow)
    assert secret.encode() not in flow.request.content


def test_request_clean_prompt_untouched(addon):
    body = b'{"model": "gpt-4", "messages": [{"role": "user", "content": "what is 2+2"}]}'
    flow = _make_request_flow("api.openai.com", "/v1/chat/completions", body=body)
    addon.request(flow)
    assert flow.request.content == body


def test_request_empty_body_no_crash(addon):
    flow = _make_request_flow("api.anthropic.com", "/v1/messages", body=b"")
    addon.request(flow)
    assert flow.request.content == b""


def test_request_passthrough_for_unlisted_domain(addon):
    body = b'{"amount": 2000, "currency": "usd"}'
    flow = _make_request_flow("stripe.com", "/v1/charges", body=body)
    addon.request(flow)
    assert flow.request.content == body


def test_request_passthrough_for_localhost(addon):
    """localhost is not intercepted unless explicitly added to domains.yaml."""
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    body = f'{{"model": "claude-3", "messages": [{{"role": "user", "content": "{secret}"}}]}}'.encode()
    flow = _make_request_flow("localhost", "/v1/chat/completions", body=body)
    addon.request(flow)
    assert flow.request.content == body


def test_request_leaves_pii_when_disabled(tmp_path, monkeypatch):
    cfg_path = _write_config(tmp_path, pii_enabled=False)
    monkeypatch.setenv("CONFIG_FILE", str(cfg_path))
    local_addon = AgentProxyAddon()
    body = b'{"messages":[{"role":"user","content":"email alice@example.com ssn 123-45-6789"}]}'
    flow = _make_request_flow("api.anthropic.com", "/v1/messages", body=body)
    local_addon.request(flow)
    assert b"alice@example.com" in flow.request.content
    assert b"123-45-6789" in flow.request.content
    monkeypatch.delenv("CONFIG_FILE", raising=False)


def test_request_masks_selected_pii_when_enabled(tmp_path, monkeypatch):
    cfg_path = _write_config(tmp_path, pii_enabled=True, entities=["email", "ssn", "phone"])
    monkeypatch.setenv("CONFIG_FILE", str(cfg_path))
    local_addon = AgentProxyAddon()
    body = (
        b'{"messages":[{"role":"user","content":"'
        b'email alice@example.com phone 415-555-2671 ssn 123-45-6789 ip 10.20.30.40'
        b'"}]}'
    )
    flow = _make_request_flow("api.anthropic.com", "/v1/messages", body=body)
    local_addon.request(flow)
    assert b"alice@example.com" not in flow.request.content
    assert b"415-555-2671" not in flow.request.content
    assert b"123-45-6789" not in flow.request.content
    assert b"10.20.30.40" in flow.request.content
    monkeypatch.delenv("CONFIG_FILE", raising=False)


# --- listed domain: any body shape is intercepted ---

def test_listed_domain_intercepts_non_ai_shaped_body(addon):
    """Domains in domains.yaml get all traffic scanned regardless of body shape."""
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    body = f'{{"data": "{secret}"}}'.encode()
    flow = _make_request_flow("api.anthropic.com", "/anything", body=body)
    addon.request(flow)
    assert secret.encode() not in flow.request.content


def test_listed_domain_intercepts_any_path(addon):
    """Listed domain: path doesn't need to look like an AI endpoint."""
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    body = f'{{"key": "{secret}"}}'.encode()
    flow = _make_request_flow("api.anthropic.com", "/some/random/path", body=body)
    addon.request(flow)
    assert secret.encode() not in flow.request.content


# --- base64 encoded secrets ---

def test_request_masks_base64_encoded_secret(addon):
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    encoded = base64.b64encode(secret.encode()).decode()
    body = f'{{"model": "claude-3", "messages": [{{"role": "user", "content": "data={encoded}"}}]}}'.encode()
    flow = _make_request_flow("api.anthropic.com", "/v1/messages", body=body)
    addon.request(flow)
    assert secret.encode() not in flow.request.content


# --- gzip content-encoding ---

def test_request_gzip_roundtrip_masks_secret(addon):
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    body_str = f'{{"model": "claude-3", "messages": [{{"role": "user", "content": "{secret}"}}]}}'
    compressed = gzip.compress(body_str.encode())
    flow = _make_request_flow(
        "api.anthropic.com", "/v1/messages",
        body=compressed, content_encoding="gzip",
    )
    addon.request(flow)
    # Re-decompress what was written back — secret must not be in there
    decompressed = gzip.decompress(flow.request.content).decode()
    assert secret not in decompressed


# --- response restoration ---

def test_response_restores_masked_token(addon):
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    body = f'{{"model": "claude-3", "messages": [{{"role": "user", "content": "{secret}"}}]}}'
    req_flow = _make_request_flow("api.anthropic.com", "/v1/messages", body=body.encode())
    addon.request(req_flow)

    masked_body = req_flow.request.content
    assert secret.encode() not in masked_body

    resp_flow = _make_response_flow("api.anthropic.com", "/v1/messages", body=masked_body)
    addon.response(resp_flow)
    assert secret.encode() in resp_flow.response.content


def test_response_passthrough_unlisted_domain(addon):
    body = b'{"result": "ok"}'
    flow = _make_response_flow("stripe.com", "/v1/charges", body=body)
    addon.response(flow)
    assert flow.response.content == body


# --- websocket ---

def test_websocket_client_message_masks_secret(addon):
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    text = f'{{"type": "message", "content": "{secret}"}}'
    flow, msg = _make_ws_flow("chatgpt.com", "/v1/realtime", text, from_client=True)
    addon.websocket_message(flow)
    assert secret.encode() not in msg.content


def test_websocket_client_clean_message_untouched(addon):
    text = '{"type": "message", "content": "hello"}'
    flow, msg = _make_ws_flow("chatgpt.com", "/v1/realtime", text, from_client=True)
    original = msg.content
    addon.websocket_message(flow)
    assert msg.content == original


def test_websocket_server_message_restores_token(addon):
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    # First mask via a client message so vault has the token
    req_flow, req_msg = _make_ws_flow("chatgpt.com", "/v1/realtime", secret, from_client=True)
    addon.websocket_message(req_flow)
    placeholder = req_msg.content.decode()

    # Now server echoes the placeholder back — should be restored
    resp_flow, resp_msg = _make_ws_flow("chatgpt.com", "/v1/realtime", placeholder, from_client=False)
    addon.websocket_message(resp_flow)
    assert secret.encode() in resp_msg.content


def test_websocket_unlisted_domain_skipped(addon):
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    text = f"key={secret}"
    flow, msg = _make_ws_flow("unknown.example.com", "/ws", text, from_client=True)
    original = msg.content
    addon.websocket_message(flow)
    assert msg.content == original  # untouched
