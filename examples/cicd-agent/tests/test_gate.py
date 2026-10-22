from cicd_agent.gate import Gate


class Identity:
    def __init__(self, fail=False):
        self.calls = 0
        self.renewed = 0
        self.fail = fail
        self.parked = []
        self.approval_states = ["waiting"]

    parked: list = []
    approval_states = ["waiting"]

    def park(self, resource, action):
        self.parked.append((resource, action))
        return {"id": "r-1", "status": "open", "approvals": 0, "required": 2}

    def take_approval(self, request_id):
        return self.approval_states.pop(0) if len(self.approval_states) > 1 else self.approval_states[0]

    def ensure(self):
        self.calls += 1
        if self.fail:
            raise RuntimeError("no credential")

    def renew(self):
        self.renewed += 1
        if self.fail:
            raise RuntimeError("no credential")


def make(server, identity=None, wait=1):
    attesta, wallet = server(), server()
    attesta.routes["POST /v1/authorize/requests"] = (200, {"requestId": "r1", "authorizationRequest": "openid4vp://?x=1"})
    attesta.routes["GET /v1/authorize/requests/r1"] = (200, {"status": "done", "decision": "Permit", "reason": "ok", "subject": {"issuer": "did:key:i", "id": "spiffe://corp/agent"}})
    wallet.routes["POST /present"] = (200, {"status": 200})
    identity = identity or Identity()
    return Gate(attesta.url, wallet.url, identity, sleep=lambda _: None, wait=wait), attesta, wallet, identity


def test_permit_after_the_wallet_answered(server):
    gate, attesta, wallet, identity = make(server)
    decision = gate.authorize("deploy:staging")
    assert decision.permit and decision.decision == "Permit" and decision.subject == "spiffe://corp/agent"
    assert identity.calls == 1
    asked = attesta.requests[0]["body"]
    assert asked["resource"] == "deploy:staging" and asked["identityClaim"] == "workload"
    assert asked["credentials"] == [{"credentialType": "Workload", "claims": ["owner", "environment"]}]
    assert wallet.requests[0]["body"] == {"link": "openid4vp://?x=1"}


def test_the_action_goes_with_the_request(server):
    gate, attesta, _, _ = make(server)
    gate.authorize("deploy:staging:v1", action={"application": "shop", "version": "v1"})
    started = [r["body"] for r in attesta.requests if r["method"] == "POST"]
    assert started[0]["action"] == {"application": "shop", "version": "v1"}
    gate.authorize("run_tests")
    started = [r["body"] for r in attesta.requests if r["method"] == "POST"]
    assert "action" not in started[1]


def test_a_request_with_a_mandate_also_asks_for_it(server):
    gate, attesta, _, _ = make(server)
    assert gate.authorize("deploy:prod:v1", mandate=True).permit
    assert [c["credentialType"] for c in attesta.requests[0]["body"]["credentials"]] == ["Workload", "Mandate"]


def test_a_wallet_without_the_mandate_asks_the_issuer_again_once(server):
    gate, _, wallet, identity = make(server)
    answers = iter([(409, {"error": "the wallet holds no credential that answers the request"}), (200, {"status": 200})])
    wallet.routes["POST /present"] = lambda _request: next(answers)
    assert gate.authorize("deploy:prod:v1", mandate=True).permit
    assert identity.renewed == 1 and len(wallet.requests) == 2


def test_a_wallet_without_the_mandate_is_a_denial(server):
    gate, _, wallet, _ = make(server)
    wallet.routes["POST /present"] = (409, {"error": "the wallet holds no credential that answers the request"})
    decision = gate.authorize("deploy:prod:v1", mandate=True)
    assert not decision.permit and decision.decision == "Deny" and "no credential" in decision.reason


def test_deny_carries_the_reason(server):
    gate, attesta, _, _ = make(server)
    attesta.routes["GET /v1/authorize/requests/r1"] = (200, {"status": "done", "decision": "Deny", "reason": "Mandate ontbreekt"})
    decision = gate.authorize("deploy:prod")
    assert not decision.permit and decision.decision == "Deny" and decision.reason == "Mandate ontbreekt" and decision.subject is None


def test_waits_for_the_answer(server):
    gate, attesta, _, _ = make(server)
    answers = iter([(200, {"status": "pending"}), (200, {"status": "pending"}), (200, {"status": "done", "decision": "Permit", "reason": "ok"})])
    attesta.routes["GET /v1/authorize/requests/r1"] = lambda _request: next(answers)
    assert gate.authorize("deploy:staging").permit


def test_a_request_that_is_never_answered_is_not_permitted(server):
    gate, attesta, _, _ = make(server, wait=0.05)
    attesta.routes["GET /v1/authorize/requests/r1"] = (200, {"status": "pending"})
    decision = gate.authorize("deploy:staging")
    assert not decision.permit and decision.decision == "Indeterminate"


def test_nothing_that_goes_wrong_is_a_permit(server):
    gate, attesta, wallet, identity = make(server)
    wallet.routes["POST /present"] = (403, {"error": "this verifier is not registered for this wallet"})
    assert not gate.authorize("deploy:staging").permit

    gate, attesta, wallet, identity = make(server, identity=Identity(fail=True))
    decision = gate.authorize("deploy:staging")
    assert not decision.permit and "no credential" in decision.reason and attesta.requests == []

    gate, attesta, wallet, identity = make(server)
    attesta.routes["POST /v1/authorize/requests"] = (404, {"error": "unknown policy"})
    assert not gate.authorize("deploy:staging").permit


