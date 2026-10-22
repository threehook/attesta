import pytest

from cicd_agent.identity import IdentityError, WorkloadIdentity


def make(server, svid=lambda: "svid-token", renew_after=60, clock=None):
    issuer, wallet = server(), server()
    issuer.routes["POST /workloads/offer"] = (200, {"offer": "openid-credential-offer://?x=1"})
    wallet.routes["POST /credentials"] = (200, {"credentials": [{"type": "Workload"}]})
    kwargs = {"clock": clock} if clock else {}
    return WorkloadIdentity(issuer.url, wallet.url, svid, renew_after=renew_after, **kwargs), issuer, wallet


def test_shows_the_svid_to_the_issuer_and_hands_the_offer_to_the_wallet(server):
    identity, issuer, wallet = make(server)
    identity.ensure()
    assert issuer.requests[0]["authorization"] == "Bearer svid-token"
    assert wallet.requests[0]["body"] == {"offer": "openid-credential-offer://?x=1"}


def test_renews_only_when_the_credential_is_old_enough(server):
    now = [1000.0]
    identity, issuer, wallet = make(server, renew_after=60, clock=lambda: now[0])
    identity.ensure()
    now[0] += 59
    identity.ensure()
    assert len(issuer.requests) == 1
    now[0] += 2
    identity.ensure()
    assert len(issuer.requests) == 2 and len(wallet.requests) == 2


@pytest.mark.parametrize("status", [401, 403, 500])
def test_says_so_when_the_issuer_refuses(server, status):
    identity, issuer, wallet = make(server)
    issuer.routes["POST /workloads/offer"] = (status, {"error": "no"})
    with pytest.raises(IdentityError):
        identity.ensure()
    assert wallet.requests == []


def test_says_so_when_the_wallet_refuses_or_the_svid_cannot_be_had(server):
    identity, issuer, wallet = make(server)
    wallet.routes["POST /credentials"] = (502, {"error": "the issuer is unreachable"})
    with pytest.raises(IdentityError):
        identity.ensure()

    def no_svid():
        raise OSError("no workload api")

    identity, _, _ = make(server, svid=no_svid)
    with pytest.raises(IdentityError):
        identity.ensure()


def test_a_failed_attempt_is_tried_again_next_time(server):
    identity, issuer, wallet = make(server)
    issuer.routes["POST /workloads/offer"] = (500, {"error": "no"})
    with pytest.raises(IdentityError):
        identity.ensure()
    issuer.routes["POST /workloads/offer"] = (200, {"offer": "openid-credential-offer://?x=2"})
    identity.ensure()
    assert wallet.requests[-1]["body"]["offer"] == "openid-credential-offer://?x=2"


def test_hands_the_mandate_offer_to_the_wallet_too(server):
    identity, issuer, wallet = make(server)
    issuer.routes["POST /workloads/offer"] = (200, {"offer": "openid-credential-offer://?x=1", "mandate": "openid-credential-offer://?x=2"})
    identity.ensure()
    assert [r["body"] for r in wallet.requests] == [{"offer": "openid-credential-offer://?x=1"}, {"offer": "openid-credential-offer://?x=2"}]
