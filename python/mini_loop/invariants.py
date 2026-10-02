"""Runtime invariants: package-owned checks that fail loud and attributed.

DeepSeek Harness's `ctx.invariants` registry establishes two rules this
module adopts:

* an invariant asserts **authoritative event streams or mutable data**,
  never the mere presence of a service or method -- presence checks pass
  vacuously and rot silently;
* a violation is **attributed to the module that owns the contract**, so
  the failure message names who broke their own rule rather than where the
  stack happened to be.

The three existing verification instruments (`tools/verify_guards.py`,
`tools/verify_scans.py`, `tests/test_timing_safety.py`) are all static or
test-time; this is the runtime counterpart. The first resident is the
"model-visible means logged" transcript invariant (session.py).
"""

from __future__ import annotations

__all__ = ["InvariantError"]


class InvariantError(AssertionError):
    """A runtime invariant was violated.

    `owner` is the module that owns the violated contract, in dotted form
    (e.g. ``mini_loop.session``): the point is that the report attributes
    the break to a package rule, not to the call stack that tripped it.
    Subclasses ``AssertionError`` so test harnesses and defensive callers
    that treat assertion failures as fatal keep doing so.
    """

    def __init__(self, owner: str, message: str) -> None:
        self.owner = owner
        super().__init__(f"invariant violated by {owner!r}: {message}")

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the reporting vocabulary itself; a registry asserting on its own error type would be circular."
)
