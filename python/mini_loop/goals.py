"""Same-session goals: one durable objective, a round budget, CAS mutations.

Modelled on DeepSeek Harness's goal domain. The properties kept:

* **A goal is state, not a scheduler.** The durable record answers "what is
  this session trying to finish and what happened to that objective"; the
  session log stays the source of truth (every mutation is a `goal_change`
  event carrying the full post-mutation snapshot, folded on restore).
* **Compare-and-set.** Every mutation names the revision it read; a stale
  revision is refused, so two continuation consumers cannot fight over one
  goal without one of them noticing.
* **A round budget.** Only goal-sourced continuations consume it -- a human
  talking to the same session does not -- and exhausting it blocks the goal
  with a stable, machine-routable code instead of silently stopping.
* **Blocked carries a reason.** A lower-kebab-case `code` for routing plus
  free text for humans and models.
* **Activation is process-local** (the round-174 cron rule, same seam):
  create and resume arm continuation; restore comes back disarmed; and the
  arming mutations require explicit-human authority, so a cron-fired or
  delegated turn cannot re-authorize its own unattended continuation.
"""

from __future__ import annotations

import re
import uuid
from typing import Any

from .run_context import EXPLICIT_HUMAN

__all__ = [
    "GOAL_PHASES",
    "current_goal",
    "fold_goal",
    "install_goals",
    "GoalContinuation",
]

GOAL_PHASES = ("active", "paused", "blocked", "complete")

#: Round cap resolved when the caller omits one.
DEFAULT_MAX_ROUNDS = 10
MAX_ROUNDS_CEILING = 100

_CODE = re.compile(r"^[a-z][a-z0-9-]*$")


def current_goal(agent: Any) -> dict | None:
    return agent.state.get("goal")


def _armed(agent: Any) -> bool:
    return bool(agent.state.get("goal_armed"))


async def _record(agent: Any, goal: dict, operation: str) -> None:
    await agent._send("goal_change", operation=operation, goal=dict(goal))


def fold_goal(events: list[dict]) -> dict | None:
    """The goal in force is the last logged snapshot; clear tombstones erase it.

    Deliberately does NOT restore activation: a restored goal is a fact, not
    an authorization (same rule as cron jobs and dsh's goal domain).
    """

    goal: dict | None = None
    for event in events:
        if event.get("type") != "goal_change":
            continue
        if event.get("operation") == "clear":
            goal = None
        elif isinstance(event.get("goal"), dict):
            goal = dict(event["goal"])
    return goal


def _check_ref(goal: dict | None, revision: int) -> str | None:
    if goal is None:
        return "Error: no current goal"
    if int(revision) != goal["revision"]:
        return (
            f"Error: stale revision {revision}; the goal is at revision "
            f"{goal['revision']}. Read goal_status and retry with the "
            "current revision."
        )
    return None


from .registry import Hook


class GoalContinuation(Hook):
    """The continuation consumer: one more round while armed, active, in budget.

    An `on_stop` hook. Exhausting the budget blocks the goal with the stable
    code `round-cap-exhausted` rather than silently stopping -- a schedule
    that quietly dies is indistinguishable from one that never existed.
    """

    async def on_stop(self, agent, messages, last_text):
        goal = current_goal(agent)
        if goal is None or goal["phase"] != "active" or not _armed(agent):
            return None
        if goal["rounds_started"] >= goal["max_rounds"]:
            goal["revision"] += 1
            goal["phase"] = "blocked"
            goal["blocked"] = {
                "code": "round-cap-exhausted",
                "message": (
                    f"the goal used all {goal['max_rounds']} continuation "
                    "rounds without completing; a human can raise the cap "
                    "with goal_edit and resume"
                ),
            }
            agent.state["goal_armed"] = False
            await _record(agent, goal, "block")
            return None
        goal["revision"] += 1
        goal["rounds_started"] += 1
        await _record(agent, goal, "round")
        return (
            f"[Goal round {goal['rounds_started']}/{goal['max_rounds']}] "
            f"Objective: {goal['objective']}\n"
            "Continue working toward it. Call goal_complete (with the "
            "current revision) when it is finished, or goal_block if you "
            "cannot proceed."
        )


_SET_SCHEMA = {
    "type": "object",
    "properties": {
        "objective": {"type": "string", "description": "The completion objective."},
        "max_rounds": {"type": "integer", "description": "Continuation round cap."},
    },
    "required": ["objective"],
}
_REF_SCHEMA = {
    "type": "object",
    "properties": {"revision": {"type": "integer"}},
    "required": ["revision"],
}
_BLOCK_SCHEMA = {
    "type": "object",
    "properties": {
        "revision": {"type": "integer"},
        "code": {"type": "string", "description": "stable lower-kebab-case classification"},
        "message": {"type": "string"},
    },
    "required": ["revision", "code", "message"],
}
_EMPTY = {"type": "object", "properties": {}}


