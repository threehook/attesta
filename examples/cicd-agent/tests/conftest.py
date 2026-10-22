import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest


class FakeServer:
    """A server whose answers the test sets per 'METHOD /path'; it records every request."""

    def __init__(self):
        self.routes: dict[str, tuple[int, dict] | object] = {}
        self.requests: list[dict] = []
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def _serve(self):
                length = int(self.headers.get("content-length") or 0)
                raw = self.rfile.read(length) if length else b""
                outer.requests.append(
                    {"method": self.command, "path": self.path, "body": json.loads(raw) if raw else None, "authorization": self.headers.get("authorization")}
                )
                route = outer.routes.get(f"{self.command} {self.path.split('?')[0]}")
                if callable(route):
                    route = route(outer.requests[-1])
                status, body = route if route else (404, {"error": "no route"})
                data = json.dumps(body).encode()
                self.send_response(status)
                self.send_header("content-type", "application/json")
                self.send_header("content-length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            do_GET = do_POST = _serve

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self._server.server_address[1]}"
        threading.Thread(target=self._server.serve_forever, daemon=True).start()

    def close(self):
        self._server.shutdown()
        self._server.server_close()


@pytest.fixture
def server():
    servers = []

    def make() -> FakeServer:
        servers.append(FakeServer())
        return servers[-1]

    yield make
    for s in servers:
        s.close()
