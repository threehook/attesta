"""The gate: asks Attesta whether an action may happen, and lets it happen only on Permit."""
import time
from dataclasses import dataclass
from typing import Callable

from .identity import WorkloadIdentity
from .jsonhttp import HttpError, call

IDENTITY_CLAIM = "workload"
WORKLOAD = {"credentialType": "Workload", "claims": ["owner", "environment"]}
MANDATE = {"credentialType": "Mandate", "claims": ["resource", "approvers"]}


@dataclass(frozen=True)
class Decision:
    permit: bool
    decision: str
    reason: str
    subject: str | None = None


class Gate:
    """Anything that goes wrong is a denial, with the reason."""

    def __init__(
        self,
        attesta_url: str,
        wallet_url: str,
        identity: WorkloadIdentity,
        policy_id: str = "cicd",
        wait: float = 30,
        sleep: Callable[[float], None] = time.sleep,
    ):
        self._attesta = attesta_url.rstrip("/")
        self._wallet = wallet_url.rstrip("/")
        self._identity = identity
        self._policy = policy_id
        self._wait = wait
        self._sleep = sleep

    def authorize(self, resource: str, mandate: bool = False, action: dict[str, str] | None = None) -> Decision:
        """Asks whether the action on the resource may happen. With `mandate` the wallet also shows what people allowed; `action` tells the policy what
        is about to be done, as texts by name."""
        try:
            self._identity.ensure()
            for attempt in (1, 2):
                started = call(
                    "POST",
                    f"{self._attesta}/v1/authorize/requests",
                    {
                        "resource": resource,
                        "policyId": self._policy,
                        "identityClaim": IDENTITY_CLAIM,
                        "credentials": [WORKLOAD, MANDATE] if mandate else [WORKLOAD],
                        **({"action": action} if action else {}),
                    },
                )
                try:
                    answered = call("POST", f"{self._wallet}/present", {"link": started["authorizationRequest"]})
                except HttpError as error:
                    if error.status != 409 or attempt == 2:
                        raise
                    # The wallet holds no credential for the request, a Mandate approved after it last asked the issuer for example.
                    self._identity.renew()
                    continue
                if answered.get("status") == 200 or attempt == 2:
                    break
                # Attesta did not accept the credential, a revoked one for example. The issuer gives a new one if the workload is still enrolled.
                self._identity.renew()
            deadline = time.monotonic() + self._wait
            while True:
                outcome = call("GET", f"{self._attesta}/v1/authorize/requests/{started['requestId']}")
                if outcome.get("status") != "pending":
                    break
                if time.monotonic() >= deadline:
                    return Decision(False, "Indeterminate", "Attesta did not answer in time")
                self._sleep(0.25)
            subject = (outcome.get("subject") or {}).get("id")
            return Decision(outcome.get("decision") == "Permit", str(outcome.get("decision")), str(outcome.get("reason", "")), subject)
        except HttpError as error:
            if error.status == 409:  # the wallet holds nothing that answers the request, such as a Mandate nobody approved
                return Decision(False, "Deny", "the wallet holds no credential for this request: " + ("a mandate is needed" if mandate else error.body))
            return Decision(False, "Indeterminate", f"{type(error).__name__}: {error}")
        except Exception as error:  # an action that cannot be authorized does not happen
            return Decision(False, "Indeterminate", f"{type(error).__name__}: {error}")