def _render(goal: dict | None, *, armed: bool) -> str:
    if goal is None:
        return "No current goal."
    lines = [
        f"goal {goal['id']} rev {goal['revision']}: {goal['phase']}"
        f"{' (armed)' if armed else ' (disarmed)'}",
        f"objective: {goal['objective']}",
        f"rounds: {goal['rounds_started']}/{goal['max_rounds']}",
    ]
    if goal.get("blocked"):
        lines.append(
            f"blocked [{goal['blocked']['code']}]: {goal['blocked']['message']}"
        )
    return "\n".join(lines)


def install_goals(registry) -> None:
    from .registry import Tool

    def _requires_human(ctx) -> str | None:
        # Create and resume ARM unattended continuation; that edge belongs to
        # an authenticated human, exactly like workflow launch. A cron-fired
        # or delegated turn carries untrusted/peer authority and is refused.
        if ctx.run_context is None or ctx.run_context.authority != EXPLICIT_HUMAN:
            return (
                "Error: goal_create and goal_resume require explicit human "
                "authority; this run is not carrying it"
            )
        return None

    async def goal_create(ctx, objective: str, max_rounds: int | None = None) -> str:
        agent = ctx.agent
        refused = _requires_human(ctx)
        if refused:
            return refused
        goal = current_goal(agent)
        if goal is not None and goal["phase"] != "complete":
            return (
                f"Error: a goal already exists in phase {goal['phase']!r} "
                f"(rev {goal['revision']}); complete, block, or clear it first"
            )
        cap = int(max_rounds) if max_rounds else DEFAULT_MAX_ROUNDS
        if not 1 <= cap <= MAX_ROUNDS_CEILING:
            return f"Error: max_rounds must be 1..{MAX_ROUNDS_CEILING}"
        goal = {
            "id": f"goal_{uuid.uuid4().hex[:8]}",
            "revision": 1,
            "objective": str(objective),
            "phase": "active",
            "rounds_started": 0,
            "max_rounds": cap,
            "blocked": None,
        }
        agent.state["goal"] = goal
        agent.state["goal_armed"] = True
        await _record(agent, goal, "create")
        return _render(goal, armed=True)

    async def goal_status(ctx) -> str:
        return _render(current_goal(ctx.agent), armed=_armed(ctx.agent))

    async def goal_complete(ctx, revision: int) -> str:
        agent = ctx.agent
        goal = current_goal(agent)
        stale = _check_ref(goal, revision)
        if stale:
            return stale
        if goal["phase"] == "complete":
            return "Error: the goal is already complete"
        goal["revision"] += 1
        goal["phase"] = "complete"
        goal["blocked"] = None
        agent.state["goal_armed"] = False
        await _record(agent, goal, "complete")
        return _render(goal, armed=False)

    async def goal_block(ctx, revision: int, code: str, message: str) -> str:
        agent = ctx.agent
        goal = current_goal(agent)
        stale = _check_ref(goal, revision)
        if stale:
            return stale
        if not _CODE.match(code or ""):
            return "Error: code must be stable lower-kebab-case (e.g. needs-credentials)"
        if not (message or "").strip():
            return "Error: a blocked goal needs a human-readable message"
        goal["revision"] += 1
        goal["phase"] = "blocked"
        goal["blocked"] = {"code": code, "message": message.strip()}
        agent.state["goal_armed"] = False
        await _record(agent, goal, "block")
        return _render(goal, armed=False)

    async def goal_resume(ctx, revision: int) -> str:
        agent = ctx.agent
        refused = _requires_human(ctx)
        if refused:
            return refused
        goal = current_goal(agent)
        stale = _check_ref(goal, revision)
        if stale:
            return stale
        if goal["phase"] == "complete":
            return "Error: a completed goal is finished; create a new one"
        if goal["rounds_started"] >= goal["max_rounds"]:
            return (
                "Error: the round budget is exhausted; raise max_rounds via "
                "a new goal"
            )
        goal["revision"] += 1
        goal["phase"] = "active"
        goal["blocked"] = None
        agent.state["goal_armed"] = True
        await _record(agent, goal, "resume")
        return _render(goal, armed=True)

    registry.register(Tool(
        "goal_create", "Create and arm this session's completion goal.",
        _SET_SCHEMA, goal_create, risk="write",
    ))
    registry.register(Tool(
        "goal_status", "Read the current goal, its phase, revision and round budget.",
        _EMPTY, goal_status, readonly=True, risk="read",
    ))
    registry.register(Tool(
        "goal_complete", "Mark the current goal complete (CAS by revision).",
        _REF_SCHEMA, goal_complete, risk="write",
    ))
    registry.register(Tool(
        "goal_block",
        "Mark the goal blocked with a stable kebab-case code and an explanation.",
        _BLOCK_SCHEMA, goal_block, risk="write",
    ))
    registry.register(Tool(
        "goal_resume", "Resume a paused/blocked goal and re-arm continuation.",
        _REF_SCHEMA, goal_resume, risk="write",
    ))

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: domain rules are enforced inline as CAS refusals (_check_ref) and authority checks; a refusal is the report."
)
