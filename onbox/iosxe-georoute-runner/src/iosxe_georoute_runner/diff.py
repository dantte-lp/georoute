import re
from dataclasses import dataclass
from ipaddress import IPv4Address, IPv4Network, ip_network

from .config import RunnerConfig
from .render import render_candidate
from .validate import PrefixSet


@dataclass(frozen=True, slots=True)
class StaticRoute:
    prefix: IPv4Network
    next_hop: IPv4Address
    vrf_name: str | None
    global_next_hop: bool


@dataclass(frozen=True, slots=True)
class StaticRouteDiff:
    add: tuple[StaticRoute, ...]
    remove: tuple[StaticRoute, ...]
    unchanged: tuple[StaticRoute, ...]


_PREFIX_LIST_RE = re.compile(
    r"^ip prefix-list (?P<name>\S+) seq \d+ permit (?P<prefix>\d+\.\d+\.\d+\.\d+/\d+)$"
)
_VRF_STATIC_RE = re.compile(
    r"^ip route vrf (?P<vrf>\S+) (?P<network>\d+\.\d+\.\d+\.\d+) "
    r"(?P<mask>\d+\.\d+\.\d+\.\d+) (?P<next_hop>\d+\.\d+\.\d+\.\d+) "
    r"(?P<attrs>.*?) tag (?P<tag>\d+) name (?P<name>\S+)"
)
_GLOBAL_STATIC_RE = re.compile(
    r"^ip route (?P<network>\d+\.\d+\.\d+\.\d+) (?P<mask>\d+\.\d+\.\d+\.\d+) "
    r"(?P<next_hop>\d+\.\d+\.\d+\.\d+) (?P<attrs>.*?) tag (?P<tag>\d+) name (?P<name>\S+)"
)


def parse_prefix_list_entries(running_config: str, prefix_list_name: str) -> PrefixSet:
    prefixes: set[IPv4Network] = set()
    for raw_line in running_config.splitlines():
        line = raw_line.strip()
        match = _PREFIX_LIST_RE.match(line)
        if match is None or match.group("name") != prefix_list_name:
            continue
        network = ip_network(match.group("prefix"), strict=True)
        if isinstance(network, IPv4Network):
            prefixes.add(network)
    return PrefixSet(tuple(sorted(prefixes, key=lambda item: int(item.network_address))))


def parse_static_routes(running_config: str, config: RunnerConfig) -> tuple[StaticRoute, ...]:
    routes: list[StaticRoute] = []
    for raw_line in running_config.splitlines():
        line = raw_line.strip()
        match = _VRF_STATIC_RE.match(line)
        if match is not None:
            route = _route_from_match(match, config, vrf_name=match.group("vrf"))
            if route is not None:
                routes.append(route)
            continue

        match = _GLOBAL_STATIC_RE.match(line)
        if match is not None:
            route = _route_from_match(match, config, vrf_name=None)
            if route is not None:
                routes.append(route)

    return tuple(sorted(routes, key=lambda item: int(item.prefix.network_address)))


def diff_static_routes(
    current: tuple[StaticRoute, ...],
    desired: PrefixSet,
    config: RunnerConfig,
) -> StaticRouteDiff:
    current_by_prefix = {route.prefix: route for route in current}
    desired_by_prefix = {
        prefix: StaticRoute(
            prefix=prefix,
            next_hop=config.next_hop,
            vrf_name=config.vrf_name if config.route_mode == "vrf-direct" else None,
            global_next_hop=config.route_mode == "vrf-direct",
        )
        for prefix in desired.prefixes
    }

    add = tuple(
        desired_by_prefix[prefix]
        for prefix in sorted(desired_by_prefix.keys() - current_by_prefix.keys())
    )
    remove = tuple(
        current_by_prefix[prefix]
        for prefix in sorted(current_by_prefix.keys() - desired_by_prefix.keys())
    )
    unchanged = tuple(
        current_by_prefix[prefix]
        for prefix in sorted(current_by_prefix.keys() & desired_by_prefix.keys())
    )

    return StaticRouteDiff(add=add, remove=remove, unchanged=unchanged)


def render_cleanup_candidate(
    running_config: str,
    desired: PrefixSet,
    config: RunnerConfig,
) -> str:
    current = parse_static_routes(running_config, config)
    plan = diff_static_routes(current, desired, config)
    cleanup_lines = [render_no_static_route(route) for route in plan.remove]
    full_candidate = render_candidate(desired, config)

    if not cleanup_lines:
        return full_candidate
    return "\n".join(cleanup_lines) + "\n" + full_candidate


def render_no_static_route(route: StaticRoute) -> str:
    parts = ["no ip route"]
    if route.vrf_name is not None:
        parts.extend(["vrf", route.vrf_name])
    parts.extend(
        [str(route.prefix.network_address), str(route.prefix.netmask), str(route.next_hop)]
    )
    if route.global_next_hop:
        parts.append("global")
    return " ".join(parts)


def _route_from_match(
    match: re.Match[str],
    config: RunnerConfig,
    vrf_name: str | None,
) -> StaticRoute | None:
    if int(match.group("tag")) != config.route_tag:
        return None
    if match.group("name") != config.static_route_name:
        return None

    network = ip_network(f"{match.group('network')}/{match.group('mask')}", strict=True)
    if not isinstance(network, IPv4Network):
        return None
    return StaticRoute(
        prefix=network,
        next_hop=IPv4Address(match.group("next_hop")),
        vrf_name=vrf_name,
        global_next_hop="global" in match.group("attrs").split(),
    )
