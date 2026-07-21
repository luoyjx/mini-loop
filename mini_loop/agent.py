"""The agent -- one async loop, complete capabilities, zero global state.

This is the s01 loop in spirit:

    while True:
        response = LLM(messages, tools)
        append assistant turn
        if there are no tool_use blocks: return
        execute tools; append results

Every capability is now a *swappable seam* rather than baked into the loop:

    tools          a ToolRegistry          (builtins.py: bash/read/write/edit/glob/
                                             TodoWrite/task/load_skill/compress)
    hooks          a Hooks chain           (permissions, audit, transforms)
    system prompt  a system_builder(agent) (prompts.py)
    compaction     a Compactor             (compaction.py)
    skills         a SkillLoader           (skills.py)
    LLM            an injected client      (config.py)

See EXTENDING.md for how to replace each one without touching this file.
"""

from __future__ import annotations

import asyncio
import time
import uuid
from collections.abc import Awaitable, Callable
from pathlib import Path

from .builtins import default_registry, explore_registry, worker_registry
from .compaction import Compactor, DefaultCompactor, estimate_tokens, microcompact  # re-exported
from .config import Settings
from .permissions import default_hooks
from .prompts import default_system_builder
from .recovery import DefaultRecovery
from .registry import Hooks, ToolCall, ToolContext, ToolRegistry
from .skills import SkillLoader
from .tools import Toolset

# An injector is `async (agent) -> list[message]` run at the top of each loop
# pass; it returns messages to splice into history (e.g. background results,
# fired cron prompts). See background.py / cron.py.
Injector = Callable[["Agent"], Awaitable[list]]

__all__ = ["Agent", "TodoManager", "microcompact", "estimate_tokens"]

EmitFn = Callable[[dict], Awaitable[None]]
DISPLAY_CAP = 2000   # how much of a tool result to surface in an event


def _usage_payload(response) -> dict | None:
    usage = getattr(response, "usage", None)
    if usage is None:
        return None
    if hasattr(usage, "model_dump"):
        return usage.model_dump()
    if hasattr(usage, "to_dict"):
        return usage.to_dict()
    if hasattr(usage, "__dict__"):
        return {
            key: value for key, value in vars(usage).items()
            if not key.startswith("_")
        }
    return {"value": str(usage)}


def _content_payload(blocks: list) -> list:
    payload = []
    for block in blocks:
        if isinstance(block, dict):
            payload.append(block)
        elif hasattr(block, "model_dump"):
            payload.append(block.model_dump())
        elif getattr(block, "type", None) == "text":
            payload.append({"type": "text", "text": getattr(block, "text", "")})
        elif getattr(block, "type", None) == "tool_use":
            payload.append({
                "type": "tool_use",
                "id": getattr(block, "id", None),
                "name": getattr(block, "name", None),
                "input": getattr(block, "input", {}),
            })
        else:
            payload.append({"type": getattr(block, "type", "unknown")})
    return payload


def _messages_payload(messages: list) -> list:
    payload = []
    for message in messages:
        if not isinstance(message, dict):
            payload.append(message)
            continue
        item = dict(message)
        if isinstance(item.get("content"), list):
            item["content"] = _content_payload(item["content"])
        payload.append(item)
    return payload


# --- s05: TodoWrite ---------------------------------------------------------

class TodoManager:
    def __init__(self) -> None:
        self.items: list[dict] = []

    def update(self, items: list) -> str:
        validated, in_progress = [], 0
        for i, item in enumerate(items):
            content = str(item.get("content", "")).strip()
            status = str(item.get("status", "pending")).lower()
            active = str(item.get("activeForm", "")).strip()
            if not content:
                raise ValueError(f"Item {i}: content required")
            if status not in ("pending", "in_progress", "completed"):
                raise ValueError(f"Item {i}: invalid status '{status}'")
            if not active:
                raise ValueError(f"Item {i}: activeForm required")
            if status == "in_progress":
                in_progress += 1
            validated.append({"content": content, "status": status, "activeForm": active})
        if len(validated) > 20:
            raise ValueError("Max 20 todos")
        if in_progress > 1:
            raise ValueError("Only one in_progress allowed")
        self.items = validated
        return self.render()

    def render(self) -> str:
        if not self.items:
            return "No todos."
        glyph = {"completed": "[x]", "in_progress": "[>]", "pending": "[ ]"}
        lines = []
        for it in self.items:
            suffix = f" <- {it['activeForm']}" if it["status"] == "in_progress" else ""
            lines.append(f"{glyph.get(it['status'], '[?]')} {it['content']}{suffix}")
        done = sum(1 for t in self.items if t["status"] == "completed")
        lines.append(f"\n({done}/{len(self.items)} completed)")
        return "\n".join(lines)

    def has_open_items(self) -> bool:
        return any(it.get("status") != "completed" for it in self.items)

    def snapshot(self) -> list[dict]:
        return list(self.items)


