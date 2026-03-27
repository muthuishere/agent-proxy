import sys

_DEFAULT_DOMAINS: set[str] = {
    "api.anthropic.com",
    "api.openai.com",
    "chatgpt.com",
    "api.githubcopilot.com",
    "copilot-proxy.githubusercontent.com",
}


def load_domains(cfg: dict) -> set[str]:
    """Load intercepted domains from the agentproxy.yaml config dict.

    Reads detection.intercepted_domains. Falls back to built-in defaults
    if the key is absent or the list is empty.
    """
    domains = cfg.get("detection", {}).get("intercepted_domains") or []
    if not domains:
        print(
            "[agentproxy] WARNING: no intercepted_domains in config — using built-in defaults",
            flush=True, file=sys.stderr,
        )
        return set(_DEFAULT_DOMAINS)
    return set(domains)
