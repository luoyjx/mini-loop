"""Prompt-cache breakpoint placement, and the message-stream reminder that
keeps the cached prefix intact.

A provider renders a request as ``tools -> system -> messages`` and caches it by
**prefix match**: the cache key is the exact bytes up to each ``cache_control``
breakpoint, so one changed byte at position N invalidates every breakpoint at or
after N. Two consequences drive this module:

1. **Volatile state must not live in the prefix.** mini-loop's system prompt
   used to carry the TodoWrite board and the memory index, both of which change
   as the agent works -- so every turn invalidated the whole conversation.
   ``prompts.runtime_facts`` now returns that state and
   ``runtime_facts_injector`` appends it to the *end* of the message stream,
   which invalidates nothing before it. The reminder is re-sent only when its
   content actually changes, so the transcript does not grow a near-duplicate
   block every round.

2. **A breakpoint only looks back so far.** Each breakpoint walks back a bounded
   number of content blocks to find a prior cache entry; if a single turn adds
   more blocks than that window, the next request's breakpoint finds nothing and
   silently misses. mini-loop executes a *batch* of tool calls per round -- one
   assistant message with N ``tool_use`` blocks plus one user message with N
   ``tool_result`` blocks -- so a parallel-heavy round blows past the window
   easily. ``DefaultCachePolicy`` therefore lays down intermediate breakpoints
   at a stride inside it, rather than only marking the newest turn.

Nothing here mutates ``agent.messages``: annotation happens on shallow copies
built per request, so the durable history stays free of provider-specific keys.
"""

from __future__ import annotations

from collections.abc import Sequence
from typing import Any, Protocol

__all__ = [
    "CACHE_TTL_DEFAULT",
    "MAX_BREAKPOINTS",
    "LOOKBACK_BLOCKS",
    "BREAKPOINT_STRIDE",
    "CachePolicy",
    "NullCachePolicy",
    "DefaultCachePolicy",
    "runtime_facts_injector",
    "RUNTIME_FACTS_OPEN",
]

# Providers cap the number of cache breakpoints per request; one is spent on the
# tools+system prefix, leaving the rest for the conversation.
MAX_BREAKPOINTS = 4
# How far back a breakpoint searches for an existing entry.
LOOKBACK_BLOCKS = 20
# Stay comfortably inside the lookback window so a batch-heavy round cannot
# push the previous breakpoint out of range.
BREAKPOINT_STRIDE = 15
CACHE_TTL_DEFAULT = None  # provider default (short-lived)

RUNTIME_FACTS_OPEN = "<runtime-state>"
RUNTIME_FACTS_CLOSE = "</runtime-state>"
_RUNTIME_FACTS_KEY = "runtime_facts_sent"


def _cache_control(ttl: str | None) -> dict[str, Any]:
    control: dict[str, Any] = {"type": "ephemeral"}
    if ttl:
        control["ttl"] = ttl
    return control


def _block_count(message: Any) -> int:
    """Count the content blocks a message contributes to the lookback window."""

    content = message.get("content") if isinstance(message, dict) else None
    if isinstance(content, str):
        return 1
    if isinstance(content, Sequence):
        return len(content)
    return 1


def _annotatable(message: Any) -> bool:
    """True when this message's last block can carry a breakpoint.

    Assistant turns are stored as provider block objects rather than dicts --
    they round-trip through the SDK untouched, and are not ours to rewrite. User
    turns (the prompt, and every tool-result batch) are plain dicts, and one is
    always the final message when a request is built, so restricting placement
    to them costs no coverage.
    """

    if not isinstance(message, dict) or message.get("role") != "user":
        return False
    content = message.get("content")
    return isinstance(content, list) and bool(content) and isinstance(content[-1], dict)


class CachePolicy(Protocol):
    """Decide where a request's cache breakpoints go."""

    def annotate(
        self,
        *,
        system: Any,
        tools: Any,
        messages: list,
    ) -> tuple[Any, Any, list]: ...


class NullCachePolicy:
    """Place no breakpoints. The behaviour before caching existed."""

    def annotate(self, *, system: Any, tools: Any, messages: list):
        return system, tools, messages


