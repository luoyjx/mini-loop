"""Pluggable context-compaction strategy (s08).

The agent calls `compactor.maybe_compact(agent)` at the top of every loop pass
and `compactor.compact(agent)` for an explicit `compress`. Swap in your own
`Compactor` to change *what* gets dropped or *how* history is summarized
(e.g. keep a rolling summary, store transcripts in S3, never auto-compact).
"""

from __future__ import annotations

import json
import re
import time
from pathlib import Path
from typing import Protocol, runtime_checkable


def estimate_tokens(messages: list) -> int:
    return len(json.dumps(messages, default=str)) // 4


def _block_type(block) -> str:
    return block.get("type", "") if isinstance(block, dict) else getattr(block, "type", "")


def _message_has_tool_use(message: dict) -> bool:
    content = message.get("content")
    return isinstance(content, list) and any(_block_type(block) == "tool_use" for block in content)


def _is_tool_result_message(message: dict) -> bool:
    content = message.get("content")
    return isinstance(content, list) and any(_block_type(block) == "tool_result" for block in content)


def snip_compact(messages: list, max_messages: int = 50) -> int:
    """Remove the conversation middle without splitting tool-use/result pairs."""
    if len(messages) <= max_messages or max_messages < 4:
        return 0

    head_end = min(3, len(messages))
    if head_end < len(messages) and _message_has_tool_use(messages[head_end - 1]):
        while head_end < len(messages) and _is_tool_result_message(messages[head_end]):
            head_end += 1

    tail_budget = max(1, max_messages - head_end - 1)
    tail_start = max(head_end, len(messages) - tail_budget)
    if (tail_start > 0 and tail_start < len(messages)
            and _is_tool_result_message(messages[tail_start])
            and _message_has_tool_use(messages[tail_start - 1])):
        tail_start -= 1

    removed = tail_start - head_end
    if removed <= 0:
        return 0
    messages[:] = [
        *messages[:head_end],
        {"role": "user", "content": f"[snipped {removed} messages from conversation middle]"},
        *messages[tail_start:],
    ]
    return removed


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


def _safe_result_id(value: str) -> str:
    return re.sub(r"[^A-Za-z0-9._-]", "_", value or "tool-result")[:100]


def tool_result_budget(
    messages: list,
    workspace: Path,
    *,
    max_bytes: int = 200_000,
    preview_chars: int = 2_000,
) -> int:
    """Persist largest results until the newest result batch fits its budget."""
    if not messages:
        return 0
    content = None
    for message in reversed(messages):
        candidate = message.get("content")
        if (message.get("role") == "user" and isinstance(candidate, list)
                and any(isinstance(part, dict) and part.get("type") == "tool_result"
                        for part in candidate)):
            content = candidate
            break
    if content is None:
        return 0
    blocks = [part for part in content
              if isinstance(part, dict) and part.get("type") == "tool_result"
              and isinstance(part.get("content"), str)]
    total = sum(len(part["content"].encode("utf-8")) for part in blocks)
    if total <= max_bytes:
        return 0

    output_dir = Path(workspace) / ".task_outputs" / "tool-results"
    output_dir.mkdir(parents=True, exist_ok=True)
    persisted = 0
    for part in sorted(blocks, key=lambda item: len(item["content"]), reverse=True):
        if total <= max_bytes:
            break
        original = part["content"]
        result_id = _safe_result_id(str(part.get("tool_use_id", "tool-result")))
        path = output_dir / f"{result_id}-{int(time.time() * 1000)}.txt"
        path.write_text(original)
        preview = original[:preview_chars]
        replacement = (
            f'<persisted-output path="{path}" bytes="{len(original.encode("utf-8"))}">\n'
            f"{preview}\n</persisted-output>"
        )
        total -= len(original.encode("utf-8"))
        total += len(replacement.encode("utf-8"))
        part["content"] = replacement
        persisted += 1
    return persisted


@runtime_checkable
class Compactor(Protocol):
    async def maybe_compact(self, agent) -> None: ...
    async def compact(self, agent) -> None: ...


class DefaultCompactor:
    """Four ordered layers: result budget, snip, micro, LLM summary."""

    def __init__(self, token_threshold: int | None = None, *, max_messages: int = 50,
                 result_budget: int = 200_000) -> None:
        self.token_threshold = token_threshold
        self.max_messages = max_messages
        self.result_budget = result_budget

    async def maybe_compact(self, agent) -> None:
        persisted = tool_result_budget(
            agent.messages, agent.workspace, max_bytes=self.result_budget
        )
        if persisted:
            await agent._send("compact", kind="budget", persisted=persisted)
        snipped = snip_compact(agent.messages, self.max_messages)
        if snipped:
            await agent._send("compact", kind="snip", removed=snipped)
        cleared = microcompact(agent.messages)
        if cleared:
            await agent._send("compact", kind="micro", cleared=cleared)
        threshold = self.token_threshold or agent.settings.token_threshold
        if estimate_tokens(agent.messages) > threshold:
            await self.compact(agent)

    async def compact(self, agent) -> None:
        transcript_dir = agent.workspace / ".transcripts"
        transcript_dir.mkdir(parents=True, exist_ok=True)
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
