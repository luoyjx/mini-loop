"""Fresh-Agent adapter for real read-only workflow node execution."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, Mapping
from pathlib import Path
from typing import Any

from ..agent import Agent
from ..builtins import default_registry
from ..compaction import InMemoryCompactor
from ..config import Settings
from ..permissions import default_hooks
from ..registry import Tool
from ..run_context import RunContext
from ..tool_policy import DEFAULT_ROLE_TOOL_POLICY
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
        secrets=None,
        sandbox=None,
        harness=None,
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
        # A fresh worker is still a worker: it reads repository files, so it
        # needs the same masking and confinement the parent runs under.
        # Without these it was the one path where a credential in a workspace
        # file reached an artifact unmasked.
        from ..harness import Harness

        # Start from the parent's policy set so a worker inherits every seam,
        # then narrow the parts a workflow node must control. Listing seams here
        # is what let masking and confinement go missing on this path.
        base = harness or Harness()
        self.harness = base.derive(secrets=secrets or base.secrets,
                                   sandbox=sandbox or base.sandbox)
        self.secrets = self.harness.secrets
        self.sandbox = self.harness.sandbox
        self.last_tool_names: tuple[str, ...] = ()
        self.last_run_context: RunContext | None = None

    def _worker_registry(self):
        """Inherit read capabilities without delegating write or process tools."""

        parent = self.harness.tools or default_registry()
        policy = self.harness.role_tool_policy or DEFAULT_ROLE_TOOL_POLICY
        return policy.select("Explore", parent)

    async def __call__(
        self,
        attempt: NodeAttempt,
        node: WorkflowNode,
        inputs: Mapping[str, Any],
    ) -> ArtifactSubmission:
        captured: list[ArtifactSubmission] = []
        registry = self._worker_registry()

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
                # Captures the node's result in-process; no external side effect.
                # Classified honestly as a read so the readonly backstop below
                # admits it -- an unclassified tool would be denied in readonly
                # mode, and leaving it unclassified is also just wrong.
                readonly=True,
                risk="read",
            )
        )
        self.last_tool_names = tuple(registry.names())
        rounds = min(self.max_rounds, node.max_rounds or self.max_rounds)
        system = (
            "You are an isolated read-only workflow worker. "
            "Use only the provided read-only repository tools for evidence. "
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
            # A readonly permission backstop. The worker's read-only guarantee
            # used to rest entirely on the `subset(("read_file", "glob"))` above
            # with `hooks=Hooks()` -- an empty policy, unlike every other agent,
            # which runs under a PermissionHook. That made the tool allowlist a
            # single point of failure: one line broadening the subset, or a read
            # tool growing a side effect, and a workflow worker (which processes
            # inputs it does not control) could mutate with nothing to stop it.
            # Readonly mode denies write/exec/external/unclassified risk, so the
            # guarantee now has two independent barriers instead of one.
            state={"permission_mode": "readonly"},
            harness=self.harness.derive(
                tools=registry,
                hooks=default_hooks(),
                compactor=InMemoryCompactor(),
                # A workflow node owns its own turn budget and history; the
                # parent's injectors and stop hooks must not reach it.
                injectors=(),
            ),
            system=system,
            label=f"workflow>{node.id}",
            depth=1,
            max_rounds=rounds,
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
