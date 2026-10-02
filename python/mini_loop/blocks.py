"""Reading a content block, whichever shape it arrived in.

A response's content is provider *objects*. `_content_payload` turns them into
dicts for the transcript. `ContinuedResponse` hands back dicts as a response.
Both shapes reach the same code, and every reader that assumed one has been a
bug -- five times now, in five different subsystems:

* the event stream emitted objects a JSON encoder could not serialize,
* the durable tables kept a credential because a masker walked dicts straight
  past objects,
* compaction could not clear a tool result it did not recognize as one,
* continuation returned an empty answer when the extractor read `.text` off a
  dict,
* and the compaction *summary* came back empty when its own request was
  truncated -- replacing an entire transcript with a file path and nothing else.

The fourth fix normalized at the one place blocks were written, which did not
hold: it addressed the writer and left every reader shape-specific. This module
is the readers' half. Nothing outside it should reach into a block by attribute
or by key.
"""

from __future__ import annotations

from typing import Any

__all__ = ["block_field", "block_text", "blocks_of_type"]


def block_field(block: Any, field: str, default: Any = None) -> Any:
    """One field of a content block, from either a dict or a provider object."""

    if isinstance(block, dict):
        return block.get(field, default)
    return getattr(block, field, default)


def block_text(content: Any) -> str:
    """Joined text of every ``text`` block in a response's content.

    The single most repeated read in the package -- the agent's reply, the
    compaction summary, the memory extraction -- and each copy was written by
    attribute, so each broke the moment a response could carry dicts.
    """

    if not content:
        return ""
    return "".join(
        block_field(block, "text", "") or ""
        for block in content
        if block_field(block, "type", "") == "text"
    )


def blocks_of_type(content: Any, kind: str) -> list:
    """Every block of one type, shape-agnostically."""

    if not content:
        return []
    return [block for block in content if block_field(block, "type", "") == kind]

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: pure readers over provider content blocks; stateless code has no mutable data for an invariant to watch."
)
