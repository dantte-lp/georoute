import pytest

from iosxe_georoute_runner.apply import (
    ApplyPlan,
    build_copy_command,
    build_eem_applet,
    build_postcheck_commands,
    build_rollback_command,
    build_syntax_check_command,
    should_apply,
)


def test_build_copy_command_uses_running_config_merge_target() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
    )

    assert (
        build_copy_command(plan)
        == "copy http://192.0.2.10:8080/georoute/ru/current.cfg running-config"
    )


def test_build_syntax_check_command_uses_candidate_url() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
    )

    assert (
        build_syntax_check_command(plan)
        == "configure check syntax http://192.0.2.10:8080/georoute/ru/current.cfg"
    )


def test_build_eem_applet_runs_syntax_check_before_copy() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
    )

    applet = build_eem_applet(plan)

    assert "event manager applet GEOROUTE_APPLY" in applet
    assert (
        'action 0010 cli command "configure check syntax '
        'http://192.0.2.10:8080/georoute/ru/current.cfg"'
    ) in applet
    assert (
        'action 0020 cli command "copy '
        'http://192.0.2.10:8080/georoute/ru/current.cfg running-config"'
    ) in applet


def test_build_postcheck_commands_include_sample_route_when_present() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
        sample_prefix="2.60.0.0",
    )

    assert build_postcheck_commands(plan) == (
        "show ip prefix-list PL-GEOROUTE-RU-V4",
        "show running-config | include ^ip route .*GEOROUTE_RU",
        "show ip route vrf VRF-TRANSIT 2.60.0.0",
        "show bgp vpnv4 unicast all | include 2.60.0.0",
    )


def test_should_apply_compares_candidate_hash() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
    )

    assert not should_apply("abc123", plan)
    assert should_apply("other", plan)
    assert should_apply(None, plan)


def test_build_rollback_command_requires_previous_candidate_url() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
    )

    with pytest.raises(ValueError):
        build_rollback_command(plan)


def test_build_rollback_command_uses_previous_candidate_url() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
        previous_candidate_url="http://192.0.2.10:8080/georoute/ru/previous.cfg",
    )

    assert (
        build_rollback_command(plan)
        == "copy http://192.0.2.10:8080/georoute/ru/previous.cfg running-config"
    )
