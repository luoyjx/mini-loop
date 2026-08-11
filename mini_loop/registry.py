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

import hashlib
import inspect
import json
from collections.abc import Callable, Iterable
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .problems import ProblemLog
from .run_context import RunContext

#: Total characters of tool definitions allowed in one request. Sized so a
#: large MCP fleet still leaves most of the window for the conversation --
#: roughly 15,000 tokens, against the ~2,100 the 37 built-ins use.
MAX_TOOL_PAYLOAD = 60_000

#: Description lengths tried in order before any tool is dropped. A tool with
#: a short description is still callable; an absent one is not.
TOOL_DESCRIPTION_STEPS = (1_000, 400, 200, 80)


def _payload_size(schemas: list[dict]) -> int:
    return len(json.dumps(schemas, default=str))


#: The risk ladder a tool declares on its contract, least to most consequential.
#: `read` inspects, `write` mutates local/session state, `exec` runs code or
#: spawns agents that can, `external` leaves the machine. The permission layer
#: executes this -- a declaration nothing executes is documentation, not a
#: contract (rounds 92/94) -- and a tool with *no* declaration is treated as
#: `external`, not `read`: OpenWorker's own review flags its fall-back-to-READ
#: as a standing hazard (OPENWORKER_RESEARCH.md 9.2.8), and the inversion is
#: the fix. Unclassified gates upward, never downward.
RISK_LEVELS = ("read", "write", "exec", "external")


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
    * `run_context` -- immutable provenance/authority for the current run
    * `action_id` -- unique identifier for this tool execution
    """

    agent: Any
    workspace: Path
    state: dict
    call: ToolCall | None = None
    run_context: RunContext | None = None
    action_id: str | None = None

    async def emit_event(self, event_type: str, **fields) -> None:
        """Push a custom event onto the session's stream (SSE/observers see it)."""
        await self.agent._send(event_type, **fields)


VerifyFn = Callable[["ToolContext", "ToolCall"], object]


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
    #
    # The reverse direction is the one that bites. `parallel_safe` **and not**
    # `readonly` claims "I mutate the workspace and I am safe to run alongside
    # other tools" -- sometimes true (a tool whose writes go somewhere with its
    # own concurrency control), and a lost update when it is not. The harness
    # cannot check the claim, so it does not reject it; the audit reports it,
    # because the one outcome ruled out is that it stays silent.
    parallel_safe: bool = False
    #: Where this tool sits on `RISK_LEVELS`. `None` means unclassified, and
    #: unclassified is gated like `external` -- see `RISK_LEVELS`.
    risk: str | None = None
    #: Optional reconciler: `async (ctx, call) -> bool | None`, answering
    #: "did this call already take effect?" -- `True` it did, `False` it did
    #: not, `None` cannot tell.
    #:
    #: Only consulted when a crash left an action `unknown`: the tool was
    #: dispatched and nobody recorded the outcome. Without a verifier the
    #: harness refuses to guess, which is correct but leaves the agent stuck;
    #: with one it can find out. This is the concrete payoff of promoting an
    #: action out of `bash` into a dedicated tool -- an opaque command string
    #: cannot be checked, a typed call often can.
    verify: VerifyFn | None = None
    #: Stable capability names used when a child agent receives a role-specific
    #: subset of its parent's tools. Empty means "do not inherit by capability".
    capabilities: frozenset[str] = field(default_factory=frozenset)

    def __post_init__(self) -> None:
        # Accept any iterable at the public construction boundary, then keep
        # one immutable representation for role-policy decisions.
        self.capabilities = frozenset(self.capabilities)

    @property
    def schema(self) -> dict:
        return {"name": self.name, "description": self.description, "input_schema": self.input_schema}

    async def already_took_effect(self, ctx: ToolContext, call: ToolCall):
        """Ask the reconciler whether this call already landed.

        Returns `None` when there is no verifier, when it cannot tell, or when
        it fails -- all three mean the same thing to the caller: still unknown,
        so still do not retry.
        """

        if self.verify is None:
            return None
        try:
            verdict = self.verify(ctx, call)
            if inspect.isawaitable(verdict):
                verdict = await verdict
        except Exception:
            # A reconciler that breaks must not turn "unknown" into "no".
            return None
        return verdict if isinstance(verdict, bool) else None

    async def run(self, ctx: ToolContext, **kwargs) -> str:
        result = self.handler(ctx, **kwargs)
        if inspect.isawaitable(result):
            result = await result
        return result


