from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread

from iosxe_georoute_runner.feed import fetch_text_feed, parse_prefix_lines


def test_parse_prefix_lines_ignores_comments_and_metadata() -> None:
    prefix_set = parse_prefix_lines(["# comment", "", "2.60.0.0/14 AS123", "217.199.254.0/24"])

    assert [str(prefix) for prefix in prefix_set.prefixes] == ["2.60.0.0/14", "217.199.254.0/24"]


def test_fetch_text_feed_retries_transient_http_error() -> None:
    FlakyFeedHandler.requests = 0
    server = ThreadingHTTPServer(("127.0.0.1", 0), FlakyFeedHandler)
    thread = Thread(target=server.serve_forever, daemon=True)

    try:
        thread.start()
        host, port = server.server_address[0], server.server_address[1]
        prefix_set = fetch_text_feed(
            f"http://{host}:{port}/feed.txt",
            timeout=2.0,
            retry_attempts=2,
            retry_base_delay=0.0,
        )
    finally:
        server.shutdown()
        thread.join(timeout=2.0)

    assert [str(prefix) for prefix in prefix_set.prefixes] == ["2.60.0.0/14"]
    assert FlakyFeedHandler.requests == 2


def test_fetch_text_feed_accepts_ripe_country_resource_json() -> None:
    RipeJsonFeedHandler.requests = 0
    server = ThreadingHTTPServer(("127.0.0.1", 0), RipeJsonFeedHandler)
    thread = Thread(target=server.serve_forever, daemon=True)

    try:
        thread.start()
        host, port = server.server_address[0], server.server_address[1]
        prefix_set = fetch_text_feed(
            f"http://{host}:{port}/data.json?resource=RU&v4_format=prefix",
            timeout=2.0,
            retry_attempts=1,
            retry_base_delay=0.0,
        )
    finally:
        server.shutdown()
        thread.join(timeout=2.0)

    assert [str(prefix) for prefix in prefix_set.prefixes] == ["2.60.0.0/14", "217.199.254.0/24"]


class FlakyFeedHandler(BaseHTTPRequestHandler):
    requests = 0

    def do_GET(self) -> None:
        type(self).requests += 1
        if type(self).requests == 1:
            self.send_response(HTTPStatus.SERVICE_UNAVAILABLE)
            self.end_headers()
            return

        payload = b"2.60.0.0/14\n"
        self.send_response(HTTPStatus.OK)
        self.send_header("content-type", "text/plain")
        self.send_header("content-length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format: str, *args: object) -> None:
        return


class RipeJsonFeedHandler(BaseHTTPRequestHandler):
    requests = 0

    def do_GET(self) -> None:
        type(self).requests += 1
        payload = b'{"data":{"resources":{"ipv4":["2.60.0.0/14","217.199.254.0/24"],"ipv6":[]}}}'
        self.send_response(HTTPStatus.OK)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format: str, *args: object) -> None:
        return
