import pytest
from proxy.scanner import Scanner

PATTERN_FILE = "./config/patterns.yaml"


@pytest.fixture
def scanner():
    return Scanner(PATTERN_FILE)


def test_detects_anthropic_key(scanner):
    text = "my key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456"
    matches = scanner.scan_text(text)
    names = [m.name for m in matches]
    assert "ANTHROPIC_API_KEY" in names


def test_detects_openai_proj_key(scanner):
    text = "openai key sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
    matches = scanner.scan_text(text)
    names = [m.name for m in matches]
    assert "OPENAI_API_KEY" in names


def test_detects_connection_string(scanner):
    text = "postgres://user:password@localhost:5432/mydb"
    matches = scanner.scan_text(text)
    names = [m.name for m in matches]
    assert "GENERIC_CONNECTION_STRING" in names


def test_detects_aws_credentials(scanner):
    text = "\n".join([
        "aws_access_key_id = AKIA1234567890ABCDEF",
        "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
        "aws_session_token = IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890",
    ])
    names = [m.name for m in scanner.scan_text(text)]
    assert "AWS_ACCESS_KEY" in names
    assert "AWS_ACCESS_KEY_ASSIGNMENT" in names
    assert "AWS_SECRET_KEY" in names
    assert "AWS_SESSION_TOKEN" in names


def test_detects_quoted_exported_aws_credentials(scanner):
    text = "\n".join([
        'export AWS_ACCESS_KEY_ID="AKIA1234567890ABCDEF"',
        'export AWS_SECRET_ACCESS_KEY="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"',
        'export AWS_SESSION_TOKEN="IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890"',
    ])
    names = [m.name for m in scanner.scan_text(text)]
    assert "AWS_ACCESS_KEY_ASSIGNMENT" in names
    assert "AWS_SECRET_KEY" in names
    assert "AWS_SESSION_TOKEN" in names


def test_detects_json_escaped_aws_credentials(scanner):
    text = (
        'export AWS_ACCESS_KEY_ID=\\"AKIA1234567890ABCDEF\\"\\n'
        'export AWS_SECRET_ACCESS_KEY=\\"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\\"\\n'
        'export AWS_SESSION_TOKEN=\\"IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890\\"'
    )
    names = [m.name for m in scanner.scan_text(text)]
    assert "AWS_ACCESS_KEY_ASSIGNMENT" in names
    assert "AWS_SECRET_KEY" in names
    assert "AWS_SESSION_TOKEN" in names


def test_detects_multiline_private_key_block(scanner):
    text = """-----BEGIN OPENSSH PRIVATE KEY-----
ZmFrZS1rZXktbWF0ZXJpYWw=
-----END OPENSSH PRIVATE KEY-----"""
    matches = scanner.scan_text(text)
    key_matches = [m for m in matches if m.name == "PRIVATE_KEY_BLOCK"]
    assert len(key_matches) == 1
    assert "END OPENSSH PRIVATE KEY" in key_matches[0].value


def test_connection_string_stops_before_json_escape(scanner):
    secret = "postgres://user:password@localhost:5432/mydb"
    text = '{"content": "' + secret + '\\nNext line"}'
    matches = scanner.scan_text(text)
    conn_matches = [m for m in matches if m.name == "GENERIC_CONNECTION_STRING"]
    assert len(conn_matches) == 1
    assert conn_matches[0].value == secret


def test_no_false_positive_clean_text(scanner):
    text = "hello world, this is a normal sentence with no secrets"
    matches = scanner.scan_text(text)
    assert len(matches) == 0


def test_threaded_scan_matches_single_threaded():
    text = "\n".join([
        "postgres://user:password@localhost:5432/mydb",
        "aws_access_key_id = AKIA1234567890ABCDEF",
        "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
        "-----BEGIN OPENSSH PRIVATE KEY-----",
        "ZmFrZS1rZXktbWF0ZXJpYWw=",
        "-----END OPENSSH PRIVATE KEY-----",
    ])
    single = Scanner(PATTERN_FILE, workers=1).scan_text(text)
    threaded = Scanner(PATTERN_FILE, workers=4).scan_text(text)
    assert [(m.name, m.value, m.start, m.end) for m in threaded] == [
        (m.name, m.value, m.start, m.end) for m in single
    ]
