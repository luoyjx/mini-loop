"""Fresh-Agent adapter for real read-only workflow node execution."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, Mapping
from pathlib import Path
from typing import Any

from ..agent import Agent
from ..builtins import default_registry
from ..compaction import InMemoryCompactor
from ..config import Settings
from ..registry import Hooks, Tool
from ..run_context import RunContext
from .artifacts import ArtifactSubmission, return_artifact
from .models import NodeAttempt, WorkflowNode, canonical_json
from .validation import validate_json_value


class FreshAgentRunner:
    """Run each workflow node in a fresh, strictly read-only Agent context.

    The runner intentionally does not accept parent messages, parent hooks, or a
    parent registry.  Its tool catalog is rebuilt per node and contains only
    ``read_file``, ``glob``, and the node-specific synthetic
    ``return_artifact`` tool.
    """

    def __init__(
        self,
        *,
        client,
        settings: Settings,
        workspace: Path,
        context_resolver: Callable[[NodeAttempt], RunContext],
        max_rounds: int = 8,
        llm_semaphore=None,
        tool_semaphore=None,
        emit: Callable[[dict], Awaitable[None]] | None = None,
    ) -> None:
        if max_rounds <= 0:
            raise ValueError("max_rounds must be positive")
        self.client = client
        self.settings = settings
        self.workspace = Path(workspace).resolve()
        self.context_resolver = context_resolver
        self.max_rounds = max_rounds
        self.llm_semaphore = llm_semaphore
        self.tool_semaphore = tool_semaphore
        self.emit = emit
        self.last_tool_names: tuple[str, ...] = ()
        self.last_run_context: RunContext | None = None

    async def __call__(
        self,
        attempt: NodeAttempt,
        node: WorkflowNode,
        inputs: Mapping[str, Any],
    ) -> ArtifactSubmission:
        captured: list[ArtifactSubmission] = []
        registry = default_registry().subset(("read_file", "glob"))

        async def submit(_ctx, value):
            if captured:
                return "Error: return_artifact was already submitted"
            validate_json_value(node.output_schema, value)
            captured.append(return_artifact(value))
            return "Structured artifact accepted. Stop and finish this node."

        registry.register(
            Tool(
                "return_artifact",
                "Submit the node's final structured result. Call exactly once.",
                {
                    "type": "object",
                    "properties": {"value": dict(node.output_schema)},
                    "required": ["value"],
                    "additionalProperties": False,
                },
                submit,
            )
        )
        self.last_tool_names = tuple(registry.names())
        rounds = min(self.max_rounds, node.max_rounds or self.max_rounds)
        system = (
            "You are an isolated read-only workflow worker. "
            "Use only the provided read_file/glob tools for repository evidence. "
            "Do not assume access to a parent conversation. "
            "You must finish by calling return_artifact exactly once with a value "
            "matching this JSON Schema:\n"
            f"{canonical_json(node.output_schema)}"
        )
        prompt = (
            f"Workflow node: {node.id} ({node.kind.value})\n"
            f"Task:\n{node.prompt_template or node.id}\n"
            f"Structured inputs:\n{canonical_json(inputs)}"
        )
        agent = Agent(
            client=self.client,
            settings=self.settings,
            workspace=self.workspace,
            tools=registry,
            hooks=Hooks(),
            system=system,
            label=f"workflow>{node.id}",
            depth=1,
            max_rounds=rounds,
            compactor=InMemoryCompactor(),
            llm_semaphore=self.llm_semaphore,
            tool_semaphore=self.tool_semaphore,
            emit=self.emit,
        )
        launch_context = self.context_resolver(attempt)
        if not isinstance(launch_context, RunContext):
            raise TypeError("context_resolver must return RunContext")
        worker_context = launch_context.derive_peer_agent(
            delegated_by=f"workflow:{attempt.run_id}",
            actor_id=attempt.agent_id,
        )
        self.last_run_context = worker_context
        await agent.run(prompt, run_context=worker_context)
        if not captured:
            raise RuntimeError(
                f"workflow node {node.id} did not call return_artifact"
            )
        return captured[0]
