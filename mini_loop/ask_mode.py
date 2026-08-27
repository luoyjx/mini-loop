"""Ask mode: a human-set Q&A posture, log-only state, hard-enforced.

The second interaction posture beside plan mode, with the opposite
enforcement choice, deliberately:

* Plan mode is the MODEL's collaboration state -- it enters and leaves it
  mid-turn, so it is soft guidance: what the model is told, never what it
  is allowed (plan_mode.py).
* Ask mode is the HUMAN's declaration that this session answers questions
  and changes nothing. The model cannot leave it, so telling is not
  enough: `PermissionHook` refuses mutating-risk calls outright while it
  is active, the way a readonly session does, with the mode named in the
  refusal (permissions.py reads `ask_mode` on purpose -- the constructive
  "enforcement never reads it" guarantee is plan mode's, not this one's).

State follows plan_mode's log-only pattern: flips are durable session
events, the value in force is the last logged one, restore recovers it by
folding the log. A restored ask session therefore STAYS ask -- unlike
`permission_mode` (runtime-only, restored to `interactive`), because
"comes back asking" is fail-safe and "comes back mutating" is not.
"""

from __future__ import annotations

from typing import Any

__all__ = ["ASK_SECTION", "ask_mode_active", "set_ask_mode", "fold_ask_mode"]

ASK_SECTION = (
    "Ask mode is ACTIVE. Answer questions, explain, and investigate with "
    "read-only tools; do not modify files, run commands with side effects, "
    "or act outside this machine -- mutating tools are refused while ask "
    "mode is on. Only the human can switch the session back to agent mode; "
    "if the answer calls for changes, describe them and say so."
)


def ask_mode_active(agent: Any) -> bool:
    return bool(agent.state.get("ask_mode"))


def set_ask_mode(agent: Any, active: bool) -> str:
    """Flip the logged state; a no-op when already there.

    The runtime value in `agent.state` is a cache of the last logged event,
    maintained here and rebuilt by `fold_ask_mode` on restore -- the log
    stays the authority.
    """

    if ask_mode_active(agent) == active:
        return "noop"
    agent.state["ask_mode"] = active
    agent.refresh_system()
    return "committed"


def fold_ask_mode(events: list[dict]) -> bool:
    """The state in force is the last logged value; absent means off."""

    active = False
    for event in events:
        if event.get("type") == "ask_mode":
            active = bool(event.get("active"))
    return active


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: log-only whole-value state whose fold is pinned by restore tests; enforcement lives in permissions.py and is pinned there."
)
