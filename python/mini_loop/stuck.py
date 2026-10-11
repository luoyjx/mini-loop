"""Loop detection -- stop an agent from burning turns against the same wall.

The core loop already bounds work by ``max_rounds``, but that bound is blunt:
an agent that retries one denied tool call thirty times and an agent doing
thirty rounds of real work are indistinguishable to it.  This module detects
the *shape* of unproductive repetition and reacts before the round budget is
spent.

Four of the five rules are ported from the OpenHands SDK ``StuckDetector``
(``openhands-sdk/openhands/sdk/conversation/stuck_detector.py``, repo
``OpenHands/software-agent-sdk``), which scans a bounded window of recent
events for repeated patterns.  The fifth (``_unproductive_tool``) covers a gap
that every consecutive-and-identical detector shares -- see its docstring.
Two adaptations were required:

* OpenHands emits one action per step and reads them back off a durable event
  history.  mini-loop executes a *batch* of tool calls per round, so the agent
  records an ordered ``ToolStep`` ledger at the point where batch order is
  still known (see ``Agent._exec_tool_batch``) rather than reconstructing one
  from ``messages``, which compaction is free to rewrite.
* OpenHands halts the run on detection.  Halting a coding agent on its first
  repeat is heavy-handed, so the default policy nudges once and halts on the
  next detection.  ``max_nudges=0`` restores the upstream behaviour.

The detector itself is a *stateless policy*: history lives on the agent, so one
detector instance can be shared by a whole ``SessionManager`` the same way
``Compactor`` is.
"""

from __future__ import annotations

import hashlib
import json
from collections.abc import Sequence
from dataclasses import dataclass
from typing import Any, Protocol

__all__ = [
    "ToolStep",
    "StuckSignal",
    "StuckThresholds",
    "StuckDetector",
    "DefaultStuckDetector",
    "NullStuckDetector",
    "step_hash",
    "STUCK_WINDOW",
]

# Bounded scan window, mirroring the upstream constant.  Large enough to hold
# the longest pattern (alternating: 6 pairs) with room for interleaved calls.
STUCK_WINDOW = 20


def step_hash(value: Any) -> str:
    """Hash a tool input or output into a comparison key."""

    try:
        encoded = json.dumps(value, sort_keys=True, ensure_ascii=False, default=str)
    except (TypeError, ValueError):
        encoded = str(value)
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()[:16]


@dataclass(frozen=True, slots=True)
class ToolStep:
    """One executed tool call, reduced to what loop detection compares.

    Ids, spans, and durations are deliberately excluded: like the upstream
    ``_event_eq``, two calls are "the same" when the model asked for the same
    thing and got the same thing back.
    """

    name: str
    input_hash: str
    output_hash: str
    failed: bool = False
    denied: bool = False

    @property
    def unproductive(self) -> bool:
        """True when the call produced no forward progress by construction."""

        return self.failed or self.denied

    def same_call(self, other: "ToolStep") -> bool:
        return self.name == other.name and self.input_hash == other.input_hash

    def same_outcome(self, other: "ToolStep") -> bool:
        return self.same_call(other) and self.output_hash == other.output_hash


_DEFAULT_ADVICE = (
    "Stop repeating this call. Either change your approach, use a different "
    "tool, or explain to the user why you cannot proceed."
)


@dataclass(frozen=True, slots=True)
class StuckSignal:
    """A detected unproductive pattern."""

    pattern: str
    detail: str
    tool_name: str | None = None
    advice: str = _DEFAULT_ADVICE

    def reminder(self) -> str:
        """The corrective text spliced into the conversation on a nudge."""

        return f'<stuck pattern="{self.pattern}">{self.detail} {self.advice}</stuck>'


@dataclass(frozen=True, slots=True)
class StuckThresholds:
    """Repeat counts before each pattern fires.

    Defaults match the upstream ``StuckDetectionThresholds`` (4 / 3 / 6 / 3).
    """

    repeat_action_result: int = 4
    repeat_action_error: int = 3
    alternating: int = 6
    monologue: int = 3
    # Unproductive uses of one tool inside the window, with no successful use
    # of it in between. Unlike the rules above this needs neither consecutive
    # calls nor identical inputs -- see ``_unproductive_tool``.
    unproductive_tool: int = 5
    # How many corrective nudges to spend before halting the turn. 0 halts on
    # first detection, which is what OpenHands does.
    max_nudges: int = 1


class StuckDetector(Protocol):
    """Inspect an agent's recent activity for unproductive repetition."""

    def inspect(self, agent) -> StuckSignal | None: ...


class NullStuckDetector:
    """Detect nothing. The pre-existing behaviour, kept explicit."""

    max_nudges = 0

    def inspect(self, agent) -> StuckSignal | None:  # noqa: ARG002
        return None


