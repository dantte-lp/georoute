from collections.abc import Iterable
from dataclasses import dataclass
from ipaddress import IPv4Network, ip_network

from .config import RunnerConfig


class PrefixValidationError(ValueError):
    """Raised when an input feed contains an unsafe prefix."""


@dataclass(frozen=True, slots=True)
class PrefixSet:
    prefixes: tuple[IPv4Network, ...]


def normalize_prefixes(raw_prefixes: Iterable[str | IPv4Network]) -> PrefixSet:
    prefixes: set[IPv4Network] = set()
    for raw_prefix in raw_prefixes:
        network = ip_network(raw_prefix, strict=True)
        if network.version != 4:
            continue
        prefixes.add(network)

    return PrefixSet(tuple(sorted(prefixes, key=lambda item: int(item.network_address))))


def validate_prefixes(prefix_set: PrefixSet, config: RunnerConfig) -> None:
    if len(prefix_set.prefixes) > config.max_prefixes:
        msg = f"prefix count {len(prefix_set.prefixes)} exceeds max_prefixes={config.max_prefixes}"
        raise PrefixValidationError(msg)

    for prefix in prefix_set.prefixes:
        for forbidden in config.forbidden_networks:
            if _is_forbidden(prefix, forbidden):
                msg = f"forbidden prefix {prefix} is inside {forbidden}"
                raise PrefixValidationError(msg)


def _is_forbidden(prefix: IPv4Network, forbidden: IPv4Network) -> bool:
    if forbidden.prefixlen == 0:
        return prefix == forbidden
    return prefix.subnet_of(forbidden)