class DefaultCachePolicy:
    """One breakpoint on tools+system, then a strided walk back through turns."""

    def __init__(
        self,
        *,
        ttl: str | None = CACHE_TTL_DEFAULT,
        max_breakpoints: int = MAX_BREAKPOINTS,
        stride: int = BREAKPOINT_STRIDE,
    ) -> None:
        if max_breakpoints < 1:
            raise ValueError("max_breakpoints must be at least 1")
        if not 0 < stride <= LOOKBACK_BLOCKS:
            raise ValueError(
                f"stride must be between 1 and the {LOOKBACK_BLOCKS}-block lookback"
            )
        self.ttl = ttl
        self.max_breakpoints = max_breakpoints
        self.stride = stride

    def annotate(self, *, system: Any, tools: Any, messages: list):
        control = _cache_control(self.ttl)
        cached_system = self._annotate_system(system, control)
        # A breakpoint on the last system block covers tools too: they render
        # first, so they are inside the same prefix.
        budget = self.max_breakpoints - (1 if cached_system is not system else 0)
        cached_messages = self._annotate_messages(messages, control, budget)
        return cached_system, tools, cached_messages

    def _annotate_system(self, system: Any, control: dict) -> Any:
        if isinstance(system, str):
            if not system:
                return system
            return [{"type": "text", "text": system, "cache_control": control}]
        if isinstance(system, list) and system and isinstance(system[-1], dict):
            blocks = [dict(block) for block in system]
            blocks[-1]["cache_control"] = control
            return blocks
        return system

    def _annotate_messages(self, messages: list, control: dict, budget: int) -> list:
        if budget <= 0 or not messages:
            return messages
        targets = self._breakpoint_positions(messages, budget)
        if not targets:
            return messages
        annotated = list(messages)
        by_message: dict[int, list[int]] = {}
        for message_index, block_index in targets:
            by_message.setdefault(message_index, []).append(block_index)
        for message_index, block_indices in by_message.items():
            message = dict(annotated[message_index])
            content = [dict(block) for block in message["content"]]
            for block_index in block_indices:
                content[block_index]["cache_control"] = control
            message["content"] = content
            annotated[message_index] = message
        return annotated

    def _breakpoint_positions(
        self,
        messages: list,
        budget: int,
    ) -> list[tuple[int, int]]:
        """Walk back block by block, marking roughly every ``stride`` blocks.

        Placement is per *block*, not per message: one batched round can add
        more blocks than the lookback window, so marking only the last block of
        a turn would leave the next request's breakpoint out of range. The
        newest annotatable block is always marked, so the current request's
        prefix is written no matter what.

        **Known limit.** A round wider than the lookback window cannot be fully
        chained within the breakpoint budget -- an assistant turn's `tool_use`
        blocks are provider objects and cannot carry a breakpoint, so a batch of
        N tools contributes N unmarkable blocks. The newest entry is still
        written, and entries written by earlier requests stay readable, so the
        cache degrades rather than breaking.
        """

        chosen: list[tuple[int, int]] = []
        run = 0
        for message_index in range(len(messages) - 1, -1, -1):
            if len(chosen) >= budget:
                break
            message = messages[message_index]
            if not _annotatable(message):
                run += _block_count(message)
                continue
            content = message["content"]
            for block_index in range(len(content) - 1, -1, -1):
                if len(chosen) >= budget:
                    break
                if not isinstance(content[block_index], dict):
                    run += 1
                    continue
                if not chosen or run >= self.stride:
                    chosen.append((message_index, block_index))
                    run = 0
                else:
                    run += 1
        chosen.reverse()
        return chosen


async def runtime_facts_injector(agent) -> list | None:
    """Deliver changed runtime state as a message, not as a prefix edit.

    Returns a user message carrying the current TodoWrite / memory state the
    first time it appears and on every later change. When nothing changed there
    is nothing to send, which keeps both the transcript and the cached prefix
    stable across turns.
    """

    from .prompts import runtime_facts

    facts = runtime_facts(agent)
    if facts == agent.state.get(_RUNTIME_FACTS_KEY, ""):
        return None
    agent.state[_RUNTIME_FACTS_KEY] = facts
    if not facts:
        return None
    # Plain string content, matching every other injector in the codebase --
    # the loop and the offline fake client both branch on that shape.
    return [
        {
            "role": "user",
            "content": f"{RUNTIME_FACTS_OPEN}\n{facts}\n{RUNTIME_FACTS_CLOSE}",
        }
    ]
