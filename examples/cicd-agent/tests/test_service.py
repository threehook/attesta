import json
import threading
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

import pytest

from cicd_agent.gate import Decision
from cicd_agent.service import make_handler


class FakeGate:
    def authorize(self, resource):
        return Decision(resource == "deploy:staging", "Permit" if resource == "deploy:staging" else "Deny", "why", "spiffe://corp/agent")


@pytest.fixture
def service():
    started = []

    def start(token=None, model=False):
        httpd = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(FakeGate(), object(), token, model))
        threading.Thread(target=httpd.serve_forever, daemon=True).start()
        started.append(httpd)
        return f"http://127.0.0.1:{httpd.server_address[1]}"

    yield start
    for httpd in started:
        httpd.shutdown()
        httpd.server_close()


def post(url, body, token=None):
    request = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST", headers={"authorization": f"Bearer {token}"} if token else {})
    try:
        with urllib.request.urlopen(request) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


def test_check_gives_the_decision(service):
    base = service()
    assert post(f"{base}/check", {"resource": "deploy:staging"}) == (200, {"permit": True, "decision": "Permit", "reason": "why", "subject": "spiffe://corp/agent"})
    assert post(f"{base}/check", {"resource": "deploy:prod"})[1]["permit"] is False
    assert post(f"{base}/check", {})[0] == 400


def test_a_token_is_required_when_one_is_set(service):
    base = service(token="s3cret")
    assert post(f"{base}/check", {"resource": "deploy:staging"})[0] == 401
    assert post(f"{base}/check", {"resource": "deploy:staging"}, token="wrong")[0] == 401
    assert post(f"{base}/check", {"resource": "deploy:staging"}, token="s3cret")[0] == 200


def test_tasks_need_a_model(service):
    base = service()
    assert post(f"{base}/tasks", {"task": "deploy"})[0] == 503
    assert post(f"{base}/tasks", {})[0] == 400


def test_health_and_unknown_paths(service):
    base = service()
    with urllib.request.urlopen(f"{base}/healthz") as response:
        assert response.status == 200
    assert post(f"{base}/nothing", {})[0] == 404
