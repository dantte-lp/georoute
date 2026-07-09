from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from .config import RunnerConfig
from .render import candidate_sha256, render_candidate
from .validate import PrefixSet


class CandidateHandler(BaseHTTPRequestHandler):
    candidate = ""
    digest = ""

    def do_GET(self) -> None:
        if self.path == "/healthz":
            self._write_text(HTTPStatus.OK, "ok\n")
            return
        if self.path == "/readyz":
            status = HTTPStatus.OK if self.candidate else HTTPStatus.SERVICE_UNAVAILABLE
            self._write_text(status, "ready\n" if self.candidate else "not-ready\n")
            return
        if self.path == "/georoute/ru/current.cfg":
            self._write_text(HTTPStatus.OK, self.candidate)
            return
        if self.path == "/georoute/ru/current.sha256":
            self._write_text(HTTPStatus.OK, f"{self.digest}\n")
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


def serve(prefix_set: PrefixSet, config: RunnerConfig, host: str, port: int) -> None:
    CandidateHandler.candidate = render_candidate(prefix_set, config)
    CandidateHandler.digest = candidate_sha256(CandidateHandler.candidate)
    server = ThreadingHTTPServer((host, port), CandidateHandler)
    server.serve_forever()
