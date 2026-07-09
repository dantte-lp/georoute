from collections.abc import Iterable
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from typing import ClassVar

import pytest

import iosxe_georoute_runner.__main__ as cli


def test_default_feed_url_uses_ripe_country_resource_list() -> None:
    assert (
        cli.default_feed_url("ru")
        == "https://stat.ripe.net/data/country-resource-list/data.json?resource=RU&v4_format=prefix"
    )


def test_load_prefix_set_prefers_existing_file_over_feed_url(tmp_path: Path) -> None:
    prefix_file = tmp_path / "prefixes.txt"
    prefix_file.write_text("2.60.0.0/14\n", encoding="utf-8")

    prefix_set = cli.load_prefix_set(
        prefix_file=str(prefix_file),
        feed_url="http://127.0.0.1:9/feed",
    )

    assert [str(prefix) for prefix in prefix_set.prefixes] == ["2.60.0.0/14"]


def test_load_prefix_set_fetches_feed_when_file_is_not_configured() -> None:
    with feed_server(["217.199.254.0/24\n"]) as url:
        prefix_set = cli.load_prefix_set(prefix_file=None, feed_url=url)

    assert [str(prefix) for prefix in prefix_set.prefixes] == ["217.199.254.0/24"]


def test_load_prefix_set_requires_file_or_feed_url() -> None:
    with pytest.raises(ValueError, match="prefix source"):
        cli.load_prefix_set(prefix_file=None, feed_url=None)


class feed_server:
    def __init__(self, bodies: Iterable[str]) -> None:
        self._bodies = list(bodies)
        self._server: ThreadingHTTPServer | None = None
        self._thread: Thread | None = None

    def __enter__(self) -> str:
        FeedHandler.bodies = self._bodies
        self._server = ThreadingHTTPServer(("127.0.0.1", 0), FeedHandler)
        self._thread = Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        host, port = self._server.server_address[0], self._server.server_address[1]
        return f"http://{host}:{port}/feed.txt"

    def __exit__(self, *args: object) -> None:
        if self._server is not None:
            self._server.shutdown()
        if self._thread is not None:
            self._thread.join(timeout=2.0)


class FeedHandler(BaseHTTPRequestHandler):
    bodies: ClassVar[list[str]] = []

    def do_GET(self) -> None:
        payload = self.bodies.pop(0).encode("utf-8")
        self.send_response(HTTPStatus.OK)
        self.send_header("content-type", "text/plain")
        self.send_header("content-length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format: str, *args: object) -> None:
        return
