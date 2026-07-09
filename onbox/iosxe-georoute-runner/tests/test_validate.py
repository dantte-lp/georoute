import pytest

from iosxe_georoute_runner.config import RunnerConfig
from iosxe_georoute_runner.validate import (
    PrefixValidationError,
    normalize_prefixes,
    validate_prefixes,
)


@pytest.mark.parametrize(
    "prefix",
    [
        "0.0.0.0/0",
        "10.0.0.0/8",
        "10.64.0.0/15",
        "100.64.0.0/10",
        "172.16.0.0/12",
        "192.168.0.0/16",
    ],
)
def test_forbidden_prefixes_are_rejected(prefix: str) -> None:
    with pytest.raises(PrefixValidationError):
        validate_prefixes(normalize_prefixes([prefix]), RunnerConfig())


def test_max_prefix_count_is_enforced() -> None:
    prefix_set = normalize_prefixes(["2.60.0.0/14", "217.199.254.0/24"])

    with pytest.raises(PrefixValidationError):
        validate_prefixes(prefix_set, RunnerConfig(max_prefixes=1))
