"""The subagent provider seam: who executes a delegated task is swappable.

DeepSeek Harness puts subagent execution behind one service (`ctx.subagents`)
whose providers range from an in-process child agent to a forked session to
a *different product* driven over its own protocol -- all behind the same
interface, so the `task` tool neither knows nor cares which ran. mini-loop
had exactly one hard-wired shape (a fresh in-process `Agent`), constructed
inline in the loop, so an embedder wanting a remote or containerized worker
had to fork `_run_subagent`.

Two dsh rules ride along:

* **lineage is data, not visibility** -- the child carries who delegated it
  and at what depth as plain state (`lineage`), and inherits nothing by
  scope: its tool set comes from the role policy, its seams from a derived
  harness value.
* **the provider owns construction; the loop owns telemetry** -- the
  `subagent_start`/`subagent_end` events stay in the loop, so every
  provider's runs are observable the same way.
"""

from __future__ import annotations

from typing import Any, Protocol, runtime_checkable

__all__ = ["SubagentProvider", "InProcessSubagents"]


@runtime_checkable
class SubagentProvider(Protocol):
    async def run(
        self,
        parent: Any,
        *,
        prompt: str,
        agent_type: str,
        run_context: Any,
    ) -> str:
        """Execute one delegated task and return its final summary."""
        ...


class InProcessSubagents:
    """The default provider: a fresh child Agent in the parent's workspace."""

    def __init__(self) -> None:
        #: The last child's lineage, for tests and diagnostics.
        self.last_lineage: dict | None = None

    async def run(self, parent, *, prompt, agent_type, run_context):
        from .agent import Agent

        child_context = run_context.derive_peer_agent(delegated_by=parent.label)
        is_explore = agent_type.strip().lower() == "explore"
        registry = parent.role_tool_policy.select(agent_type, parent.tools)
        verb = "explore and report" if is_explore else "complete the task"
        # Lineage as data: who delegated, from which session shape, how deep.
        # Never used for visibility -- the tool set above came from the role
        # policy, not from inheriting the parent's scope.
        lineage = {
            "parent": parent.label,
            "delegation_depth": parent.depth + 1,
        }
        self.last_lineage = lineage
        state: dict = {"lineage": lineage}
        if is_explore:
            state["permission_mode"] = "readonly"
        child = Agent(
            client=parent.client,
            settings=parent.settings,
            workspace=parent.workspace,
            # Derive, do not re-list: the child inherits every seam the parent
            # has, including ones added after this line was written.
            harness=parent.harness.derive(
                tools=registry,
                hooks=parent.hooks,
                skills=parent.skills,
                compactor=parent.compactor,
                recovery=parent.recovery,
                stuck_detector=parent.stuck_detector,
                cache_policy=parent.cache_policy,
                secrets=parent.secrets,
                sandbox=parent.sandbox,
                transport=parent.transport,
            ),
            system=f"You are a {agent_type} subagent in {parent.workspace}. "
                   f"Use tools to {verb}, then give a concise final summary. No preamble.",
            emit=parent.emit,
            llm_semaphore=parent.semaphore,
            tool_semaphore=parent.tool_semaphore,
            label=f"{parent.label}>{agent_type.lower()}",
            depth=parent.depth + 1,
            max_rounds=parent.settings.subagent_max_rounds,
            state=state,
        )
        if is_explore:
            # "Explore is read-only" is a promise the `task` tool makes to the
            # model, and it was only a tool-list convention: the default
            # interactive mode runs a plain `echo x > file` via bash with no
            # approval (only *destructive* shell asks), so an Explore subagent
            # could mutate the workspace a caller delegated as read-only.
            # Read-only mode denies every mutating-risk tool -- bash included --
            # so the promise holds by construction, whatever the registry carries.
            assert child.state["permission_mode"] == "readonly"
        return await child.run(prompt, run_context=child_context)


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: providers are pure construction plus one awaited "
    "run; the readonly promise is asserted inline at build time."
)