def test_an_unreachable_attesta_is_not_a_permit():
    gate = Gate("http://127.0.0.1:1", "http://127.0.0.1:1", Identity(), sleep=lambda _: None)
    assert not gate.authorize("deploy:staging").permit


def test_renews_the_credential_once_when_attesta_did_not_accept_it(server):
    gate, attesta, wallet, identity = make(server)
    answers = iter([(200, {"status": 400}), (200, {"status": 200})])
    wallet.routes["POST /present"] = lambda _request: next(answers)
    attesta.routes["POST /v1/authorize/requests"] = (200, {"requestId": "r1", "authorizationRequest": "openid4vp://?x=1"})
    decision = gate.authorize("deploy:staging")
    assert decision.permit and identity.renewed == 1
    assert len([r for r in attesta.requests if r["method"] == "POST"]) == 2


def test_a_renewed_credential_that_is_refused_again_is_not_tried_a_third_time(server):
    gate, attesta, wallet, identity = make(server)
    wallet.routes["POST /present"] = (200, {"status": 400})
    attesta.routes["GET /v1/authorize/requests/r1"] = (200, {"status": "done", "decision": "Deny", "reason": "credential has been revoked"})
    decision = gate.authorize("deploy:staging")
    assert not decision.permit and identity.renewed == 1
    assert len([r for r in attesta.requests if r["method"] == "POST"]) == 2


def test_no_renewal_when_the_policy_itself_said_deny(server):
    gate, attesta, wallet, identity = make(server)
    attesta.routes["GET /v1/authorize/requests/r1"] = (200, {"status": "done", "decision": "Deny", "reason": "Productie vraagt een mandaat"})
    assert not gate.authorize("deploy:prod").permit and identity.renewed == 0


def needs_approval(attesta, advice=({"id": "request_approval"},)):
    answers = iter([
        (200, {"status": "done", "decision": "Deny", "reason": "Productie vraagt een mandaat", "advice": list(advice)}),
        (200, {"status": "done", "decision": "Permit", "reason": "Toegestaan met goedkeuring", "subject": {"id": "spiffe://corp/agent"}}),
    ])
    attesta.routes["GET /v1/authorize/requests/r1"] = lambda _request: next(answers)


def asked_credentials(attesta):
    return [[c["credentialType"] for c in r["body"]["credentials"]] for r in attesta.requests if r["method"] == "POST"]


def test_a_denial_people_can_undo_parks_the_action_and_says_it_waits(server):
    gate, attesta, _, identity = make(server)
    needs_approval(attesta)
    decision = gate.authorize("deploy:prod:v2", mandate=True, action={"version": "v2"})
    assert not decision.permit and decision.decision == "Pending" and "request r-1" in decision.reason
    assert identity.parked == [("deploy:prod:v2", {"version": "v2"})]
    assert asked_credentials(attesta) == [["Workload", "Mandate"]]


def test_an_approval_that_arrives_is_shown_in_place_of_the_mandate(server):
    gate, attesta, _, identity = make(server)
    needs_approval(attesta)
    identity.approval_states = ["ready"]
    decision = gate.authorize("deploy:prod:v2", mandate=True)
    assert decision.permit and decision.subject == "spiffe://corp/agent"
    assert asked_credentials(attesta) == [["Workload", "Mandate"], ["Workload", "Approval"]]


def test_it_waits_for_the_approval_as_long_as_it_is_told_to(server):
    attesta, wallet = server(), server()
    attesta.routes["POST /v1/authorize/requests"] = (200, {"requestId": "r1", "authorizationRequest": "openid4vp://?x=1"})
    wallet.routes["POST /present"] = (200, {"status": 200})
    needs_approval(attesta)
    identity = Identity()
    identity.approval_states = ["waiting", "waiting", "ready"]
    sleeps = []
    gate = Gate(attesta.url, wallet.url, identity, sleep=sleeps.append, approval_wait=60, approval_poll=7)
    assert gate.authorize("deploy:prod:v2", mandate=True).permit
    assert [s for s in sleeps if s == 7] == [7, 7]


def test_a_rejected_request_is_a_denial(server):
    gate, attesta, _, identity = make(server)
    needs_approval(attesta)
    identity.approval_states = ["gone"]
    decision = gate.authorize("deploy:prod:v2", mandate=True)
    assert not decision.permit and decision.decision == "Deny" and "rejected" in decision.reason


def test_a_denial_without_the_advice_is_not_parked(server):
    gate, attesta, _, identity = make(server)
    needs_approval(attesta, advice=())
    decision = gate.authorize("deploy:prod:v2", mandate=True)
    assert not decision.permit and decision.decision == "Deny" and identity.parked == []


def test_a_wallet_without_a_mandate_lets_the_policy_say_what_is_missing(server):
    gate, attesta, wallet, identity = make(server)
    needs_approval(attesta)
    presented = iter([(409, {"error": "no"}), (409, {"error": "no"}), (200, {"status": 200}), (200, {"status": 200})])
    wallet.routes["POST /present"] = lambda _request: next(presented)
    decision = gate.authorize("deploy:prod:v2", mandate=True)
    assert decision.decision == "Pending" and identity.parked
    assert asked_credentials(attesta)[:3] == [["Workload", "Mandate"], ["Workload", "Mandate"], ["Workload"]]
