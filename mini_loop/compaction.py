"""Pluggable context-compaction strategy (s08).

The agent calls `compactor.maybe_compact(agent)` at the top of every loop pass
and `compactor.compact(agent)` for an explicit `compress`. Swap in your own
`Compactor` to change *what* gets dropped or *how* history is summarized
(e.g. keep a rolling summary, store transcripts in S3, never auto-compact).
"""

from __future__ import annotations

import json
import time
from typing import Protocol, runtime_checkable


def estimate_tokens(messages: list) -> int:
    return len(json.dumps(messages, default=str)) // 4


def microcompact(messages: list) -> int:
    """Blank out the body of all but the 3 most recent tool results, in place.

    Returns how many were cleared. Old tool output is the cheapest context to
    shed -- the model already acted on it.
    """
    results = [
        part
        for msg in messages
        if msg["role"] == "user" and isinstance(msg.get("content"), list)
        for part in msg["content"]
        if isinstance(part, dict) and part.get("type") == "tool_result"
    ]
    cleared = 0
    for part in results[:-3]:
        if isinstance(part.get("content"), str) and len(part["content"]) > 100:
            part["content"] = "[cleared]"
            cleared += 1
    return cleared


@runtime_checkable
class Compactor(Protocol):
    async def maybe_compact(self, agent) -> None: ...
    async def compact(self, agent) -> None: ...


class DefaultCompactor:
    """micro-compact every pass; full auto-compact once history crosses the
    token threshold (or on explicit `compress`)."""

    def __init__(self, token_threshold: int | None = None) -> None:
        self.token_threshold = token_threshold

    async def maybe_compact(self, agent) -> None:
        cleared = microcompact(agent.messages)
        if cleared:
            await agent._send("compact", kind="micro", cleared=cleared)
        threshold = self.token_threshold or agent.settings.token_threshold
        if estimate_tokens(agent.messages) > threshold:
            await self.compact(agent)

    async def compact(self, agent) -> None:
        transcript_dir = agent.workspace / ".transcripts"
        transcript_dir.mkdir(exist_ok=True)
        path = transcript_dir / f"transcript_{int(time.time() * 1000)}.jsonl"
        with open(path, "w") as f:
            for msg in agent.messages:
                f.write(json.dumps(msg, default=str) + "\n")

        conv = json.dumps(agent.messages, default=str)[-80_000:]
        resp = await agent._create(
            [{"role": "user", "content": f"Summarize this agent session for continuity:\n{conv}"}],
            max_tokens=2000,
        )
        summary = "".join(getattr(b, "text", "") for b in resp.content if getattr(b, "type", "") == "text")
        agent.messages[:] = [
            {"role": "user", "content": f"[Context compressed. Full transcript: {path}]\n{summary}"}
        ]
        await agent._send("compact", kind="auto", transcript=str(path))
