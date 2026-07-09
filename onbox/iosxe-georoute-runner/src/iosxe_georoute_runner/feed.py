from collections.abc import Iterable
from time import sleep

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


def fetch_text_feed(
    url: str,
    timeout: float = 10.0,
    retry_attempts: int = 3,
    retry_base_delay: float = 0.5,
) -> PrefixSet:
    if retry_attempts < 1:
        msg = "retry_attempts must be >= 1"
        raise ValueError(msg)

    last_error: httpx.HTTPError | None = None
    for attempt in range(retry_attempts):
        try:
            with httpx.Client(timeout=timeout, follow_redirects=True, trust_env=False) as client:
                response = client.get(url)
                response.raise_for_status()
            return parse_prefix_lines(response.text.splitlines())
        except httpx.HTTPError as exc:
            last_error = exc
            if attempt + 1 >= retry_attempts:
                break
            sleep(retry_base_delay * (2**attempt))

    if last_error is not None:
        raise last_error
    msg = "feed fetch failed without an HTTP error"
    raise RuntimeError(msg)
