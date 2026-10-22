"""The gate as WSGI middleware: a service asks Attesta before it handles a request, and answers 403 with the reason when the answer is not Permit."""
import json
from typing import Callable, Mapping

from .guard import Guard


def gated(app, guard: Guard, action_of: Callable[[Mapping], tuple[str, Mapping] | None]):
    """Wraps a WSGI app. `action_of(environ)` names the action a request stands for and its arguments, or gives None for a request that needs no
    decision, such as a health probe."""

    def wrapper(environ, start_response):
        wanted = action_of(environ)
        if wanted is None:
            return app(environ, start_response)
        decision = guard.check(*wanted)
        if decision.permit:
            return app(environ, start_response)
        body = json.dumps({"error": "refused", "decision": decision.decision, "reason": decision.reason}).encode()
        start_response("403 Forbidden", [("content-type", "application/json"), ("content-length", str(len(body)))])
        return [body]

    return wrapper
