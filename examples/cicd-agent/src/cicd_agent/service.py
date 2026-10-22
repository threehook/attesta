"""The agent as a service: POST /tasks gives it a task, POST /check asks the gate about a resource without the model."""
import asyncio
import hmac
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from .gate import Gate
from .guard import Guard
from .tools import Runner, run_task


def make_handler(gate: Gate, runner: Runner, token: str | None, model_available: bool):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):  # the runner and the gate log what matters
            pass

        def _send(self, status: int, body: dict) -> None:
            data = json.dumps(body).encode()
            self.send_response(status)
            self.send_header("content-type", "application/json")
            self.send_header("content-length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def _authorized(self) -> bool:
            if not token:
                return True
            given = self.headers.get("authorization", "").removeprefix("Bearer ")
            return hmac.compare_digest(given, token)

        def _body(self) -> dict:
            length = min(int(self.headers.get("content-length") or 0), 1 << 16)
            return json.loads(self.rfile.read(length) or b"{}")

        def do_GET(self):
            if self.path == "/healthz":
                return self._send(200, {"ok": True})
            self._send(404, {"error": "not found"})

        def do_POST(self):
            if self.path not in ("/check", "/tasks"):
                return self._send(404, {"error": "not found"})
            if not self._authorized():
                return self._send(401, {"error": "a token is required"})
            try:
                body = self._body()
            except (ValueError, TypeError):
                return self._send(400, {"error": "the request is not JSON"})
            if self.path == "/check":
                resource = body.get("resource")
                if not isinstance(resource, str) or not resource:
                    return self._send(400, {"error": "resource is required"})
                action = body.get("action")
                decision = gate.authorize(resource, mandate=body.get("mandate") is True, action=action if isinstance(action, dict) else None)
                return self._send(200, {"permit": decision.permit, "decision": decision.decision, "reason": decision.reason, "subject": decision.subject})
            task = body.get("task")
            if not isinstance(task, str) or not task:
                return self._send(400, {"error": "task is required"})
            if not model_available:
                return self._send(503, {"error": "no model is configured: set ANTHROPIC_API_KEY"})
            self._send(200, {"result": asyncio.run(run_task(task, Guard(gate), runner))})

    return Handler


def serve(gate: Gate, runner: Runner, port: int = 8090) -> None:
    handler = make_handler(gate, runner, os.environ.get("CICD_AGENT_TOKEN"), bool(os.environ.get("ANTHROPIC_API_KEY")))
    ThreadingHTTPServer(("0.0.0.0", port), handler).serve_forever()
