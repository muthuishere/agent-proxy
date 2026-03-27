"""Tests for the domain detector / allowlist loader."""
from proxy.detector import load_domains, _DEFAULT_DOMAINS


def _cfg(domains: list[str]) -> dict:
    return {"detection": {"intercepted_domains": domains}}


def test_load_domains_returns_listed_domains():
    domains = load_domains(_cfg(["api.anthropic.com", "api.openai.com"]))
    assert "api.anthropic.com" in domains
    assert "api.openai.com" in domains


def test_load_domains_no_detection_key_returns_defaults():
    domains = load_domains({})
    assert domains == set(_DEFAULT_DOMAINS)


def test_load_domains_empty_list_returns_defaults():
    domains = load_domains(_cfg([]))
    assert domains == set(_DEFAULT_DOMAINS)


def test_load_domains_none_value_returns_defaults():
    domains = load_domains({"detection": {"intercepted_domains": None}})
    assert domains == set(_DEFAULT_DOMAINS)


def test_default_domains_includes_anthropic():
    assert "api.anthropic.com" in _DEFAULT_DOMAINS


def test_default_domains_includes_openai():
    assert "api.openai.com" in _DEFAULT_DOMAINS


def test_default_domains_includes_chatgpt():
    assert "chatgpt.com" in _DEFAULT_DOMAINS


def test_load_domains_unlisted_domain_not_in_result():
    domains = load_domains(_cfg(["api.anthropic.com"]))
    assert "stripe.com" not in domains
    assert "localhost" not in domains


def test_load_domains_returns_set():
    domains = load_domains(_cfg(["api.anthropic.com", "api.anthropic.com"]))
    assert isinstance(domains, set)
    assert len(domains) == 1  # deduped


def test_load_domains_host_port_entry():
    """Entries like localhost:4000 are stored as-is."""
    domains = load_domains(_cfg(["localhost:4000"]))
    assert "localhost:4000" in domains
