from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class ApplyPlan:
    candidate_url: str
    candidate_sha256: str
    previous_candidate_url: str | None = None
    applet_name: str = "GEOROUTE_APPLY"
    prefix_list_name: str = "PL-GEOROUTE-RU-V4"
    static_route_name: str = "GEOROUTE_RU"
    sample_prefix: str | None = None
    vrf_name: str = "VRF-TRANSIT"


def build_copy_command(plan: ApplyPlan) -> str:
    return f"copy {plan.candidate_url} running-config"


def build_syntax_check_command(plan: ApplyPlan) -> str:
    return f"configure check syntax {plan.candidate_url}"


def build_rollback_command(plan: ApplyPlan) -> str:
    if plan.previous_candidate_url is None:
        msg = "previous_candidate_url is required for rollback"
        raise ValueError(msg)
    return f"copy {plan.previous_candidate_url} running-config"


def build_eem_applet(plan: ApplyPlan) -> str:
    lines = [
        f"event manager applet {plan.applet_name}",
        " event none maxrun 600",
        f' action 0010 cli command "{build_syntax_check_command(plan)}"',
        f' action 0020 cli command "{build_copy_command(plan)}"',
        f' action 0030 syslog msg "georoute apply candidate {plan.candidate_sha256}"',
    ]
    return "\n".join(lines)


def build_postcheck_commands(plan: ApplyPlan) -> tuple[str, ...]:
    commands = [
        f"show ip prefix-list {plan.prefix_list_name}",
        f"show running-config | include ^ip route .*{plan.static_route_name}",
    ]
    if plan.sample_prefix is not None:
        commands.append(f"show ip route vrf {plan.vrf_name} {plan.sample_prefix}")
        commands.append(f"show bgp vpnv4 unicast all | include {plan.sample_prefix}")
    return tuple(commands)


def should_apply(current_sha256: str | None, plan: ApplyPlan) -> bool:
    return current_sha256 != plan.candidate_sha256
