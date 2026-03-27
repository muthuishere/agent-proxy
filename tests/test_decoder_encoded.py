import base64
import json
from proxy.decoder import decode_encoded_blobs
from proxy.scanner import Scanner

PATTERN_FILE = "./config/patterns.yaml"


def scanner():
    return Scanner(PATTERN_FILE)


def test_base64_secret_detected():
    sc = scanner()
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    encoded = base64.b64encode(secret.encode()).decode()
    text = f"use this key: {encoded}"
    result, count = decode_encoded_blobs(text, sc)
    assert count > 0
    assert secret not in result


def test_base64_dotenv_block_detected():
    sc = scanner()
    env_block = "ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456\nOPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz123456"
    encoded = base64.b64encode(env_block.encode()).decode()
    text = f"here is my config: {encoded}"
    result, count = decode_encoded_blobs(text, sc)
    assert count > 0
    assert "sk-ant-api03" not in result
    assert "sk-abcdef" not in result


def test_hex_secret_detected():
    sc = scanner()
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    encoded = secret.encode().hex()
    text = f"encoded key: {encoded}"
    result, count = decode_encoded_blobs(text, sc)
    assert count > 0
    assert secret not in result


def test_url_encoded_secret_detected():
    sc = scanner()
    from urllib.parse import quote
    secret = "postgres://admin:SuperSecret@db.internal.com:5432/prod"
    encoded = quote(secret)
    text = f"dsn={encoded}"
    result, count = decode_encoded_blobs(text, sc)
    assert count > 0
    assert secret not in result
    assert "%" in result


def test_url_encoded_secret_keeps_json_valid():
    sc = scanner()
    from urllib.parse import quote
    secret = "postgres://admin:SuperSecret@db.internal.com:5432/prod"
    encoded = quote(secret)
    text = json.dumps({"content": f"urlencoded={encoded}"})
    result, count = decode_encoded_blobs(text, sc)
    assert count > 0
    parsed = json.loads(result)
    assert "SuperSecret" not in parsed["content"]
    assert "%5BMASKED%3A" in parsed["content"]


def test_double_base64_detected():
    sc = scanner()
    secret = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
    once = base64.b64encode(secret.encode()).decode()
    twice = base64.b64encode(once.encode()).decode()
    text = f"data: {twice}"
    result, count = decode_encoded_blobs(text, sc)
    assert count > 0


def test_clean_base64_not_flagged():
    sc = scanner()
    # A normal base64 string with no secrets inside
    plain = "Hello, this is a normal message with no secrets at all."
    encoded = base64.b64encode(plain.encode()).decode()
    text = f"message: {encoded}"
    result, count = decode_encoded_blobs(text, sc)
    assert count == 0
    assert result == text


def test_clean_text_untouched():
    sc = scanner()
    text = "just a normal prompt asking about Python lists"
    result, count = decode_encoded_blobs(text, sc)
    assert count == 0
    assert result == text
