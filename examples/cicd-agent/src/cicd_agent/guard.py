"""Puts the gate in front of whatever executes an action. A host calls `check` before it runs a tool, a request handler or a worker's step; the agent
hook and the WSGI middleware here are two such hosts."""
from typing import Callable, Mapping

from .actions import ACTIONS, Action
from .gate import Decision, Gate


class Guard:
    def __init__(self, gate: Gate, actions: Mapping[str, Callable[[Mapping], Action]] = ACTIONS):
        self._gate = gate
        self._actions = actions

    def check(self, name: str, args: Mapping) -> Decision:
        """Asks Attesta whether the named action with these arguments may happen. A name or arguments the guard does not know is a denial."""
        make = self._actions.get(name)
        if make is None:
            return Decision(False, "Deny", f"{name} is not an action of this agent")
        try:
            action = make(args)
        except (KeyError, TypeError) as error:
            return Decision(False, "Deny", f"{name} lacks {error}")
        return self._gate.authorize(action.resource, mandate=action.mandate, action=dict(action.detail))
