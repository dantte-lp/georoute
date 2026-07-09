from datetime import UTC, datetime, timedelta
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from signal import SIGTERM, signal

from .config import RunnerConfig
from .render import candidate_sha256, render_candidate
from .state import CandidateState
from .validate import PrefixSet


def create_candidate_handler(state: CandidateState) -> type[BaseHTTPRequestHandler]:
    class CandidateHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            if self.path == "/healthz":
                self._write_text(HTTPStatus.OK, "ok\n")
                return
            if self.path == "/readyz":
                status = HTTPStatus.OK if state.ready() else HTTPStatus.SERVICE_UNAVAILABLE
                self._write_text(status, "ready\n" if state.ready() else "not-ready\n")
                return
            if self.path == "/georoute/ru/current.cfg":
                self._write_text(HTTPStatus.OK, state.candidate)
                return
            if self.path == "/georoute/ru/current.sha256":
                self._write_text(HTTPStatus.OK, f"{state.digest}\n")
                return
            self._write_text(HTTPStatus.NOT_FOUND, "not-found\n")

        def log_message(self, format: str, *args: object) -> None:
            return

        def _write_text(self, status: HTTPStatus, body: str) -> None:
            payload = body.encode("utf-8")
            self.send_response(status.value)
            self.send_header("content-type", "text/plain; charset=utf-8")
            self.send_header("content-length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

    return CandidateHandler


def serve(prefix_set: PrefixSet, config: RunnerConfig, host: str, port: int) -> None:
    candidate = render_candidate(prefix_set, config)
    state = CandidateState(
        candidate=candidate,
        digest=candidate_sha256(candidate),
        generated_at=datetime.now(UTC),
        stale_after=timedelta(minutes=30),
    )
    server = ThreadingHTTPServer((host, port), create_candidate_handler(state))
    previous_handler = signal(SIGTERM, _raise_keyboard_interrupt)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        return
    finally:
        server.server_close()
        signal(SIGTERM, previous_handler)


def _raise_keyboard_interrupt(signum: int, frame: object) -> None:
    raise KeyboardInterrupt
