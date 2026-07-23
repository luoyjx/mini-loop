"""The tool + hook extension layer -- the main seam developers build on.

A `Tool` bundles a name, a JSON schema (the contract the model sees), and a
handler. A `ToolRegistry` is the set of tools an agent can call; you add your
own with `register(...)` or the `@registry.add(...)` decorator -- no core edits.

Handlers receive a `ToolContext` first, then the model-supplied arguments:

    async def my_tool(ctx, query):       # ctx + your schema's properties
        ...
        return "result string"

`Hook`s wrap every tool call: `before_tool` can deny (return a string that
becomes the tool result) or rewrite arguments (mutate `call.input` in place);
`after_tool` can transform the output. Permissions, auditing, rate limiting,
and redaction are all just hooks.
"""

from __future__ import annotations

import inspect
from collections.abc import Callable, Iterable
from dataclasses import dataclass
from pathlib import Path
from typing import Any

# A handler is `(ctx, **input) -> str | Awaitable[str]`.
ToolHandler = Callable[..., Any]


@dataclass
class ToolCall:
    """One tool invocation the model asked for."""

    name: str
    input: dict
    id: str = ""


@dataclass
class ToolContext:
    """Everything a handler (or hook) needs, passed as the first argument.

    * `agent`     -- the running Agent (for advanced use: messages, todo, etc.)
    * `workspace` -- this session's sandboxed directory
    * `state`     -- a per-session dict for your business state (survives turns)
    * `call`      -- the current ToolCall
    """

    agent: Any
    workspace: Path
    state: dict
    call: ToolCall | None = None

    async def emit_event(self, event_type: str, **fields) -> None:
        """Push a custom event onto the session's stream (SSE/observers see it)."""
        await self.agent._send(event_type, **fields)


@dataclass
class Tool:
    name: str
    description: str
    input_schema: dict
    handler: ToolHandler
    readonly: bool = False  # advisory: True = does not mutate the workspace
    # Explicit opt-in: handlers and their hooks may run concurrently.
    # This is deliberately separate from readonly; a read can still drain or
    # mutate external state.
    parallel_safe: bool = False

    @property
    def schema(self) -> dict:
        return {"name": self.name, "description": self.description, "input_schema": self.input_schema}

    async def run(self, ctx: ToolContext, **kwargs) -> str:
        result = self.handler(ctx, **kwargs)
        if inspect.isawaitable(result):
            result = await result
        return result


class ToolRegistry:
    """An ordered, named collection of tools."""

    def __init__(self, tools: Iterable[Tool] | None = None) -> None:
        self._tools: dict[str, Tool] = {}
        for t in tools or []:
            self.register(t)

    def register(self, tool: Tool, *, replace: bool = False) -> "ToolRegistry":
        if tool.name in self._tools and not replace:
            raise ValueError(f"Tool '{tool.name}' already registered (pass replace=True to override)")
        self._tools[tool.name] = tool
        return self

    def add(
        self,
        name: str,
        description: str,
        input_schema: dict,
        *,
        readonly: bool = False,
        parallel_safe: bool = False,
        replace: bool = False,
    ):
        """Decorator form: `@registry.add("greet", "...", {...})`."""
        def deco(fn: ToolHandler) -> ToolHandler:
            self.register(
                Tool(
                    name,
                    description,
                    input_schema,
                    fn,
                    readonly=readonly,
                    parallel_safe=parallel_safe,
                ),
                replace=replace,
            )
            return fn
        return deco

    def unregister(self, name: str) -> "ToolRegistry":
        self._tools.pop(name, None)
        return self

    def get(self, name: str) -> Tool | None:
        return self._tools.get(name)

    def names(self) -> list[str]:
        return list(self._tools)

    def schemas(self) -> list[dict]:
        return [t.schema for t in self._tools.values()]

    def subset(self, names: Iterable[str]) -> "ToolRegistry":
        keep = set(names)
        return ToolRegistry([t for n, t in self._tools.items() if n in keep])

    def clone(self) -> "ToolRegistry":
        return ToolRegistry(list(self._tools.values()))

    def __contains__(self, name: str) -> bool:
        return name in self._tools

    def __len__(self) -> int:
        return len(self._tools)


class Hook:
    """Lifecycle extension points. All methods are async no-ops by default.

    `before_tool` -> return a string to DENY/short-circuit (it becomes the tool
                     result); mutate `call.input` in place to rewrite arguments;
                     return None to allow.
    `after_tool`  -> return a string to REPLACE the output; return None to keep.
    `on_user_prompt` -> return a string to rewrite the submitted prompt.
    `on_stop` -> return a continuation prompt to keep the loop running.
    """

    async def before_tool(self, ctx: ToolContext, call: ToolCall) -> str | None:
        return None

    async def after_tool(self, ctx: ToolContext, call: ToolCall, output: str) -> str | None:
        return None

    async def on_user_prompt(self, agent: Any, text: str) -> str | None:
        return None

    async def on_stop(self, agent: Any, messages: list[dict], last_text: str) -> str | None:
        return None


class Hooks:
    """An ordered hook chain.

    Hooks are shared across concurrent sessions and parallel-safe tool calls,
    so custom hooks must be stateless or guard their own state.
    """

    def __init__(self, hooks: Iterable[Hook] | None = None) -> None:
        self._hooks: list[Hook] = list(hooks or [])

    def add(self, hook: Hook) -> "Hooks":
        self._hooks.append(hook)
        return self

    def __len__(self) -> int:
        return len(self._hooks)

    async def before_tool(self, ctx: ToolContext, call: ToolCall) -> str | None:
        for h in self._hooks:
            decision = await h.before_tool(ctx, call)
            if decision is not None:
                return decision  # first hook to object wins
        return None

    async def after_tool(self, ctx: ToolContext, call: ToolCall, output: str) -> str:
        for h in self._hooks:
            replaced = await h.after_tool(ctx, call, output)
            if replaced is not None:
                output = replaced
        return output

    async def user_prompt(self, agent: Any, text: str) -> str:
        for h in self._hooks:
            replaced = await h.on_user_prompt(agent, text)
            if replaced is not None:
                text = str(replaced)
        return text

    async def stop(self, agent: Any, messages: list[dict], last_text: str) -> str | None:
        for h in self._hooks:
            continuation = await h.on_stop(agent, messages, last_text)
            if continuation is not None:
                return str(continuation)
        return None
