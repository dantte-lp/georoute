from ipaddress import IPv4Address

from iosxe_georoute_runner.config import RunnerConfig
from iosxe_georoute_runner.diff import (
    diff_static_routes,
    parse_prefix_list_entries,
    parse_static_routes,
    render_cleanup_candidate,
)
from iosxe_georoute_runner.validate import normalize_prefixes

RUNNING_CONFIG = """
ip prefix-list PL-GEOROUTE-RU-V4 seq 10 permit 2.60.0.0/14
ip prefix-list PL-GEOROUTE-RU-V4 seq 20 permit 217.199.254.0/24
ip route vrf VRF-TRANSIT 2.60.0.0 255.252.0.0 203.0.113.1 global tag 201 name GEOROUTE_RU
ip route vrf VRF-TRANSIT 198.51.100.0 255.255.255.0 203.0.113.1 global tag 201 name GEOROUTE_RU
ip route 192.0.2.0 255.255.255.0 203.0.113.1 tag 999 name OTHER
"""


def test_parse_prefix_list_entries_filters_by_name() -> None:
    entries = parse_prefix_list_entries(RUNNING_CONFIG, "PL-GEOROUTE-RU-V4")

    assert [str(prefix) for prefix in entries.prefixes] == ["2.60.0.0/14", "217.199.254.0/24"]


def test_parse_static_routes_filters_by_tag_and_name() -> None:
    config = RunnerConfig(next_hop=IPv4Address("203.0.113.1"))

    routes = parse_static_routes(RUNNING_CONFIG, config)

    assert [str(route.prefix) for route in routes] == ["2.60.0.0/14", "198.51.100.0/24"]


def test_diff_static_routes_detects_add_remove_unchanged() -> None:
    config = RunnerConfig(next_hop=IPv4Address("203.0.113.1"))
    current = parse_static_routes(RUNNING_CONFIG, config)
    desired = normalize_prefixes(["2.60.0.0/14", "217.199.254.0/24"])

    plan = diff_static_routes(current, desired, config)

    assert [str(route.prefix) for route in plan.add] == ["217.199.254.0/24"]
    assert [str(route.prefix) for route in plan.remove] == ["198.51.100.0/24"]
    assert [str(route.prefix) for route in plan.unchanged] == ["2.60.0.0/14"]


def test_render_cleanup_candidate_removes_stale_static_before_full_candidate() -> None:
    config = RunnerConfig(next_hop=IPv4Address("203.0.113.1"))
    desired = normalize_prefixes(["2.60.0.0/14", "217.199.254.0/24"])

    candidate = render_cleanup_candidate(RUNNING_CONFIG, desired, config)

    assert "no ip route vrf VRF-TRANSIT 198.51.100.0 255.255.255.0 203.0.113.1 global" in candidate
    assert "no ip prefix-list PL-GEOROUTE-RU-V4" in candidate
    assert (
        "ip route vrf VRF-TRANSIT 217.199.254.0 255.255.255.0 203.0.113.1 "
        "global tag 201 name GEOROUTE_RU"
    ) in candidate
