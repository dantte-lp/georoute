from iosxe_georoute_runner.apply import ApplyPlan, build_copy_command


def test_build_copy_command_uses_running_config_merge_target() -> None:
    plan = ApplyPlan(
        candidate_url="http://192.0.2.10:8080/georoute/ru/current.cfg",
        candidate_sha256="abc123",
    )

    assert (
        build_copy_command(plan)
        == "copy http://192.0.2.10:8080/georoute/ru/current.cfg running-config"
    )
