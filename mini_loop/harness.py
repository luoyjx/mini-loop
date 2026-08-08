"""The swappable policy set an agent runs under, as one value.

Every capability around the loop is a seam, and until now each seam was a
separate keyword argument threaded to each of the three places an `Agent` is
constructed:

* `SessionManager._build_agent` -- the blessed path,
* `Agent._run_subagent` -- delegation,
* `workflows.FreshAgentRunner` -- workflow workers.

Each site kept its own list of what to pass. Adding a seam meant remembering all
three, and over five added seams two sites were missed: workflow workers ran
without secret masking or sandboxing, because they were constructed directly
rather than through the manager. Nothing failed loudly; the capability was
simply absent on one path.

The fix is the shape OpenHands uses for `AgentBase`: configuration is a **value
object**, not a parameter list. Deriving a variant copies the whole value and
overrides the fields that differ, so a seam added tomorrow reaches every
construction site the moment it is added to this dataclass. You cannot forget a
field you never had to type.

`None` means "the Agent's own default" -- the harness records policy choices,
not their fallbacks, so an unset field stays unset rather than freezing today's
default into every derived agent.
"""

from __future__ import annotations

import dataclasses
from collections.abc import Callable, Sequence
from dataclasses import dataclass, field
from typing import Any

__all__ = ["Harness"]


@dataclass(frozen=True, slots=True)
class Harness:
    """One agent's policy set. Immutable; derive variants with `derive`."""

    tools: Any = None
    transport: Any = None
    hooks: Any = None
    skills: Any = None
    system_builder: Callable[[Any], str] | None = None
    compactor: Any = None
    recovery: Any = None
    stuck_detector: Any = None
    cache_policy: Any = None
    secrets: Any = None
    sandbox: Any = None
    injectors: Sequence[Any] = field(default_factory=tuple)

    def derive(self, **overrides: Any) -> "Harness":
        """Copy this harness with specific fields replaced.

        Used wherever a child agent needs a narrower tool set or its own hooks:
        starting from a complete value means the child inherits every seam the
        parent had, including ones added after the call site was written.
        """

        unknown = set(overrides) - {f.name for f in dataclasses.fields(self)}
        if unknown:
            raise TypeError(f"Harness has no field(s): {sorted(unknown)}")
        return dataclasses.replace(self, **overrides)

    def resolve(self, name: str, explicit: Any) -> Any:
        """Return `explicit` when the caller passed one, else the harness value.

        Per-argument overrides stay supported so existing call sites and tests
        keep working; the harness only supplies what the caller left out.
        """

        return explicit if explicit is not None else getattr(self, name)
