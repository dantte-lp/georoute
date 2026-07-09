from datetime import UTC, datetime, timedelta
from http import HTTPStatus
from http.server import ThreadingHTTPServer
from threading import Event, Thread

import httpx

from iosxe_georoute_runner.config import RunnerConfig
from iosxe_georoute_runner.server import create_candidate_handler, serve_with_loader
from iosxe_georoute_runner.state import CandidateState
from iosxe_georoute_runner.validate import normalize_prefixes


def test_candidate_server_endpoints() -> None:
    state = CandidateState(
        candidate="ip prefix-list PL-GEOROUTE-RU-V4 seq 10 permit 2.60.0.0/14\n",
        digest="abc123",
        generated_at=datetime.now(UTC),
        stale_after=timedelta(minutes=5),
    )

    with running_server(state) as base_url, httpx.Client(timeout=2.0, trust_env=False) as client:
        assert client.get(f"{base_url}/healthz").text == "ok\n"
        assert client.get(f"{base_url}/readyz").status_code == 200
        assert "2.60.0.0/14" in client.get(f"{base_url}/georoute/ru/current.cfg").text
        assert client.get(f"{base_url}/georoute/ru/current.sha256").text == "abc123\n"


def test_candidate_server_readiness_is_stale_aware() -> None:
    state = CandidateState(
        candidate="candidate\n",
        digest="abc123",
        generated_at=datetime.now(UTC) - timedelta(hours=2),
        stale_after=timedelta(minutes=5),
    )

    with running_server(state) as base_url, httpx.Client(timeout=2.0, trust_env=False) as client:
        response = client.get(f"{base_url}/readyz")

    assert response.status_code == 503
    assert response.text == "not-ready\n"


def test_candidate_server_rejects_candidate_fetch_when_not_ready() -> None:
    state = CandidateState.empty()

    with running_server(state) as base_url, httpx.Client(timeout=2.0, trust_env=False) as client:
        assert client.get(f"{base_url}/healthz").status_code == HTTPStatus.OK
        assert client.get(f"{base_url}/readyz").status_code == HTTPStatus.SERVICE_UNAVAILABLE
        assert (
            client.get(f"{base_url}/georoute/ru/current.cfg").status_code
            == HTTPStatus.SERVICE_UNAVAILABLE
        )
        assert (
            client.get(f"{base_url}/georoute/ru/current.sha256").status_code
            == HTTPStatus.SERVICE_UNAVAILABLE
        )


def test_serve_with_loader_exposes_health_before_feed_load_completes() -> None:
    feed_allowed = Event()
    started = Event()
    holder: dict[str, ThreadingHTTPServer] = {}

    def load_prefixes():
        feed_allowed.wait(timeout=5.0)
        return normalize_prefixes(["2.60.0.0/14"])

    thread = Thread(
        target=serve_with_loader,
        kwargs={
            "load_prefix_set": load_prefixes,
            "config": RunnerConfig(),
            "host": "127.0.0.1",
            "port": 0,
            "started": started,
            "server_holder": holder,
        },
        daemon=True,
    )
    thread.start()
    assert started.wait(timeout=2.0)
    server = holder["server"]
    host, port = server.server_address[0], server.server_address[1]
    base_url = f"http://{host}:{port}"

    try:
        with httpx.Client(timeout=2.0, trust_env=False) as client:
            assert client.get(f"{base_url}/healthz").status_code == HTTPStatus.OK
            assert client.get(f"{base_url}/readyz").status_code == HTTPStatus.SERVICE_UNAVAILABLE
            feed_allowed.set()
            for _ in range(20):
                response = client.get(f"{base_url}/readyz")
                if response.status_code == HTTPStatus.OK:
                    break
            assert response.status_code == HTTPStatus.OK
            assert "2.60.0.0/14" in client.get(f"{base_url}/georoute/ru/current.cfg").text
    finally:
        server.shutdown()
        thread.join(timeout=2.0)


class running_server:
    def __init__(self, state: CandidateState) -> None:
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), create_candidate_handler(state))
        self.thread = Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> str:
        self.thread.start()
        host = self.server.server_address[0]
        port = self.server.server_address[1]
        return f"http://{host}:{port}"

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        self.server.shutdown()
        self.thread.join(timeout=2.0)