@dataclass(frozen=True, slots=True)
class ToolCatalogSnapshot:
    """One immutable, byte-stable view of the tools sent on a model request.

    The JSON payload is stored rather than a tuple of mutable dictionaries. A
    caller gets a fresh decoded list from :meth:`schemas`, so neither cache
    annotation nor a provider SDK can mutate the snapshot shared with the
    system-prompt builder.
    """

    revision: int
    fingerprint: str
    schema_json: str
    sent_names: tuple[str, ...]
    omitted_names: tuple[str, ...]
    trimmed_to: int | None
    inventory_count: int

    def schemas(self) -> list[dict]:
        return json.loads(self.schema_json)


class ToolRegistry:
    """An ordered, named collection of tools."""

    def __init__(self, tools: Iterable[Tool] | None = None) -> None:
        self._tools: dict[str, Tool] = {}
        self._revision = 0
        #: Definitions trimmed or omitted to fit the request. Every other store
        #: in the package grew one of these.
        self.problems = ProblemLog()
        for t in tools or []:
            self.register(t)

    def register(self, tool: Tool, *, replace: bool = False) -> "ToolRegistry":
        if tool.name in self._tools and not replace:
            raise ValueError(f"Tool '{tool.name}' already registered (pass replace=True to override)")
        if tool.risk is not None and tool.risk not in RISK_LEVELS:
            # A typo'd level would fall through to "unclassified" and gate as
            # external -- safe, but silently stricter than the author asked
            # for. Reject it loudly instead.
            raise ValueError(
                f"Tool '{tool.name}' declares unknown risk {tool.risk!r}; "
                f"expected one of {RISK_LEVELS} or None"
            )
        if any(not isinstance(name, str) or not name.strip() for name in tool.capabilities):
            raise ValueError(
                f"Tool '{tool.name}' capabilities must be non-empty strings"
            )
        self._tools[tool.name] = tool
        self._revision += 1
        return self

    def add(
        self,
        name: str,
        description: str,
        input_schema: dict,
        *,
        readonly: bool = False,
        parallel_safe: bool = False,
        risk: str | None = None,
        capabilities: Iterable[str] = (),
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
                    risk=risk,
                    capabilities=frozenset(capabilities),
                ),
                replace=replace,
            )
            return fn
        return deco

    def unregister(self, name: str) -> "ToolRegistry":
        if self._tools.pop(name, None) is not None:
            self._revision += 1
        return self

    def get(self, name: str) -> Tool | None:
        return self._tools.get(name)

    def names(self) -> list[str]:
        return list(self._tools)

    def _fit(self) -> tuple[list[dict], int | None, list[str]]:
        """(schemas to send, trim step applied or None, names omitted).

        Pure: no problem reports, no state. `schemas()` adds the reporting;
        `sent_names()`/`omitted_names()` let the system builder describe the
        request that will actually be made without logging a problem twice.
        """

        schemas = [dict(t.schema) for t in self._tools.values()]
        if _payload_size(schemas) <= MAX_TOOL_PAYLOAD:
            return schemas, None, []
        for limit in TOOL_DESCRIPTION_STEPS:
            for schema in schemas:
                description = schema.get("description") or ""
                if len(description) > limit:
                    schema["description"] = description[:limit] + "..."
            if _payload_size(schemas) <= MAX_TOOL_PAYLOAD:
                return schemas, limit, []
        # Names and schemas alone are over budget: drop, keeping request order.
        kept, used = [], 0
        for schema in schemas:
            size = _payload_size([schema])
            if used + size > MAX_TOOL_PAYLOAD:
                continue
            kept.append(schema)
            used += size
        kept_names = {s["name"] for s in kept}
        omitted = [name for name in self._tools if name not in kept_names]
        return kept, TOOL_DESCRIPTION_STEPS[-1], omitted

    def schemas(self) -> list[dict]:
        """Tool definitions for one request, within a total budget.

        Round 40 capped each MCP description at 4,000 characters and left the
        *count* alone, which is the same inversion round 90 found in skills.
        Tool definitions are sent on every single request, and MCP servers add
        them by the dozen:

             50 extra tools ->   222,485 chars  ~55,621 tokens per request
            200 extra tools ->   851,835 chars ~212,958 tokens per request
            500 extra tools -> 2,110,635 chars ~527,658 tokens per request

        Past a point that is not a cost problem but a hard failure: the request
        exceeds the context window and *every* turn fails, with a provider error
        that says nothing about which tool did it.

        Descriptions are trimmed before any tool is dropped, because they are
        the compressible part -- a tool with a short description is still
        callable, and a tool that is absent is a capability the model is told it
        does not have. Only if the names and schemas alone exceed the budget is
        anything omitted, and then it is reported both ways: an operator sees
        `problems`, and `default_system_builder` names the omission to the
        model via `omitted_names()`.
        """

        return self.snapshot(report=True).schemas()

    def snapshot(self, *, report: bool = False) -> ToolCatalogSnapshot:
        """Freeze the fitted catalogue once for a single model request.

        Ordering remains registry insertion order: changing it is a provider
        cache change and therefore changes the fingerprint. Object key order is
        canonicalized only for the serialized payload so equal schemas built by
        different dictionary insertion paths still have the same identity.
        """

        schemas, trimmed_to, omitted = self._fit()
        schema_json = json.dumps(
            schemas,
            default=str,
            ensure_ascii=False,
            separators=(",", ":"),
            sort_keys=True,
        )
        snapshot = ToolCatalogSnapshot(
            revision=self._revision,
            fingerprint=hashlib.sha256(schema_json.encode("utf-8")).hexdigest(),
            schema_json=schema_json,
            sent_names=tuple(schema["name"] for schema in schemas),
            omitted_names=tuple(omitted),
            trimmed_to=trimmed_to,
            inventory_count=len(self._tools),
        )
        if not report:
            return snapshot
        if omitted:
            self.problems.append(
                f"{len(omitted)} tool(s) omitted from the request: "
                f"{len(self._tools)} tools exceed {MAX_TOOL_PAYLOAD:,} "
                "characters even with minimal descriptions"
            )
        elif trimmed_to is not None:
            self.problems.append(
                f"tool descriptions trimmed to {trimmed_to:,} characters to fit "
                f"{MAX_TOOL_PAYLOAD:,} in the request ({len(schemas)} tools)"
            )
        return snapshot

    def sent_names(self) -> list[str]:
        """Names of the tools the next request will actually carry.

        `names()` is the registry inventory and stays that way -- the audit,
        session events, and workflow provenance all want what is *registered*.
        The system prompt is the one place that must describe the *request*:
        round 93 found `default_system_builder` listing all 3,037 names of a
        pathological registry while `schemas()` sent 529, telling the model it
        had 2,508 tools it could not reliably call.
        """

        return list(self.snapshot().sent_names)

    def omitted_names(self) -> list[str]:
        """Registered tools the next request will not carry (usually empty)."""

        return list(self.snapshot().omitted_names)

    def subset(self, names: Iterable[str]) -> "ToolRegistry":
        keep = set(names)
        return ToolRegistry([t for n, t in self._tools.items() if n in keep])

    def with_capabilities(self, capabilities: Iterable[str]) -> "ToolRegistry":
        """Return tools whose complete authority fits within ``capabilities``."""

        allowed = set(capabilities)
        return ToolRegistry(
            tool for tool in self._tools.values()
            if tool.capabilities and tool.capabilities.issubset(allowed)
        )

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
