from ipaddress import IPv4Address, IPv4Network

from iosxe_georoute_runner.config import RunnerConfig
from iosxe_georoute_runner.render import candidate_sha256, render_candidate
from iosxe_georoute_runner.validate import normalize_prefixes


def test_render_is_deterministic_and_sorted() -> None:
    config = RunnerConfig(next_hop=IPv4Address("203.0.113.1"))
    prefix_set = normalize_prefixes(["217.199.254.0/24", "2.60.0.0/14", "2.60.0.0/14"])

    candidate = render_candidate(prefix_set, config)

    assert "seq 10 permit 2.60.0.0/14" in candidate
    assert "seq 20 permit 217.199.254.0/24" in candidate
    assert candidate.count("2.60.0.0/14") == 1
    assert "route-map RM-GEOROUTE-RU-STATIC permit 10\n match tag 201" in candidate
    assert candidate_sha256(candidate) == candidate_sha256(candidate)


def test_render_vrf_direct_static_routes() -> None:
    config = RunnerConfig(next_hop=IPv4Address("203.0.113.1"), route_mode="vrf-direct")
    prefix_set = normalize_prefixes(["2.60.0.0/14"])

    candidate = render_candidate(prefix_set, config)

    assert (
        "ip route vrf VRF-TRANSIT 2.60.0.0 255.252.0.0 203.0.113.1 global tag 201 name GEOROUTE_RU"
    ) in candidate
    assert "router bgp 4200099722\n address-family ipv4 vrf VRF-TRANSIT" in candidate
    assert "  redistribute static route-map RM-GEOROUTE-RU-STATIC" in candidate


def test_render_global_replicated_static_routes() -> None:
    config = RunnerConfig(
        next_hop=IPv4Address("203.0.113.1"),
        route_mode="global-replicated",
    )
    prefix_set = normalize_prefixes([IPv4Network("217.199.254.0/24")])

    candidate = render_candidate(prefix_set, config)

    assert "ip route 217.199.254.0 255.255.255.0 203.0.113.1 tag 201 name GEOROUTE_RU" in candidate
