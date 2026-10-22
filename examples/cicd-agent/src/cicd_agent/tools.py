"""The agent's tools. They are all it has: no shell, no files. A hook asks the gate before each tool runs, so an action runs only after Permit."""
import asyncio
import json
import logging
from typing import Protocol

from claude_agent_sdk import ClaudeAgentOptions, HookMatcher, create_sdk_mcp_server, query, tool
from claude_agent_sdk import AssistantMessage, ResultMessage, TextBlock

from .guard import Guard

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


def _reply(text: str, error: bool = False) -> dict:
    return {"content": [{"type": "text", "text": text}], **({"is_error": True} if error else {})}


def build_tools(runner: Runner):
    """The tools only carry out the action. Whether it may happen is decided before, by the hook."""

    @tool("run_tests", "Run the tests of an application", {"application": str})
    async def run_tests(args: dict) -> dict:
        return _reply(runner.run_tests(args["application"]))

    @tool("deploy", "Deploy a version of an application to an environment", {"application": str, "version": str, "environment": str})
    async def deploy(args: dict) -> dict:
        return _reply(runner.deploy(args["application"], args["version"], args["environment"]))

    return [run_tests, deploy]


def _refuse(reason: str) -> dict:
    return {"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}}


def gate_hook(guard: Guard):
    """The hook that runs before every tool call: only our tools, and only what Attesta permits. A tool that is refused does not run."""

    async def hook(input_data, tool_use_id, context):
        name = input_data.get("tool_name", "")
        if name not in TOOLS:
            return _refuse("this agent has only run_tests and deploy")
        decision = await asyncio.to_thread(guard.check, name.removeprefix(f"mcp__{SERVER}__"), input_data.get("tool_input") or {})
        return {} if decision.permit else _refuse(f"refused: {decision.reason}")

    return hook


def build_options(guard: Guard, runner: Runner) -> ClaudeAgentOptions:
    return ClaudeAgentOptions(
        system_prompt=SYSTEM_PROMPT,
        mcp_servers={SERVER: create_sdk_mcp_server(name=SERVER, version="0.1.0", tools=build_tools(runner))},
        tools=[],  # none of the built-in tools
        allowed_tools=TOOLS,
        hooks={"PreToolUse": [HookMatcher(matcher=None, hooks=[gate_hook(guard)])]},
        max_turns=10,
    )


async def run_task(task: str, guard: Guard, runner: Runner) -> str:
    """Lets the agent carry out the task with its tools, and gives what it answered."""
    answer = ""
    async for message in query(prompt=task, options=build_options(guard, runner)):
        if isinstance(message, AssistantMessage):
            answer = "".join(block.text for block in message.content if isinstance(block, TextBlock)) or answer
        elif isinstance(message, ResultMessage) and message.result:
            answer = message.result
    return answer
