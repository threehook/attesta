"""The agent's identity: a JWT-SVID shown to the issuer gives the credentials its wallet holds: the Workload credential and, when people allowed
something, the Mandate."""
import threading
import time
from typing import Callable

from .jsonhttp import HttpError, call


class IdentityError(Exception):
    """The agent could not get a credential for its wallet."""


def spiffe_jwt_source(audience: str) -> Callable[[], str]:
    """Fetches a JWT-SVID for the audience from the SPIFFE Workload API (the address is in SPIFFE_ENDPOINT_SOCKET)."""

    def fetch() -> str:
        from spiffe import WorkloadApiClient

        client = WorkloadApiClient()
        try:
            return client.fetch_jwt_svid(audience={audience}).token
        finally:
            client.close()

    return fetch


class WorkloadIdentity:
    """Keeps the wallet's credentials fresh. Each credential ends by itself, so the agent renews before that."""

    def __init__(self, issuer_url: str, wallet_url: str, svid: Callable[[], str], renew_after: float = 1800, clock: Callable[[], float] = time.monotonic):
        self._issuer = issuer_url.rstrip("/")
        self._wallet = wallet_url.rstrip("/")
        self._svid = svid
        self._renew_after = renew_after
        self._clock = clock
        self._obtained: float | None = None
        self._lock = threading.Lock()

    def ensure(self) -> None:
        """Gets a credential when there is none or it is old enough to renew."""
        with self._lock:
            if self._obtained is not None and self._clock() - self._obtained < self._renew_after:
                return
            self._obtain()

    def renew(self) -> None:
        """Gets a new credential now, for when the verifier did not accept the one the wallet holds, for example because it was revoked."""
        with self._lock:
            self._obtain()

    def _obtain(self) -> None:
        try:
            offers = call("POST", f"{self._issuer}/workloads/offer", token=self._svid())
            for offer in (offers["offer"], offers.get("mandate")):
                if offer:
                    call("POST", f"{self._wallet}/credentials", {"offer": offer})
        except HttpError as error:
            raise IdentityError(f"no credential: {error}") from error
        except (KeyError, OSError) as error:
            raise IdentityError(f"no credential: {error!r}") from error
        self._obtained = self._clock()
