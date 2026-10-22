import json

from cicd_agent.gate import Decision
from cicd_agent.middleware import gated


class FakeGuard:
    def __init__(self, permit):
        self.permit = permit
        self.asked: list[tuple] = []

    def check(self, name, args):
        self.asked.append((name, dict(args)))
        return Decision(self.permit, "Permit" if self.permit else "Deny", "ok" if self.permit else "Mandaat ontbreekt")


def app(environ, start_response):
    start_response("200 OK", [("content-type", "text/plain")])
    return [b"done"]


def action_of(environ):
    return None if environ["PATH_INFO"] == "/healthz" else ("deploy", {"application": "shop", "version": "v1", "environment": "prod"})


def run(wrapped, path):
    seen = {}

    def start_response(status, headers):
        seen["status"] = status

    body = b"".join(wrapped({"PATH_INFO": path}, start_response))
    return seen["status"], body


def test_a_permitted_request_is_handled():
    guard = FakeGuard(True)
    assert run(gated(app, guard, action_of), "/deploy") == ("200 OK", b"done")
    assert guard.asked == [("deploy", {"application": "shop", "version": "v1", "environment": "prod"})]


def test_a_denied_request_is_not_handled_and_says_why():
    status, body = run(gated(app, FakeGuard(False), action_of), "/deploy")
    assert status == "403 Forbidden" and json.loads(body)["reason"] == "Mandaat ontbreekt"


def test_a_request_that_needs_no_decision_is_not_asked_about():
    guard = FakeGuard(False)
    assert run(gated(app, guard, action_of), "/healthz") == ("200 OK", b"done")
    assert guard.asked == []
