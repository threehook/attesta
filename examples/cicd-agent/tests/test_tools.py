import asyncio

from cicd_agent.gate import Decision
from cicd_agent.tools import TOOLS, _only_our_tools, build_options, build_tools


class FakeGate:
    def __init__(self, permit):
        self.permit = permit
        self.asked: list[str] = []

    def authorize(self, resource):
        self.asked.append(resource)
        return Decision(self.permit, "Permit" if self.permit else "Deny", "ok" if self.permit else "Mandate ontbreekt")


class FakeRunner:
    def __init__(self):
        self.done: list[tuple] = []

    def run_tests(self, application):
        self.done.append(("run_tests", application))
        return "tests passed"

    def deploy(self, application, version, environment):
        self.done.append(("deploy", application, version, environment))
        return f"{application} {version} deployed to {environment}"


def tools_of(gate, runner):
    return {t.name: t for t in build_tools(gate, runner)}


def test_an_action_runs_only_after_a_permit():
    gate, runner = FakeGate(True), FakeRunner()
    reply = asyncio.run(tools_of(gate, runner)["deploy"].handler({"application": "shop", "version": "v1", "environment": "staging"}))
    assert gate.asked == ["deploy:staging"] and runner.done == [("deploy", "shop", "v1", "staging")]
    assert reply["content"][0]["text"] == "shop v1 deployed to staging" and "is_error" not in reply


def test_a_denied_action_does_not_run_and_says_why():
    gate, runner = FakeGate(False), FakeRunner()
    reply = asyncio.run(tools_of(gate, runner)["deploy"].handler({"application": "shop", "version": "v1", "environment": "prod"}))
    assert runner.done == [] and reply["is_error"] and "Mandate ontbreekt" in reply["content"][0]["text"]
    reply = asyncio.run(tools_of(gate, runner)["run_tests"].handler({"application": "shop"}))
    assert runner.done == [] and reply["is_error"] and gate.asked == ["deploy:prod", "run_tests"]


def test_the_agent_has_only_its_own_tools():
    options = build_options(FakeGate(True), FakeRunner())
    assert options.tools == [] and options.allowed_tools == TOOLS
    hook = options.hooks["PreToolUse"][0].hooks[0]
    assert asyncio.run(hook({"tool_name": TOOLS[1]}, None, None)) == {}
    for foreign in ("Bash", "Read", "WebFetch", "mcp__other__thing"):
        denied = asyncio.run(_only_our_tools({"tool_name": foreign}, None, None))
        assert denied["hookSpecificOutput"]["permissionDecision"] == "deny"
