from cicd_agent.gate import Decision
from cicd_agent.guard import Guard


class FakeGate:
    def __init__(self, permit=True):
        self.permit = permit
        self.asked: list[tuple] = []

    def authorize(self, resource, mandate=False, action=None):
        self.asked.append((resource, mandate, action))
        return Decision(self.permit, "Permit" if self.permit else "Deny", "why")


def test_a_deployment_names_its_resource_and_the_concrete_action():
    gate = FakeGate()
    assert Guard(gate).check("deploy", {"application": "shop", "version": "v1.4.2", "environment": "staging"}).permit
    assert gate.asked == [("deploy:staging:v1.4.2", False, {"application": "shop", "version": "v1.4.2", "environment": "staging"})]


def test_production_asks_for_the_mandate_and_other_environments_do_not():
    gate = FakeGate()
    Guard(gate).check("deploy", {"application": "shop", "version": "v1", "environment": "prod"})
    Guard(gate).check("deploy", {"application": "shop", "version": "v1", "environment": "test"})
    assert [asked[1] for asked in gate.asked] == [True, False]


def test_tests_are_an_action_of_an_application():
    gate = FakeGate()
    Guard(gate).check("run_tests", {"application": "shop"})
    assert gate.asked == [("run_tests", False, {"application": "shop"})]


def test_what_the_guard_does_not_know_is_denied_without_asking():
    gate = FakeGate()
    assert not Guard(gate).check("rm", {}).permit
    decision = Guard(gate).check("deploy", {"application": "shop"})
    assert not decision.permit and "lacks" in decision.reason
    assert gate.asked == []
