"""The agent's tools. They are all it has: no shell, no files. Each action runs only after the gate said Permit."""
import json
import logging
from typing import Protocol

from claude_agent_sdk import ClaudeAgentOptions, HookMatcher, create_sdk_mcp_server, query, tool
from claude_agent_sdk import AssistantMessage, ResultMessage, TextBlock

from .gate import Gate

SERVER = "cicd"
TOOLS = [f"mcp__{SERVER}__run_tests", f"mcp__{SERVER}__deploy"]

SYSTEM_PROMPT = (
    "You are a CI/CD agent. You have two tools: run_tests and deploy. When a tool says the action was refused, tell the user the reason and stop; "
    "do not try another way."
)


class Runner(Protocol):
    """What the actions do. The default only records them; a real one builds and deploys."""

    def run_tests(self, application: str) -> str: ...

    def deploy(self, application: str, version: str, environment: str) -> str: ...


class LoggingRunner:
    def __init__(self, log: logging.Logger | None = None):
        self._log = log or logging.getLogger("cicd_agent.runner")

    def run_tests(self, application: str) -> str:
        self._log.info(json.dumps({"action": "run_tests", "application": application}))
        return f"tests of {application} passed"

    def deploy(self, application: str, version: str, environment: str) -> str:
        self._log.info(json.dumps({"action": "deploy", "application": application, "version": version, "environment": environment}))
        return f"{application} {version} deployed to {environment}"


# The environments where people must have allowed the deployment.
MANDATE_FOR = {"prod"}


def _reply(text: str, error: bool = False) -> dict:
    return {"content": [{"type": "text", "text": text}], **({"is_error": True} if error else {})}


def build_tools(gate: Gate, runner: Runner):
    @tool("run_tests", "Run the tests of an application", {"application": str})
    async def run_tests(args: dict) -> dict:
        decision = gate.authorize("run_tests")
        if not decision.permit:
            return _reply(f"refused: {decision.reason}", error=True)
        return _reply(runner.run_tests(args["application"]))

    @tool("deploy", "Deploy a version of an application to an environment", {"application": str, "version": str, "environment": str})
    async def deploy(args: dict) -> dict:
        environment = args["environment"]
        decision = gate.authorize(f"deploy:{environment}:{args['version']}", mandate=environment in MANDATE_FOR)
        if not decision.permit:
            return _reply(f"refused: {decision.reason}", error=True)
        return _reply(runner.deploy(args["application"], args["version"], args["environment"]))

    return [run_tests, deploy]


async def _only_our_tools(input_data, tool_use_id, context):
    if input_data.get("tool_name") in TOOLS:
        return {}
    return {
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "deny",
            "permissionDecisionReason": "this agent has only run_tests and deploy",
        }
    }


def build_options(gate: Gate, runner: Runner) -> ClaudeAgentOptions:
    return ClaudeAgentOptions(
        system_prompt=SYSTEM_PROMPT,
        mcp_servers={SERVER: create_sdk_mcp_server(name=SERVER, version="0.1.0", tools=build_tools(gate, runner))},
        tools=[],  # none of the built-in tools
        allowed_tools=TOOLS,
        hooks={"PreToolUse": [HookMatcher(matcher=None, hooks=[_only_our_tools])]},
        max_turns=10,
    )


async def run_task(task: str, gate: Gate, runner: Runner) -> str:
    """Lets the agent carry out the task with its tools, and gives what it answered."""
    answer = ""
    async for message in query(prompt=task, options=build_options(gate, runner)):
        if isinstance(message, AssistantMessage):
            answer = "".join(block.text for block in message.content if isinstance(block, TextBlock)) or answer
        elif isinstance(message, ResultMessage) and message.result:
            answer = message.result
    return answer
