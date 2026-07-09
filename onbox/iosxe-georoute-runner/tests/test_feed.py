from iosxe_georoute_runner.feed import parse_prefix_lines


def test_parse_prefix_lines_ignores_comments_and_metadata() -> None:
    prefix_set = parse_prefix_lines(["# comment", "", "2.60.0.0/14 AS123", "217.199.254.0/24"])

    assert [str(prefix) for prefix in prefix_set.prefixes] == ["2.60.0.0/14", "217.199.254.0/24"]
