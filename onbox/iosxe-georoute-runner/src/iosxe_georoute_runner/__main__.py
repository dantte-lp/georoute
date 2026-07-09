import argparse
import os
from pathlib import Path

from .config import RunnerConfig
from .feed import fetch_text_feed, parse_prefix_lines
from .render import candidate_sha256, render_candidate
from .server import serve
from .validate import PrefixSet


def load_prefix_set(prefix_file: str | None, feed_url: str | None) -> PrefixSet:
    if prefix_file is not None and Path(prefix_file).is_file():
        with open(prefix_file, encoding="utf-8") as fh:
            return parse_prefix_lines(fh)
    if feed_url is not None:
        return fetch_text_feed(feed_url)
    msg = "prefix source is required: configure --prefix-file or --feed-url"
    raise ValueError(msg)


def main() -> None:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command")

    render_parser = subparsers.add_parser("render")
    render_parser.add_argument(
        "--prefix-file",
        default=os.getenv("GEOROUTE_PREFIX_FILE", "/data/prefixes.txt"),
    )
    render_parser.add_argument("--feed-url", default=os.getenv("GEOROUTE_FEED_URL"))

    serve_parser = subparsers.add_parser("serve")
    serve_parser.add_argument(
        "--prefix-file",
        default=os.getenv("GEOROUTE_PREFIX_FILE", "/data/prefixes.txt"),
    )
    serve_parser.add_argument("--feed-url", default=os.getenv("GEOROUTE_FEED_URL"))
    serve_parser.add_argument("--host", default=os.getenv("GEOROUTE_HTTP_HOST", "0.0.0.0"))
    serve_parser.add_argument(
        "--port",
        type=int,
        default=int(os.getenv("GEOROUTE_HTTP_PORT", "8080")),
    )

    args = parser.parse_args()

    if args.command is None:
        args = parser.parse_args(["serve"])

    prefix_set = load_prefix_set(args.prefix_file, args.feed_url)
    config = RunnerConfig()

    if args.command == "serve":
        serve(prefix_set, config, args.host, args.port)
        return

    candidate = render_candidate(prefix_set, config)
    print(candidate, end="")
    print(f"! sha256 {candidate_sha256(candidate)}")


if __name__ == "__main__":
    main()
