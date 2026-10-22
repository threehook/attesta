"""Small JSON calls over HTTP with the standard library."""
import json
import urllib.error
import urllib.request


class HttpError(Exception):
    """The server answered with an error status; `status` and `body` say which."""

    def __init__(self, status: int, body: str):
        super().__init__(f"HTTP {status}: {body}")
        self.status = status
        self.body = body


def call(method: str, url: str, body: dict | None = None, token: str | None = None, timeout: float = 30) -> dict:
    headers = {"content-type": "application/json"}
    if token:
        headers["authorization"] = f"Bearer {token}"
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as error:
        raise HttpError(error.code, error.read().decode(errors="replace")) from None
