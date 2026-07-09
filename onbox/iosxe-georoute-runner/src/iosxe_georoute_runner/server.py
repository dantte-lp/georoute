from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from signal import SIGTERM, signal
from threading import Event, Thread, current_thread, main_thread
from types import FrameType
from typing import Any

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
                if not state.ready():
                    self._write_text(HTTPStatus.SERVICE_UNAVAILABLE, "not-ready\n")
                    return
                self._write_text(HTTPStatus.OK, state.candidate)
                return
            if self.path == "/georoute/ru/current.sha256":
                if not state.ready():
                    self._write_text(HTTPStatus.SERVICE_UNAVAILABLE, "not-ready\n")
                    return
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
    previous_handler = _install_sigterm_handler()
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        return
    finally:
        server.server_close()
        _restore_sigterm_handler(previous_handler)


def serve_with_loader(
    load_prefix_set: Callable[[], PrefixSet],
    config: RunnerConfig,
    host: str,
    port: int,
    started: Event | None = None,
    server_holder: dict[str, ThreadingHTTPServer] | None = None,
) -> None:
    state = CandidateState.empty()
    server = ThreadingHTTPServer((host, port), create_candidate_handler(state))
    if server_holder is not None:
        server_holder["server"] = server

    refresh_thread = Thread(
        target=_refresh_state_once,
        args=(state, load_prefix_set, config),
        daemon=True,
    )
    refresh_thread.start()

    previous_handler = _install_sigterm_handler()
    if started is not None:
        started.set()
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        return
    finally:
        server.server_close()
        _restore_sigterm_handler(previous_handler)


def _refresh_state_once(
    state: CandidateState,
    load_prefix_set: Callable[[], PrefixSet],
    config: RunnerConfig,
) -> None:
    try:
        prefix_set = load_prefix_set()
        candidate = render_candidate(prefix_set, config)
        state.update(candidate, candidate_sha256(candidate), datetime.now(UTC))
    except Exception as exc:
        state.mark_error(exc)


type SignalHandler = Callable[[int, FrameType | None], Any] | int | None


def _raise_keyboard_interrupt(signum: int, frame: FrameType | None) -> None:
    raise KeyboardInterrupt


def _install_sigterm_handler() -> SignalHandler:
    if current_thread() is not main_thread():
        return None
    return signal(SIGTERM, _raise_keyboard_interrupt)


def _restore_sigterm_handler(previous_handler: SignalHandler) -> None:
    if previous_handler is None:
        return
    signal(SIGTERM, previous_handler)
