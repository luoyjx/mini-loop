"""A deterministic, offline stand-in for `anthropic.AsyncAnthropic`.

It mimics exactly the surface the agent loop touches:

    resp = await client.messages.create(model=, messages=, tools=, ...)
    resp.content       # list of blocks with .type / .text  or  .type/.name/.id/.input
    resp.stop_reason   # "tool_use" | "end_turn"

Two uses:
  * the FastAPI server boots with it when MINILOOP_FAKE_LLM=1, so the whole
    thing can be curled with no API key;
  * tests inject a custom `responder` to script precise tool-call sequences.

The default responder runs one `bash echo` then summarizes -- enough to drive
the loop, tool dispatch, sandboxing, and event stream end to end.
"""

from __future__ import annotations

import asyncio
import os
from collections.abc import Callable


class TextBlock:
    type = "text"

    def __init__(self, text: str) -> None:
        self.text = text

    def __repr__(self) -> str:
        return f"TextBlock({self.text!r})"


class ToolUseBlock:
    type = "tool_use"

    def __init__(self, name: str, input: dict, id: str) -> None:
        self.name = name
        self.input = input
        self.id = id

    def __repr__(self) -> str:
        return f"ToolUseBlock({self.name!r}, {self.input!r})"


class FakeMessage:
    def __init__(self, content: list, stop_reason: str) -> None:
        self.content = content
        self.stop_reason = stop_reason


# --- block helpers (handy when scripting a responder in tests) -------------

def text(s: str) -> TextBlock:
    return TextBlock(s)


def tool(name: str, /, _id: str = "toolu_x", **input) -> ToolUseBlock:
    # `name` is positional-only so a tool's own `name` input field can still be
    # passed as a kwarg, e.g. tool("greet", name="World").
    return ToolUseBlock(name, input, _id)


def _last_result_text(content: list) -> str:
    for part in content:
        if isinstance(part, dict) and part.get("type") == "tool_result":
            return str(part.get("content", ""))[:200]
    return ""


def default_responder(kwargs: dict) -> tuple[list, str]:
    """One bash echo, then a summary. Stateless: keyed off the last message."""
    tools = kwargs.get("tools")
    messages = kwargs["messages"]
    last = messages[-1]

    # No tools => this is the auto-compaction summarization call.
    if not tools:
        return [text(f"[summary of {len(messages)} message(s)]")], "end_turn"

    # Fresh user prompt (a plain string) => take one action.
    if isinstance(last.get("content"), str):
        prompt = last["content"].replace("\n", " ")[:60]
        return [
            text("Working on it."),
            tool("bash", _id="toolu_1", command=f'echo handled: {prompt}'),
        ], "tool_use"

    # Tool results came back (a list) => wrap up.
    return [text(f"Done. Tool said: {_last_result_text(last['content'])}")], "end_turn"


def scripted(turns: list[tuple[list, str]]) -> Callable[[dict], tuple[list, str]]:
    """Build a responder that returns each (blocks, stop_reason) turn in order,
    falling back to a plain end_turn once the script is exhausted."""
    state = {"i": 0}

    def responder(kwargs: dict) -> tuple[list, str]:
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        i = state["i"]
        state["i"] += 1
        if i < len(turns):
            return turns[i]
        return [text("Done.")], "end_turn"

    return responder


class _Messages:
    def __init__(self, parent: "FakeAsyncAnthropic") -> None:
        self._parent = parent

    async def create(self, **kwargs) -> FakeMessage:
        self._parent.calls += 1
        if self._parent.delay:
            await asyncio.sleep(self._parent.delay)
        content, stop = self._parent.responder(kwargs)
        return FakeMessage(content, stop)


class FakeAsyncAnthropic:
    def __init__(self, responder: Callable[[dict], tuple[list, str]] | None = None,
                 delay: float | None = None) -> None:
        self.responder = responder or default_responder
        self.delay = delay if delay is not None else float(os.getenv("MINILOOP_FAKE_DELAY", "0") or 0)
        self.calls = 0
        self.messages = _Messages(self)