class _Unbounded:
    """Stand-in for an absent LLM semaphore."""

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_):
        return False


class Agent:
    """A single conversational agent. Reused recursively for subagents."""

    def __init__(
        self,
        *,
        client,
        settings: Settings,
        workspace: Path,
        skills: SkillLoader | None = None,
        tools: ToolRegistry | None = None,
        hooks: Hooks | None = None,
        system: str | None = None,
        system_builder: Callable[["Agent"], str] | None = None,
        compactor: Compactor | None = None,
        recovery=None,
        injectors: list[Injector] | None = None,
        emit: EmitFn | None = None,
        llm_semaphore=None,
        label: str = "main",
        depth: int = 0,
        max_rounds: int | None = None,
        state: dict | None = None,
    ) -> None:
        self.client = client
        self.settings = settings
        self.workspace = Path(workspace)
        self.skills = skills or SkillLoader(settings.skills_dir)
        self.emit = emit
        self.semaphore = llm_semaphore or _Unbounded()
        self.label = label
        self.depth = depth
        self.max_rounds = max_rounds if max_rounds is not None else settings.max_turns

        self.toolset = Toolset(self.workspace, bash_timeout=settings.bash_timeout)
        self.todo = TodoManager()
        self.tools = tools if tools is not None else default_registry()
        self.hooks = hooks if hooks is not None else default_hooks()
        self.compactor = compactor or DefaultCompactor()
        self.recovery = recovery or DefaultRecovery()
        self.injectors: list[Injector] = list(injectors or [])
        self.state: dict = state if state is not None else {}

        # System prompt: explicit string wins, else build from the agent.
        self.system_builder = system_builder or default_system_builder
        self._dynamic_system = system is None
        self._system = system if system is not None else self.system_builder(self)

        self.messages: list[dict] = []
        self.last_text: str = ""
        self._last_model_span_id: str | None = None
        self._rounds_without_todo = 0
        self._pending_compact = False

    @property
    def system(self) -> str:
        return self._system

    @system.setter
    def system(self, value: str) -> None:
        # Direct assignment is an explicit override. This keeps the public API
        # backward compatible while allowing builder-based prompts to refresh.
        self._system = value
        self._dynamic_system = False

    def refresh_system(self) -> str:
        if self._dynamic_system:
            self._system = self.system_builder(self)
        return self._system

    def use_system_builder(self, builder: Callable[["Agent"], str]) -> None:
        """Switch back to a per-call prompt builder after a fixed override."""
        self.system_builder = builder
        self._dynamic_system = True
        self.refresh_system()

    def enter_workspace(self, workspace: Path) -> None:
        """Switch this agent's file tools to an already-provisioned workspace."""
        self.toolset = Toolset(Path(workspace), bash_timeout=self.settings.bash_timeout)
        self.workspace = self.toolset.workspace
        background = self.state.get("background")
        if background is not None:
            background.workspace = self.workspace

    async def _send(self, event_type: str, **fields) -> None:
        if self.emit is None:
            return
        await self.emit({**fields, "type": event_type, "agent": self.label, "depth": self.depth})

    async def _create(
        self,
        messages,
        *,
        tools=None,
        system=None,
        max_tokens=None,
        purpose: str = "agent_turn",
    ):
        kwargs: dict = {
            "model": self.state.get("recovery_model", self.settings.model),
            "messages": messages,
            "max_tokens": max_tokens or self.settings.max_tokens,
        }
        if system is not None:
            kwargs["system"] = system
        if tools is not None:
            kwargs["tools"] = tools

        async def call(kw: dict):
            async with self.semaphore:   # backoff sleeps happen OUTSIDE the slot
                return await self.client.messages.create(**kw)

        span_id = f"model_{uuid.uuid4().hex[:16]}"
        self._last_model_span_id = span_id
        started = time.monotonic()
        await self._send(
            "model_start",
            span_id=span_id,
            purpose=purpose,
            model=kwargs["model"],
            message_count=len(messages),
            input_tokens_estimate=estimate_tokens(messages),
            tool_count=len(tools or []),
            max_tokens=kwargs["max_tokens"],
            _trajectory_fields={
                "model_input": {
                    "messages": _messages_payload(messages),
                    "system": system,
                    "tools": tools,
                    "max_tokens": kwargs["max_tokens"],
                },
            },
        )
        try:
            response = await self.recovery.run(self, kwargs, call)
        except asyncio.CancelledError:
            await self._send(
                "model_end",
                span_id=span_id,
                purpose=purpose,
                status="cancelled",
                duration_ms=round((time.monotonic() - started) * 1000, 3),
            )
            raise
        except Exception as error:
            await self._send(
                "model_end",
                span_id=span_id,
                purpose=purpose,
                status="error",
                duration_ms=round((time.monotonic() - started) * 1000, 3),
                error=f"{type(error).__name__}: {error}"[:500],
            )
            raise
        await self._send(
            "model_end",
            span_id=span_id,
            purpose=purpose,
            status="completed",
            duration_ms=round((time.monotonic() - started) * 1000, 3),
            stop_reason=getattr(response, "stop_reason", None),
            usage=_usage_payload(response),
            _trajectory_fields={
                "model_output": _content_payload(response.content),
            },
        )
        return response

    # -- public entry: run one user turn to completion, return final text --
    async def run(self, user_text: str) -> str:
        user_text = await self.hooks.user_prompt(self, user_text)
        # s09: index + selected bodies are loaded before the user turn.
        from .memory import prepare_memory_context

        user_text = await prepare_memory_context(self, user_text)
        self.messages.append({"role": "user", "content": user_text})
        await self._loop()
        return self.last_text

    # -- the loop --
    async def _loop(self) -> None:
        for _ in range(self.max_rounds):
            # Pre-turn injection: background results, fired cron prompts, etc.
            for inject in self.injectors:
                extra = await inject(self)
                if extra:
                    self.messages.extend(extra)

            # Every new notification goes through the same context-budget
            # pipeline before the model sees it.
            await self.compactor.maybe_compact(self)  # s08, pluggable

            try:
                response = await self._create(
                    self.messages,
                    tools=self.tools.schemas(),
                    system=self.refresh_system(),
                    purpose="agent_turn",
                )
            except Exception as error:
                detail = f"{type(error).__name__}: {error}"[:500]
                self.last_text = f"[Error] {detail}"
                self.messages.append({
                    "role": "assistant",
                    "content": [{"type": "text", "text": self.last_text}],
                })
                await self._send("error", error=detail)
                return
            self.messages.append({"role": "assistant", "content": response.content})

            text = "".join(getattr(b, "text", "") for b in response.content if getattr(b, "type", "") == "text")
            if text:
                self.last_text = text
                await self._send("assistant_text", text=text)

            # Providers occasionally report an inconsistent stop_reason. The
            # protocol contract is the content itself: execute actual tool_use
            # blocks, and stop when none are present.
            tool_blocks = [
                block for block in response.content
                if getattr(block, "type", "") == "tool_use"
            ]
            if not tool_blocks:
                continuation = await self.hooks.stop(self, self.messages, self.last_text)
                if continuation is not None:
                    self.messages.append({"role": "user", "content": continuation})
                    continue
                from .memory import memory_on_stop

                await memory_on_stop(self)
                return

            results, used_todo = [], False
            self._pending_compact = False
            for block in tool_blocks:
                if block.name == "TodoWrite":
                    used_todo = True
                call = ToolCall(block.name, dict(block.input), block.id)
                output = await self._exec_tool(
                    call, parent_span_id=self._last_model_span_id
                )
                # Keep the complete value until the next context-budget pass;
                # DefaultCompactor persists oversized batches before the LLM sees them.
                results.append({"type": "tool_result", "tool_use_id": block.id, "content": output})

            # s05 nag: a plan is open but the model drifted off TodoWrite.
            self._rounds_without_todo = 0 if used_todo else self._rounds_without_todo + 1
            if self.todo.has_open_items() and self._rounds_without_todo >= 3:
                results.append({"type": "text", "text": "<reminder>Update your todos.</reminder>"})
                self._rounds_without_todo = 0

            self.messages.append({"role": "user", "content": results})

            if self._pending_compact:
                await self.compactor.compact(self)
                self._pending_compact = False
                continue

        await self._send("error", error=f"Hit max_rounds ({self.max_rounds}) without finishing")
        self.last_text = self.last_text or f"[stopped after {self.max_rounds} rounds]"

    # -- one tool call: emit, pre-hooks, dispatch via registry, post-hooks --
    async def _exec_tool(self, call: ToolCall, *, parent_span_id: str | None = None) -> str:
        ctx = ToolContext(agent=self, workspace=self.workspace, state=self.state, call=call)
        span_id = f"tool_{uuid.uuid4().hex[:16]}"
        started = time.monotonic()
        await self._send(
            "tool_use",
            name=call.name,
            input=call.input,
            id=call.id,
            span_id=span_id,
            parent_span_id=parent_span_id,
        )

        denied = await self.hooks.before_tool(ctx, call)
        if denied is not None:
            out = str(denied)
            await self._send(
                "tool_result",
                name=call.name,
                output=out[:DISPLAY_CAP],
                id=call.id,
                span_id=span_id,
                parent_span_id=parent_span_id,
                denied=True,
                duration_ms=round((time.monotonic() - started) * 1000, 3),
                _trajectory_fields={"output": out},
            )
            return out

        tool = self.tools.get(call.name)
        failed = False
        if tool is None:
            out = f"Unknown tool: {call.name}"
            failed = True
        else:
            try:
                out = str(await tool.run(ctx, **call.input))
            except Exception as e:  # tool errors are data the model reacts to, not crashes
                out = f"Error: {e}"
                failed = True

        out = str(await self.hooks.after_tool(ctx, call, out))
        await self._send(
            "tool_result",
            name=call.name,
            output=out[:DISPLAY_CAP],
            id=call.id,
            span_id=span_id,
            parent_span_id=parent_span_id,
            error=failed,
            duration_ms=round((time.monotonic() - started) * 1000, 3),
            _trajectory_fields={"output": out},
        )
        return out

    # -- s06: subagent = a fresh Agent, isolated context, restricted tools --
    async def _run_subagent(self, prompt: str, agent_type: str = "Explore") -> str:
        registry = explore_registry() if agent_type == "Explore" else worker_registry()
        verb = "explore and report" if agent_type == "Explore" else "complete the task"
        child = Agent(
            client=self.client,
            settings=self.settings,
            workspace=self.workspace,
            skills=self.skills,
            tools=registry,
            hooks=self.hooks,           # policies apply to subagents too
            compactor=self.compactor,
            recovery=self.recovery,
            system=f"You are a {agent_type} subagent in {self.workspace}. "
                   f"Use tools to {verb}, then give a concise final summary. No preamble.",
            emit=self.emit,
            llm_semaphore=self.semaphore,
            label=f"{self.label}>{agent_type.lower()}",
            depth=self.depth + 1,
            max_rounds=self.settings.subagent_max_rounds,
        )
        await self._send(
            "subagent_start",
            agent_type=agent_type,
            prompt=prompt[:DISPLAY_CAP],
            _trajectory_fields={"prompt": prompt},
        )
        summary = await child.run(prompt)
        await self._send(
            "subagent_end",
            agent_type=agent_type,
            summary=summary[:DISPLAY_CAP],
            _trajectory_fields={"summary": summary},
        )
        return summary or "(subagent produced no summary)"
