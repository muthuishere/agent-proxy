"""Upstream proxy resolution for AgentProxy.

Sits between AgentProxy and the existing corporate/enterprise proxy so the
full chain is:

  Agent → AgentProxy (masks secrets) → upstream proxy → internet → AI API

Priority order (first match wins):
  1. Explicit URL in agentproxy.yaml  (upstream_proxy.url)
  2. PAC file                         (upstream_proxy.pac_file)  — requires pypac
  3. Auto-detect from env / system    (upstream_proxy.auto_detect: true)
  4. None — connect directly

PAC file support requires the pypac package:
  uv add pypac
"""
import sys
import urllib.request
from dataclasses import dataclass
from urllib.parse import urlparse, urlunparse


@dataclass
class UpstreamProxy:
    url: str        # bare URL, no embedded credentials: http://host:port
    username: str = ""
    password: str = ""

    @property
    def auth_string(self) -> str:
        """'user:pass' for --upstream-auth, empty if no auth."""
        return f"{self.username}:{self.password}" if self.username else ""

    def mitm_args(self) -> list[str]:
        """mitmproxy command-line flags for upstream proxy."""
        args = ["--mode", f"upstream:{self.url}"]
        if self.auth_string:
            args += ["--upstream-auth", self.auth_string]
        return args


def resolve(cfg: dict) -> "UpstreamProxy | None":
    """Resolve upstream proxy from agentproxy.yaml config dict.

    Returns None if no upstream proxy is configured or detected.
    """
    upstream_cfg = cfg.get("upstream_proxy", {})
    if not upstream_cfg:
        return None

    # Case 1: explicit URL in config
    explicit_url = (upstream_cfg.get("url") or "").strip()
    if explicit_url:
        parsed = urlparse(explicit_url)
        return UpstreamProxy(
            url=_bare_url(parsed),
            username=upstream_cfg.get("username") or parsed.username or "",
            password=upstream_cfg.get("password") or parsed.password or "",
        )

    # Case 2: PAC file
    pac_source = (upstream_cfg.get("pac_file") or "").strip()
    if pac_source:
        result = _resolve_pac(pac_source)
        if result is not None:
            return result

    # Case 3: auto-detect from environment variables and OS system settings
    if upstream_cfg.get("auto_detect", True):
        return _auto_detect()

    return None


def _auto_detect() -> "UpstreamProxy | None":
    """Read proxy URL from HTTP_PROXY/HTTPS_PROXY env vars and OS system settings.

    On macOS this also reads System Preferences → Network → Proxies.
    On Windows it reads the registry.
    """
    proxies = urllib.request.getproxies()
    proxy_url = proxies.get("https") or proxies.get("http") or ""
    if not proxy_url:
        return None
    parsed = urlparse(proxy_url)
    if not parsed.hostname:
        return None
    return UpstreamProxy(
        url=_bare_url(parsed),
        username=parsed.username or "",
        password=parsed.password or "",
    )


def _resolve_pac(pac_source: str) -> "UpstreamProxy | None":
    """Evaluate a PAC file and return the proxy for a representative AI API host.

    pac_source may be a local file path or an http(s):// URL.
    Evaluates against https://api.openai.com/v1/chat/completions — which covers
    the vast majority of PAC files since they typically return the same proxy for
    all external HTTPS traffic.
    """
    try:
        from pypac import PACFile  # type: ignore[import]
    except ImportError:
        print(
            "[agentproxy] WARNING: pypac not installed — PAC file support requires it. "
            "Install with: uv add pypac",
            flush=True, file=sys.stderr,
        )
        return None

    try:
        content = _fetch_pac(pac_source)
        if not content:
            return None

        pac = PACFile(content)
        # find_proxy_for_url returns e.g. "PROXY corp.proxy:8080" or "DIRECT"
        # or "PROXY a:80; PROXY b:80; DIRECT" (semicolon-separated fallback list)
        raw = pac.find_proxy_for_url(
            "https://api.openai.com/v1/chat/completions",
            "api.openai.com",
        )
        return _parse_pac_result(raw)

    except Exception as e:
        print(
            f"[agentproxy] WARNING: PAC file evaluation failed: {e}",
            flush=True, file=sys.stderr,
        )
        return None


def _fetch_pac(pac_source: str) -> str:
    """Fetch PAC content from a URL or local file path."""
    if pac_source.startswith(("http://", "https://")):
        resp = urllib.request.urlopen(pac_source, timeout=10)
        return resp.read().decode()
    with open(pac_source) as f:
        return f.read()


def _parse_pac_result(raw: str) -> "UpstreamProxy | None":
    """Parse PAC FindProxyForURL result string.

    Format: "PROXY host:port; PROXY host2:port2; DIRECT"
    We use the first PROXY entry; ignore SOCKS (not supported by mitmproxy upstream mode).
    """
    if not raw:
        return None
    for directive in raw.split(";"):
        directive = directive.strip()
        upper = directive.upper()
        if upper.startswith("PROXY "):
            host_port = directive[6:].strip()
            proxy_url = f"http://{host_port}"
            parsed = urlparse(proxy_url)
            return UpstreamProxy(
                url=_bare_url(parsed),
                username=parsed.username or "",
                password=parsed.password or "",
            )
        if upper == "DIRECT":
            break  # first DIRECT means no proxy
    return None


def _bare_url(parsed) -> str:
    """Return URL with credentials stripped from netloc."""
    host = parsed.hostname or ""
    port = f":{parsed.port}" if parsed.port else ""
    scheme = parsed.scheme or "http"
    return urlunparse(parsed._replace(scheme=scheme, netloc=f"{host}{port}"))


def main() -> None:
    """CLI entry point: print mitmdump upstream args to stdout (empty if none).

    Usage: python -m proxy.upstream [config_path]
    Stdout: e.g. "--mode upstream:http://corp.proxy:8080 --upstream-auth user:pass"
    """
    import yaml

    config_path = sys.argv[1] if len(sys.argv) > 1 else "./config/agentproxy.yaml"
    try:
        with open(config_path) as f:
            cfg = yaml.safe_load(f)
    except FileNotFoundError:
        return  # no config file → no upstream

    upstream = resolve(cfg)
    if upstream:
        print(" ".join(upstream.mitm_args()))
        print(
            f"[agentproxy] Upstream proxy: {upstream.url}"
            + (f" (auth: {upstream.username}:***)" if upstream.username else ""),
            file=sys.stderr, flush=True,
        )


if __name__ == "__main__":
    main()
