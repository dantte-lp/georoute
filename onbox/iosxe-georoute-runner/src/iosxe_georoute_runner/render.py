from hashlib import sha256
from ipaddress import IPv4Network

from .config import RunnerConfig
from .validate import PrefixSet, validate_prefixes


def render_candidate(prefix_set: PrefixSet, config: RunnerConfig) -> str:
    validate_prefixes(prefix_set, config)
    parts = [
        render_prefix_list(prefix_set.prefixes, config),
        render_route_maps(config),
        render_static_routes(prefix_set.prefixes, config),
    ]
    return "\n".join(part for part in parts if part).strip() + "\n"


def render_prefix_list(prefixes: tuple[IPv4Network, ...], config: RunnerConfig) -> str:
    lines = [f"no ip prefix-list {config.prefix_list_name}"]
    for index, prefix in enumerate(prefixes, start=1):
        lines.append(f"ip prefix-list {config.prefix_list_name} seq {index * 10} permit {prefix}")
    lines.append(f"ip prefix-list {config.prefix_list_name} seq 999999 deny 0.0.0.0/0 le 32")
    return "\n".join(lines)


def render_route_maps(config: RunnerConfig) -> str:
    return "\n".join(
        [
            f"route-map {config.global_to_vrf_route_map} permit 80",
            f" match ip address prefix-list {config.prefix_list_name}",
            f"route-map {config.outbound_route_map} permit 20",
            f" match ip address prefix-list {config.prefix_list_name}",
            f"route-map {config.static_route_map} permit 10",
            f" match tag {config.route_tag}",
            f"route-map {config.static_route_map} deny 9999",
        ]
    )


def render_static_routes(prefixes: tuple[IPv4Network, ...], config: RunnerConfig) -> str:
    if config.route_mode == "global-replicated":
        return "\n".join(_render_global_static(prefix, config) for prefix in prefixes)
    return "\n".join(_render_vrf_static(prefix, config) for prefix in prefixes)


def candidate_sha256(candidate: str) -> str:
    return sha256(candidate.encode("utf-8")).hexdigest()


def _render_global_static(prefix: IPv4Network, config: RunnerConfig) -> str:
    return (
        f"ip route {prefix.network_address} {prefix.netmask} {config.next_hop} "
        f"tag {config.route_tag} name {config.static_route_name}"
    )


def _render_vrf_static(prefix: IPv4Network, config: RunnerConfig) -> str:
    return (
        f"ip route vrf {config.vrf_name} {prefix.network_address} {prefix.netmask} "
        f"{config.next_hop} global tag {config.route_tag} name {config.static_route_name}"
    )
