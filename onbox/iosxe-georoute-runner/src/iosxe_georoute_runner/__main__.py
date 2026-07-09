import argparse
import os

from .config import RunnerConfig
from .feed import parse_prefix_lines
from .render import candidate_sha256, render_candidate
from .server import serve


def main() -> None:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command")

    render_parser = subparsers.add_parser("render")
    render_parser.add_argument(
        "--prefix-file",
        default=os.getenv("GEOROUTE_PREFIX_FILE", "/data/prefixes.txt"),
    )

    serve_parser = subparsers.add_parser("serve")
    serve_parser.add_argument(
        "--prefix-file",
        default=os.getenv("GEOROUTE_PREFIX_FILE", "/data/prefixes.txt"),
    )
    serve_parser.add_argument("--host", default=os.getenv("GEOROUTE_HTTP_HOST", "0.0.0.0"))
    serve_parser.add_argument(
        "--port",
        type=int,
        default=int(os.getenv("GEOROUTE_HTTP_PORT", "8080")),
    )

    args = parser.parse_args()

    if args.command is None:
        args = parser.parse_args(["serve"])

    with open(args.prefix_file, encoding="utf-8") as fh:
        prefix_set = parse_prefix_lines(fh)
    config = RunnerConfig()

    if args.command == "serve":
        serve(prefix_set, config, args.host, args.port)
        return

    candidate = render_candidate(prefix_set, config)
    print(candidate, end="")
    print(f"! sha256 {candidate_sha256(candidate)}")


if __name__ == "__main__":
    main()
