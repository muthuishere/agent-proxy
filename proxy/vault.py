import hashlib
import threading


class Vault:
    """Maps placeholder tokens back to original values for response restoration."""

    def __init__(self, prefix_chars: int = 4, suffix_chars: int = 4, star_length: int = 24):
        self._store: dict[str, str] = {}
        self._lock = threading.Lock()
        self.prefix_chars = prefix_chars
        self.suffix_chars = suffix_chars
        self.star_length = star_length

    def mask(self, value: str, name: str) -> str:
        placeholder = self._make_placeholder(value, name)
        with self._lock:
            self._store[placeholder] = value
        return placeholder

    def restore(self, text: str) -> str:
        with self._lock:
            for placeholder, original in self._store.items():
                text = text.replace(placeholder, original)
        return text

    def _make_placeholder(self, value: str, name: str) -> str:
        prefix = value[: self.prefix_chars] if len(value) > self.prefix_chars else value
        suffix = value[-self.suffix_chars :] if len(value) > self.suffix_chars else ""
        digest = hashlib.sha256(value.encode()).hexdigest()[:8]
        stars = "*" * self.star_length
        return f"{prefix}{stars}{suffix}[{name}:{digest}]"

    def clear(self):
        with self._lock:
            self._store.clear()
