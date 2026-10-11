"""Guardian: an agent answers approvals a human would otherwise answer.

Codex's auto-review rule, adopted whole (research doc section 11):
**替换审批者，不是提升权限** -- the guardian replaces the ANSWERER and
nothing else. It resolves a pending approval through the same
`ApprovalBroker.resolve` path a human uses, so every persistence and
event behavior is inherited; it cannot widen writable roots, relax the
sandbox, or change permission modes, because approvals never could.

Default OFF: the broker takes `guardian=None` and behaves exactly as
before. Failure routes toward the STRICTER answerer: an unparseable
verdict, a review exception, or a None all leave the request parked for
the human (whose silence is already a deny).

`AgentGuardian` reviews with a zero-write role agent (round 211): fresh
context, explore catalog, readonly mode -- a reviewer that could mutate
the workspace could approve its own tracks.
"""

from __future__ import annotations

import re
from typing import Protocol, runtime_checkable

__all__ = ["Guardian", "AgentGuardian", "broker_reviewer"]


@runtime_checkable
class Guardian(Protocol):
    async def review(
        self, *, tool: str, rule: str, message: str,
        input_preview: str, session_id: str,
    ) -> tuple[bool, str] | None:
        """(verdict, reason), or None to leave the request to the human."""
        ...


_VERDICT = re.compile(r"\b(ALLOW|DENY)\b")

_REVIEW_PROMPT = """An agent requests approval for a potentially dangerous \
action. Judge ONLY whether this specific action should proceed. You cannot \
grant new powers, change the sandbox, or widen permissions -- you answer the \
same allow/deny a human reviewer would.

Tool: {tool}
Rule that flagged it: {rule} -- {message}
Arguments (already redacted): {input_preview}

Reply with a single line: ALLOW or DENY, followed by a one-sentence reason. \
If you cannot judge confidently, reply DEFER and the human will decide.
"""


class AgentGuardian:
    """Review with a round-211 zero-write role agent.

    Fresh context, explore catalog, readonly mode: a reviewer that could
    mutate the workspace could approve its own tracks. The verdict is
    parsed from the agent's reply; an unparseable answer -- or DEFER, or
    any exception -- returns None so the request falls to the human, the
    stricter answerer.
    """

    def __init__(self, parent) -> None:
        self._parent = parent

    async def review(
        self, *, tool: str, rule: str, message: str,
        input_preview: str, session_id: str,
    ) -> tuple[bool, str] | None:
        from .verified_roles import readonly_role_agent

        reviewer = readonly_role_agent(
            self._parent, "auditor",
            system="You are a security reviewer. You may read to inform your "
                   "judgment but never modify anything.",
        )
        answer = await reviewer.run(_REVIEW_PROMPT.format(
            tool=tool, rule=rule, message=message,
            input_preview=input_preview,
        ))
        match = _VERDICT.search(answer or "")
        if match is None:
            return None  # DEFER or unparseable -> the human decides
        reason = (answer or "").strip().splitlines()[0][:200]
        return (match.group(1) == "ALLOW", reason)


def broker_reviewer(guardian: "Guardian"):
    """Adapt a Guardian to the round-215 `ApprovalBroker.reviewer` hook.

    The broker calls `reviewer(ctx, call, rule) -> bool | None`; a Guardian
    speaks `review(**fields) -> (verdict, reason) | None`. This bridges the
    two and drops the reason (the broker records its own event), returning
    just the bool the hook expects, or None to fall through to the human.
    A Guardian that raises is left to the broker's own containment.
    """

    import json

    async def reviewer(ctx, call, rule):
        session = getattr(ctx.agent, "state", {}).get("session")
        secrets = getattr(ctx.agent, "secrets", None)
        shown = secrets.mask_payload(call.input) if secrets is not None else call.input
        verdict = await guardian.review(
            tool=call.name,
            rule=getattr(rule, "name", str(rule)),
            message=getattr(rule, "message", ""),
            input_preview=json.dumps(shown, default=str)[:2000],
            session_id=getattr(session, "id", ""),
        )
        return None if verdict is None else verdict[0]

    return reviewer


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the guardian only answers an approval the broker "
    "already gates; enforcement (readonly review agent, fall-through to the "
    "human on any non-verdict) lives in verified_roles and ApprovalBroker."
)