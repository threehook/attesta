import asyncio

from cicd_agent.tools import TOOLS, build_options, build_tools, gate_hook


class FakeGuard:
    def __init__(self, permit):
        self.permit = permit
        self.asked: list[tuple] = []

    def check(self, name, args):
        from cicd_agent.gate import Decision

        self.asked.append((name, dict(args)))
        return Decision(self.permit, "Permit" if self.permit else "Deny", "ok" if self.permit else "Mandaat ontbreekt")


class FakeRunner:
    def __init__(self):
        self.done: list[tuple] = []

    def run_tests(self, application):
        self.done.append(("run_tests", application))
        return "tests passed"

    def deploy(self, application, version, environment):
        self.done.append(("deploy", application, version, environment))
        return f"{application} {version} deployed to {environment}"


def tools_of(runner):
    return {t.name: t for t in build_tools(runner)}


def hook_for(permit):
    guard = FakeGuard(permit)
    return guard, gate_hook(guard)


def call(hook, name, args=None):
    return asyncio.run(hook({"tool_name": name, "tool_input": args or {}}, None, None))


def test_the_tools_only_carry_out_the_action():
    runner = FakeRunner()
    reply = asyncio.run(tools_of(runner)["deploy"].handler({"application": "shop", "version": "v1", "environment": "staging"}))
    assert runner.done == [("deploy", "shop", "v1", "staging")] and reply["content"][0]["text"] == "shop v1 deployed to staging" and "is_error" not in reply


def test_a_permitted_tool_call_goes_on_with_what_was_asked():
    guard, hook = hook_for(True)
    args = {"application": "shop", "version": "v1", "environment": "prod"}
    assert call(hook, TOOLS[1], args) == {}
    assert guard.asked == [("deploy", args)]


def test_a_denied_tool_call_is_refused_with_the_reason():
    guard, hook = hook_for(False)
    refused = call(hook, TOOLS[1], {"application": "shop", "version": "v1", "environment": "prod"})["hookSpecificOutput"]
    assert refused["permissionDecision"] == "deny" and "Mandaat ontbreekt" in refused["permissionDecisionReason"]


def test_the_agent_has_only_its_own_tools_and_nothing_else_is_asked_about():
    guard, hook = hook_for(True)
    for foreign in ("Bash", "Read", "WebFetch", "mcp__other__thing"):
        assert call(hook, foreign)["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert guard.asked == []
    options = build_options(guard, FakeRunner())
    assert options.tools == [] and options.allowed_tools == TOOLS
    assert len(options.hooks["PreToolUse"]) == 1
