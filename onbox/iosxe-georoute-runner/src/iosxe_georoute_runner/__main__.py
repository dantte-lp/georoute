import argparse

from .config import RunnerConfig
from .feed import parse_prefix_lines
from .render import candidate_sha256, render_candidate


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("prefix_file")
    args = parser.parse_args()

    with open(args.prefix_file, encoding="utf-8") as fh:
        prefix_set = parse_prefix_lines(fh)
    candidate = render_candidate(prefix_set, RunnerConfig())
    print(candidate, end="")
    print(f"! sha256 {candidate_sha256(candidate)}")


if __name__ == "__main__":
    main()
