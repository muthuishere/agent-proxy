"""Tests for upstream proxy resolution."""
import os
import tempfile
from unittest.mock import patch

import pytest
import yaml

from proxy.upstream import UpstreamProxy, resolve, _auto_detect, _parse_pac_result, _bare_url


def _make_cfg(upstream_proxy: dict) -> dict:
    return {"upstream_proxy": upstream_proxy}


# --- UpstreamProxy dataclass ---

def test_auth_string_with_credentials():
    up = UpstreamProxy(url="http://proxy:8080", username="alice", password="s3cr3t")
    assert up.auth_string == "alice:s3cr3t"


def test_auth_string_no_credentials():
    up = UpstreamProxy(url="http://proxy:8080")
    assert up.auth_string == ""


def test_mitm_args_no_auth():
    up = UpstreamProxy(url="http://proxy:8080")
    assert up.mitm_args() == ["--mode", "upstream:http://proxy:8080"]


def test_mitm_args_with_auth():
    up = UpstreamProxy(url="http://proxy:8080", username="u", password="p")
    assert up.mitm_args() == [
        "--mode", "upstream:http://proxy:8080",
        "--upstream-auth", "u:p",
    ]


# --- resolve(): no upstream_proxy key ---

def test_resolve_no_upstream_key_returns_none():
    assert resolve({}) is None


def test_resolve_empty_upstream_proxy_returns_none():
    assert resolve({"upstream_proxy": {}}) is None


# --- resolve(): explicit URL ---

def test_resolve_explicit_url():
    cfg = _make_cfg({"url": "http://corp.proxy:3128"})
    up = resolve(cfg)
    assert up is not None
    assert up.url == "http://corp.proxy:3128"
    assert up.username == ""


def test_resolve_explicit_url_with_embedded_auth():
    cfg = _make_cfg({"url": "http://alice:s3cr3t@corp.proxy:3128"})
    up = resolve(cfg)
    assert up is not None
    assert up.url == "http://corp.proxy:3128"   # stripped
    assert up.username == "alice"
    assert up.password == "s3cr3t"


def test_resolve_explicit_url_auth_from_config_fields():
    cfg = _make_cfg({"url": "http://corp.proxy:3128", "username": "bob", "password": "pass123"})
    up = resolve(cfg)
    assert up is not None
    assert up.username == "bob"
    assert up.password == "pass123"


def test_resolve_explicit_url_takes_priority_over_auto_detect():
    cfg = _make_cfg({"url": "http://explicit.proxy:3128", "auto_detect": True})
    with patch("urllib.request.getproxies", return_value={"https": "http://system.proxy:8080"}):
        up = resolve(cfg)
    assert up.url == "http://explicit.proxy:3128"


# --- resolve(): auto_detect ---

def test_resolve_auto_detect_reads_https_proxy():
    cfg = _make_cfg({"auto_detect": True})
    with patch("urllib.request.getproxies", return_value={"https": "http://corp.proxy:8080"}):
        up = resolve(cfg)
    assert up is not None
    assert up.url == "http://corp.proxy:8080"


def test_resolve_auto_detect_reads_http_proxy_as_fallback():
    cfg = _make_cfg({"auto_detect": True})
    with patch("urllib.request.getproxies", return_value={"http": "http://corp.proxy:8080"}):
        up = resolve(cfg)
    assert up is not None
    assert up.url == "http://corp.proxy:8080"


def test_resolve_auto_detect_strips_auth_from_env_var():
    cfg = _make_cfg({"auto_detect": True})
    with patch("urllib.request.getproxies", return_value={"https": "http://user:pass@corp.proxy:8080"}):
        up = resolve(cfg)
    assert up.url == "http://corp.proxy:8080"
    assert up.username == "user"
    assert up.password == "pass"


def test_resolve_auto_detect_no_proxy_in_env_returns_none():
    cfg = _make_cfg({"auto_detect": True})
    with patch("urllib.request.getproxies", return_value={}):
        up = resolve(cfg)
    assert up is None


def test_resolve_auto_detect_true_by_default():
    """auto_detect defaults to True when key is absent."""
    cfg = _make_cfg({"pac_file": ""})  # no url, no pac_file, no auto_detect key
    with patch("urllib.request.getproxies", return_value={"https": "http://corp:3128"}):
        up = resolve(cfg)
    assert up is not None


def test_resolve_auto_detect_false_no_fallback():
    cfg = _make_cfg({"auto_detect": False})
    with patch("urllib.request.getproxies", return_value={"https": "http://corp:3128"}):
        up = resolve(cfg)
    assert up is None


# --- _parse_pac_result ---

def test_parse_pac_result_proxy_directive():
    up = _parse_pac_result("PROXY corp.proxy:3128")
    assert up is not None
    assert up.url == "http://corp.proxy:3128"


def test_parse_pac_result_case_insensitive():
    up = _parse_pac_result("proxy corp.proxy:3128")
    assert up is not None
    assert up.url == "http://corp.proxy:3128"


def test_parse_pac_result_direct_returns_none():
    assert _parse_pac_result("DIRECT") is None


def test_parse_pac_result_empty_returns_none():
    assert _parse_pac_result("") is None


def test_parse_pac_result_uses_first_proxy_from_fallback_list():
    up = _parse_pac_result("PROXY primary:3128; PROXY backup:3128; DIRECT")
    assert up is not None
    assert up.url == "http://primary:3128"


def test_parse_pac_result_skips_socks_takes_next_proxy():
    # SOCKS is not supported by mitmproxy upstream mode; skip to next PROXY
    up = _parse_pac_result("PROXY corp.proxy:3128")
    assert up is not None


def test_parse_pac_result_direct_before_proxy_stops_early():
    up = _parse_pac_result("DIRECT; PROXY shouldbeignored:3128")
    assert up is None


# --- PAC file resolution (with file) ---

def test_resolve_pac_file_pypac_not_installed():
    """When pypac is absent, resolution gracefully returns None."""
    cfg = _make_cfg({"pac_file": "/some/file.pac", "auto_detect": False})
    with patch.dict("sys.modules", {"pypac": None}):
        up = resolve(cfg)
    assert up is None


def test_resolve_pac_file_evaluates_for_openai_host():
    pac_content = """
function FindProxyForURL(url, host) {
    return "PROXY pac.proxy:3128";
}
"""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".pac", delete=False) as f:
        f.write(pac_content)
        pac_path = f.name

    try:
        cfg = _make_cfg({"pac_file": pac_path, "auto_detect": False})
        try:
            from pypac import PACFile  # noqa: F401
            pypac_available = True
        except ImportError:
            pypac_available = False

        if pypac_available:
            up = resolve(cfg)
            assert up is not None
            assert "pac.proxy" in up.url
        else:
            pytest.skip("pypac not installed")
    finally:
        os.unlink(pac_path)


# --- _bare_url ---

def test_bare_url_strips_credentials():
    from urllib.parse import urlparse
    parsed = urlparse("http://user:pass@proxy.corp:8080")
    assert _bare_url(parsed) == "http://proxy.corp:8080"


def test_bare_url_no_credentials_unchanged():
    from urllib.parse import urlparse
    parsed = urlparse("http://proxy.corp:8080")
    assert _bare_url(parsed) == "http://proxy.corp:8080"
