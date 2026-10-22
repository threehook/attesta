import logging
import os

from .gate import Gate
from .identity import WorkloadIdentity, spiffe_jwt_source
from .service import serve
from .tools import LoggingRunner


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    wallet = os.environ.get("CICD_WALLET_URL", "http://127.0.0.1:7100")
    identity = WorkloadIdentity(
        issuer_url=os.environ.get("CICD_ISSUER_URL", "http://attesta-issuer-api:4001"),
        wallet_url=wallet,
        svid=spiffe_jwt_source(os.environ.get("CICD_SPIFFE_AUDIENCE", "attesta-issuer")),
        renew_after=float(os.environ.get("CICD_CREDENTIAL_RENEW_SECONDS", "1800")),
    )
    gate = Gate(os.environ.get("ATTESTA_URL", "http://127.0.0.1:8080"), wallet, identity, policy_id=os.environ.get("CICD_POLICY_ID", "cicd"))
    serve(gate, LoggingRunner(), int(os.environ.get("CICD_PORT", "8090")))


main()