class DefaultStuckDetector:
    """The four upstream patterns, adapted to batched tool calls."""

    def __init__(self, thresholds: StuckThresholds | None = None) -> None:
        self.thresholds = thresholds or StuckThresholds()

    @property
    def max_nudges(self) -> int:
        return self.thresholds.max_nudges

    def inspect(self, agent) -> StuckSignal | None:
        steps: Sequence[ToolStep] = tuple(getattr(agent, "recent_steps", ()))
        monologue = self._monologue(int(getattr(agent, "rounds_without_tools", 0)))
        if monologue is not None:
            return monologue
        return (
            self._repeat_action_error(steps)
            or self._unproductive_tool(steps)
            or self._repeat_action_result(steps)
            or self._alternating(steps)
        )

    # -- scenario 1: identical call, identical result -----------------------
    def _repeat_action_result(self, steps: Sequence[ToolStep]) -> StuckSignal | None:
        n = self.thresholds.repeat_action_result
        if len(steps) < n:
            return None
        window = steps[-n:]
        if not all(window[0].same_outcome(step) for step in window):
            return None
        return StuckSignal(
            pattern="repeat_action_result",
            detail=(
                f"The last {n} tool calls were all `{window[0].name}` with identical "
                "input and identical output."
            ),
            tool_name=window[0].name,
        )

    # -- scenario 2: identical call, repeated failure or denial -------------
    def _repeat_action_error(self, steps: Sequence[ToolStep]) -> StuckSignal | None:
        n = self.thresholds.repeat_action_error
        if len(steps) < n:
            return None
        window = steps[-n:]
        if not all(window[0].same_call(step) for step in window):
            return None
        if not all(step.unproductive for step in window):
            return None
        kind = "denied" if all(step.denied for step in window) else "failed"
        return StuckSignal(
            pattern="repeat_action_error",
            detail=(
                f"The last {n} tool calls were all `{window[0].name}` with identical "
                f"input and every one of them {kind}."
            ),
            tool_name=window[0].name,
        )

    # -- one tool that never works, however it is called --------------------
    def _unproductive_tool(self, steps: Sequence[ToolStep]) -> StuckSignal | None:
        """Catch a tool that keeps failing when the repeat rules cannot see it.

        Every consecutive-and-identical rule -- ours, the OpenHands SDK's,
        Cline's ``LoopDetectionTracker``, opencode's doom-loop check -- is
        blind to a model that varies its arguments or interleaves other calls.
        Cline pairs its loop detector with a ``MistakeTracker`` for exactly
        that reason, but that counter resets on any success, and a measured
        trace of this harness showed the miss precisely there: the model
        alternated a denied call with a *succeeding* workaround call, so no
        reset-on-success counter ever accumulated.

        So this rule is scoped per tool and ignores ordering entirely: N
        unproductive uses of one tool inside the window with no successful use
        of that same tool. A tool that sometimes works is a flaky tool, not a
        stuck agent, and does not fire.
        """

        n = self.thresholds.unproductive_tool
        if n <= 0 or len(steps) < n:
            return None
        bad: dict[str, int] = {}
        good: set[str] = set()
        for step in steps:
            if step.unproductive:
                bad[step.name] = bad.get(step.name, 0) + 1
            else:
                good.add(step.name)
        for name, count in bad.items():
            if count >= n and name not in good:
                return StuckSignal(
                    pattern="unproductive_tool",
                    detail=(
                        f"`{name}` failed or was denied {count} times in the last "
                        f"{len(steps)} tool calls and never once succeeded, however "
                        "it was called."
                    ),
                    tool_name=name,
                    advice=(
                        f"`{name}` is not going to start working. Do not call it "
                        "again with different arguments. Either reach the goal "
                        "another way, or tell the user what is blocking you."
                    ),
                )
        return None

    # -- scenario 4: two calls ping-ponging ---------------------------------
    def _alternating(self, steps: Sequence[ToolStep]) -> StuckSignal | None:
        n = self.thresholds.alternating
        if n < 4 or len(steps) < n:
            return None
        window = steps[-n:]
        # A/B/A/B... -- every step matches the one two positions later.
        if not all(window[i].same_outcome(window[i + 2]) for i in range(n - 2)):
            return None
        # An all-identical run is scenario 1, not a ping-pong.
        if window[0].same_outcome(window[1]):
            return None
        pair = f"`{window[0].name}` and `{window[1].name}`"
        return StuckSignal(
            pattern="alternating",
            detail=(
                f"The last {n} tool calls alternated between {pair} with identical "
                "inputs and identical outputs each cycle."
            ),
            tool_name=window[0].name,
        )

    # -- scenario 3: the model talking to itself ----------------------------
    def _monologue(self, rounds_without_tools: int) -> StuckSignal | None:
        n = self.thresholds.monologue
        if rounds_without_tools < n:
            return None
        return StuckSignal(
            pattern="monologue",
            detail=(
                f"The model produced {rounds_without_tools} consecutive turns with no "
                "tool call, and a stop hook keeps resuming it."
            ),
        )

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the detector is itself the observer of agent progress; an invariant about the observer would watch the watcher."
)
