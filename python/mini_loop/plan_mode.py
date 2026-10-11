"""Plan mode: logged collaboration state, soft guidance, stable catalog.

Modelled on DeepSeek Harness's `dsh-plan-mode`, whose three design choices
this module keeps:

* **Log-only, whole-value state.** `plan_mode` flips are durable session
  events; the state in force is the last logged value, so resume recovers it
  by folding the event log -- no separate store to drift.
* **Soft guidance.** While active, one prompt section asks the model to plan
  before mutating. Sandbox and permission policy enforce restrictions
  independently and neither reads plan state -- plan mode changes what the
  model is TOLD, never what it is ALLOWED. A deployment that wants hard
  enforcement composes `permission_mode` (readonly) beside it.
* **A stable tool catalog.** `exit_plan_mode` stays registered while plan
  mode is OFF and simply fails there. Entering or leaving plan mode changes
  only the prompt, never the request's tool list -- a catalog flip would
  invalidate the cached prompt prefix on every mode change (caching.py) and
  make the mode observable to the provider as a schema change.

Keep-planning is a *failed* `exit_plan_mode` call carrying the reviewer's
feedback, so the model revises and presents again -- not a silent exit.
"""

from __future__ import annotations

from typing import Any

__all__ = ["PLAN_SECTION", "plan_mode_active", "set_plan_mode", "install_plan_mode"]

PLAN_SECTION = (
    "Plan mode is ACTIVE. Investigate and design, but do not mutate the "
    "workspace or run commands with side effects yet. When your plan is "
    "ready, present it with the `exit_plan_mode` tool -- a markdown plan "
    "starting with a `#` heading -- and wait for approval. If the reviewer "
    "asks for changes, revise the plan and present it again."
)


def plan_mode_active(agent: Any) -> bool:
    return bool(agent.state.get("plan_mode"))


def set_plan_mode(agent: Any, active: bool) -> str:
    """Flip the logged state; a no-op when already there.

    The runtime value in `agent.state` is a cache of the last logged event,
    maintained here and rebuilt by `fold_plan_mode` on restore -- the log
    stays the authority.
    """

    if plan_mode_active(agent) == active:
        return "noop"
    agent.state["plan_mode"] = active
    agent.refresh_system()
    return "committed"


def fold_plan_mode(events: list[dict]) -> bool:
    """The state in force is the last logged value; absent means off."""

    active = False
    for event in events:
        if event.get("type") == "plan_mode":
            active = bool(event.get("active"))
    return active


_EXIT_SCHEMA = {
    "type": "object",
    "properties": {
        "plan": {
            "type": "string",
            "description": "The complete plan, markdown, starting with a # heading.",
        },
    },
    "required": ["plan"],
}

_ENTER_SCHEMA = {"type": "object", "properties": {}}


def install_plan_mode(registry, *, approval=None) -> None:
    """Register `enter_plan_mode` + `exit_plan_mode`; both always present.

    `approval` is an async `(ctx, plan) -> tuple[bool, str]` deciding whether
    the presented plan is accepted (and any feedback); `None` auto-approves,
    which keeps plan mode useful headless while a server wires the approvals
    broker in.
    """

    from .registry import Tool

    async def enter_plan_mode(ctx) -> str:
        agent = ctx.agent
        outcome = set_plan_mode(agent, True)
        await agent._send("plan_mode", active=True)
        if outcome == "noop":
            return "Already in plan mode."
        return "Plan mode is now active: investigate and plan; present with exit_plan_mode."

    async def exit_plan_mode(ctx, plan: str) -> str:
        agent = ctx.agent
        if not plan_mode_active(agent):
            # Registered while inactive so the catalog never changes shape;
            # calling it there is an error the model can read, not a state flip.
            return "Error: not in plan mode; there is no plan to present."
        text = (plan or "").strip()
        if not text.startswith("#"):
            return (
                "Error: present the COMPLETE plan as markdown starting with a "
                "`#` heading."
            )
        if approval is not None:
            approved, feedback = await approval(ctx, text)
            if not approved:
                # Keep-planning: a failed call carrying the reviewer's words,
                # so the model revises and presents again.
                return f"Error: plan not approved. Reviewer feedback: {feedback}"
        set_plan_mode(agent, False)
        await agent._send("plan_mode", active=False)
        return "Plan approved. Plan mode is off; proceed with the plan."

    registry.register(
        Tool(
            "enter_plan_mode",
            "Switch to plan mode: investigate and design before mutating anything.",
            _ENTER_SCHEMA,
            enter_plan_mode,
            readonly=True,
            risk="read",
        )
    )
    registry.register(
        Tool(
            "exit_plan_mode",
            "Present the finished plan for approval and leave plan mode. "
            "Only meaningful while plan mode is active.",
            _EXIT_SCHEMA,
            exit_plan_mode,
            readonly=True,
            risk="read",
        )
    )

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: log-only whole-value state whose fold is pinned by restore tests; the runtime value is a cache the log always outranks."
)
