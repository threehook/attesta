"""What the agent can do, as the gate sees it: the resource the policy decides on, the concrete action, and whether people must have allowed it."""
from dataclasses import dataclass, field
from typing import Callable, Mapping

# The environments where people must have allowed the deployment.
MANDATE_FOR = {"prod"}


@dataclass(frozen=True)
class Action:
    resource: str
    detail: Mapping[str, str] = field(default_factory=dict)
    mandate: bool = False


def run_tests(args: Mapping) -> Action:
    return Action("run_tests", {"application": str(args["application"])})


def deploy(args: Mapping) -> Action:
    environment, version = str(args["environment"]), str(args["version"])
    return Action(
        f"deploy:{environment}:{version}",
        {"application": str(args["application"]), "version": version, "environment": environment},
        mandate=environment in MANDATE_FOR,
    )


# The action of each tool, by the tool's name. A tool that is not here is refused.
ACTIONS: dict[str, Callable[[Mapping], Action]] = {"run_tests": run_tests, "deploy": deploy}
