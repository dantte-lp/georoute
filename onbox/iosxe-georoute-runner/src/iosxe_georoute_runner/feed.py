from collections.abc import Iterable

import httpx

from .validate import PrefixSet, normalize_prefixes


def parse_prefix_lines(lines: Iterable[str]) -> PrefixSet:
    prefixes: list[str] = []
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        prefixes.append(stripped.split()[0])
    return normalize_prefixes(prefixes)


def fetch_text_feed(url: str, timeout: float = 10.0) -> PrefixSet:
    with httpx.Client(timeout=timeout, follow_redirects=True, trust_env=False) as client:
        response = client.get(url)
        response.raise_for_status()
    return parse_prefix_lines(response.text.splitlines())
