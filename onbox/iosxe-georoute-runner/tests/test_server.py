from datetime import UTC, datetime, timedelta
from http.server import ThreadingHTTPServer
from threading import Thread

import httpx

from iosxe_georoute_runner.server import create_candidate_handler
from iosxe_georoute_runner.state import CandidateState


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
