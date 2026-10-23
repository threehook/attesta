"""The gate: asks Attesta whether an action may happen, and lets it happen only on Permit."""
import time
from dataclasses import dataclass
from typing import Callable

from .identity import WorkloadIdentity
from .jsonhttp import HttpError, call


class _NoCredential(Exception):
    """The wallet holds no credential that answers the request."""

IDENTITY_CLAIM = "workload"
WORKLOAD = {"credentialType": "Workload", "claims": ["owner", "environment"]}
MANDATE = {"credentialType": "Mandate", "claims": ["resource", "approvers"]}
APPROVAL = {"credentialType": "Approval", "claims": ["resource", "approvers"]}
# The policy gives this advice with a denial that people can undo.
PARKABLE = "request_approval"


@dataclass(frozen=True)
class Decision:
    permit: bool
    decision: str
    reason: str
    subject: str | None = None
    advice: tuple[str, ...] = ()


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
        approval_wait: float = 0,
        approval_poll: float = 5,
        signing_retries: int = 3,
        signing_wait: float = 1,
    ):
        self._attesta = attesta_url.rstrip("/")
        self._wallet = wallet_url.rstrip("/")
        self._identity = identity
        self._policy = policy_id
        self._wait = wait
        self._sleep = sleep
        self._approval_wait = approval_wait
        self._approval_poll = approval_poll
        self._signing_retries = signing_retries
        self._signing_wait = signing_wait

    def authorize(self, resource: str, mandate: bool = False, action: dict[str, str] | None = None) -> Decision:
        """Asks whether the action on the resource may happen. With `mandate` the wallet also shows what people allowed; `action` tells the policy what
        is about to be done, as texts by name. A denial that people can undo parks the action at the issuer, and waits `approval_wait` seconds for them."""
        try:
            self._identity.ensure()
            try:
                decision = self._ask(resource, [WORKLOAD, MANDATE] if mandate else [WORKLOAD], action)
            except _NoCredential:
                # The wallet holds no mandate. Asking without it lets the policy say what is missing, and whether people can approve it.
                decision = self._ask(resource, [WORKLOAD], action)
            if decision.permit or PARKABLE not in decision.advice:
                return decision
            return self._park(resource, action, decision)
        except _NoCredential:
            return Decision(False, "Deny", "the wallet holds no credential for this request")
        except Exception as error:  # an action that cannot be authorized does not happen
            return Decision(False, "Indeterminate", f"{type(error).__name__}: {error}")

    def _park(self, resource: str, action: dict[str, str] | None, denied: Decision) -> Decision:
        """Asks people to approve the action and, when they have, asks again with their Approval."""
        parked = self._identity.park(resource, action or {})
        deadline = time.monotonic() + self._approval_wait
        while True:
            state = self._identity.take_approval(parked["id"])
            if state == "ready":
                return self._ask(resource, [WORKLOAD, APPROVAL], action)
            if state == "gone":
                return Decision(False, "Deny", f"{denied.reason}; the request for approval was rejected, handed out or has run out")
            if time.monotonic() >= deadline:
                return Decision(False, "Pending", f"{denied.reason}; waiting for approval, request {parked['id']}")
            self._sleep(self._approval_poll)

    def _present(self, link: str) -> dict:
        """Has the wallet answer the request. When the wallet could not sign (502), it tries again up to `signing_retries` times, `signing_wait`
        seconds apart. When that does not help, say because the key is gone, it gets a new credential with a new key and tries once more."""
        for retry in range(self._signing_retries + 1):
            try:
                return call("POST", f"{self._wallet}/present", {"link": link})
            except HttpError as error:
                if error.status != 502:
                    raise
                failed = error
            if retry < self._signing_retries:
                self._sleep(self._signing_wait)
        try:
            self._identity.renew()
        except Exception:
            raise failed from None
        return call("POST", f"{self._wallet}/present", {"link": link})

    def _ask(self, resource: str, credentials: list[dict], action: dict[str, str] | None) -> Decision:
        for attempt in (1, 2):
            started = call(
                "POST",
                f"{self._attesta}/v1/authorize/requests",
                {
                    "resource": resource,
                    "policyId": self._policy,
                    "identityClaim": IDENTITY_CLAIM,
                    "credentials": credentials,
                    **({"action": action} if action else {}),
                },
            )
            try:
                answered = self._present(started["authorizationRequest"])
            except HttpError as error:
                if error.status != 409:
                    raise
                # The wallet holds no credential for the request, a Mandate approved after it last asked the issuer for example.
                if attempt == 2:
                    raise _NoCredential() from None
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
        advice = tuple(item.get("id", "") for item in outcome.get("advice") or [])
        return Decision(outcome.get("decision") == "Permit", str(outcome.get("decision")), str(outcome.get("reason", "")), subject, advice)
