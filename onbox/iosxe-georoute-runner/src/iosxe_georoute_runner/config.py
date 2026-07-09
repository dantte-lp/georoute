from ipaddress import IPv4Address, IPv4Network
from typing import Literal

from pydantic import BaseModel, Field

RouteMode = Literal["global-replicated", "vrf-direct"]


class RunnerConfig(BaseModel):
    country_code: str = Field(default="RU", min_length=2, max_length=2)
    vrf_name: str = "VRF-TRANSIT"
    prefix_list_name: str = "PL-GEOROUTE-RU-V4"
    global_to_vrf_route_map: str = "RM-GLOBAL-TO-TRANSIT"
    outbound_route_map: str = "RM-RU-GEOROUTE-OUT"
    static_route_map: str = "RM-GEOROUTE-RU-STATIC"
    static_route_name: str = "GEOROUTE_RU"
    route_tag: int = 201
    next_hop: IPv4Address = IPv4Address("94.139.250.25")
    route_mode: RouteMode = "vrf-direct"
    max_prefixes: int = 20_000
    forbidden_networks: tuple[IPv4Network, ...] = (
        IPv4Network("0.0.0.0/0"),
        IPv4Network("10.0.0.0/8"),
        IPv4Network("172.16.0.0/12"),
        IPv4Network("192.168.0.0/16"),
        IPv4Network("100.64.0.0/10"),
        IPv4Network("10.64.0.0/15"),
    )
