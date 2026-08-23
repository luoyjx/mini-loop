"""Role-constrained agents for the verified loop (LongHorizon priority 3).

The research doc's authority rule 3: "Manager 和 Auditor 同时通过
ToolCatalog、permission mode 与 sandbox 强制只读，不能只在 prompt 中声明"
-- and its boundary #2 upstream: LongHorizon's own "independent auditor"
guarantees a fresh context but NOT enforced read-only. mini-loop already
holds the machinery (the Explore promise, round 183: readonly permission
mode denies every mutating-risk tool by construction, whatever the
registry carries). This module binds that machinery to the two read-only
roles the coordinator will drive, so isolation is a property of
CONSTRUCTION, not of any role's prompt.

The Executor is deliberately absent: it runs with real capabilities
through the ordinary session/subagent paths and needs nothing special
here. What this module owns is the guarantee that a proposal or an audit
can never be a side effect.
"""

from __future__ import annotations

__all__ = ["readonly_role_agent", "READONLY_ROLES"]

#: The verified loop's zero-write roles. A proposal and an audit are
#: observations; a role that can mutate the workspace can manufacture the
#: evidence it later cites.
READONLY_ROLES = ("manager", "auditor")


def readonly_role_agent(parent, role: str, *, system: str):
    """A fresh agent for a zero-write role, isolation enforced three ways.

    Catalog: the explore capability set (read-only tools only). Permission
    mode: `readonly` in agent state, which denies write/exec/external and
    unclassified risks outright even if the catalog ever widens. Prompts
    carry the role's INSTRUCTIONS; they carry none of its authority.

    Built on the parent's harness via `derive`, the round-183 rule: the
    child inherits every seam, including ones added later.
    """

    from .agent import Agent

    if role not in READONLY_ROLES:
        raise ValueError(
            f"unknown read-only role {role!r}; the executor runs through "
            "the ordinary paths and takes no construction from this module"
        )
    registry = parent.role_tool_policy.select("explore", parent.tools)
    state: dict = {
        "permission_mode": "readonly",
        "lineage": {
            "parent": parent.label,
            "delegation_depth": parent.depth + 1,
            "role": role,
        },
    }
    agent = Agent(
        client=parent.client,
        settings=parent.settings,
        workspace=parent.workspace,
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
        system=system,
        emit=parent.emit,
        llm_semaphore=parent.semaphore,
        tool_semaphore=parent.tool_semaphore,
        label=f"{parent.label}>{role}",
        depth=parent.depth + 1,
        max_rounds=parent.settings.subagent_max_rounds,
        state=state,
    )
    # The same inline assertion the Explore promise carries (round 183):
    # constructed wrong is refused at build time, not discovered at audit.
    assert agent.state["permission_mode"] == "readonly"
    return agent


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: role isolation is enforced at construction "
    "(explore catalog + readonly permission mode) and pinned by tests that "
    "drive hostile writes through a built role agent."
)
