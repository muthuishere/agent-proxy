import re
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass

import yaml


@dataclass
class Match:
    name: str
    value: str
    type: str
    confidence: str
    start: int
    end: int


class Scanner:
    def __init__(self, pattern_file: str, workers: int = 1, include_names: list[str] | None = None):
        self.patterns = self._load_patterns(pattern_file, include_names=include_names)
        self.workers = max(1, int(workers))

    def _load_patterns(self, path: str, include_names: list[str] | None = None) -> list[dict]:
        with open(path) as f:
            data = yaml.safe_load(f)
        compiled = []
        allowed_names = {name.upper() for name in include_names} if include_names else None
        for p in data.get("patterns", []):
            if allowed_names and p["name"].upper() not in allowed_names:
                continue
            try:
                compiled.append({**p, "_re": re.compile(p["regex"], re.MULTILINE)})
            except re.error:
                pass
        return compiled

    def scan_text(self, text: str) -> list[Match]:
        if self.workers == 1 or len(self.patterns) < 2:
            return sorted(
                self._scan_patterns(text, self.patterns),
                key=lambda match: (match.start, match.end, match.name),
            )

        with ThreadPoolExecutor(max_workers=min(self.workers, len(self.patterns))) as executor:
            chunks = executor.map(lambda pattern: self._scan_patterns(text, [pattern]), self.patterns)

        matches = []
        for chunk in chunks:
            matches.extend(chunk)
        return sorted(matches, key=lambda match: (match.start, match.end, match.name))

    def _scan_patterns(self, text: str, patterns: list[dict]) -> list[Match]:
        matches = []
        for pattern in patterns:
            for m in pattern["_re"].finditer(text):
                matches.append(
                    Match(
                        name=pattern["name"],
                        value=m.group(0),
                        type=pattern["type"],
                        confidence=pattern["confidence"],
                        start=m.start(),
                        end=m.end(),
                    )
                )
        return matches
