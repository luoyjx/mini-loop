"""The agent -- one async loop, complete capabilities, zero global state.

This is the s01 loop verbatim in spirit:

    while True:
        response = LLM(messages, tools)
        append assistant turn
        if stop_reason != "tool_use": return
        execute tools; append results

Layered on top, each as a small addition that leaves the loop intact:

    s02  tool dispatch        bash / read / write / edit (workspace-sandboxed)
    s05  TodoWrite            plan-then-execute checklist
    s06  subagent (`task`)    spawn a fresh-context child, get back a summary
    s07  load_skill           pull domain knowledge in on demand
    s08  compaction           micro-compact tool results + auto-compact history

Every mechanism that `s_full.py` keeps in module globals lives on the instance
here, so thousands of these can run side by side in one process.
"""

from __future__ import annotations

import json
import time
from collections.abc import Awaitable, Callable
from pathlib import Path

from .config import Settings
from .skills import SkillLoader
from .tools import EDIT_FILE, FILE_TOOLS, READ_FILE, READONLY_TOOLS, BASH, WRITE_FILE, Toolset

EmitFn = Callable[[dict], Awaitable[None]]

# --- agent-level tool schemas (the file tools live in tools.py) ------------

TODO_WRITE = {
    "name": "TodoWrite",
    "description": "Create/replace the task checklist. Use for multi-step work.",
    "input_schema": {
        "type": "object",
        "properties": {
            "items": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "content": {"type": "string"},
                        "status": {"type": "string", "enum": ["pending", "in_progress", "completed"]},
                        "activeForm": {"type": "string"},
                    },
                    "required": ["content", "status", "activeForm"],
                },
            }
        },
        "required": ["items"],
    },
}
TASK = {
    "name": "task",
    "description": "Delegate isolated work to a subagent with a fresh context. "
                   "'Explore' is read-only; 'general-purpose' may also edit files. "
                   "Returns only the subagent's final summary.",
    "input_schema": {
        "type": "object",
        "properties": {
            "prompt": {"type": "string"},
            "agent_type": {"type": "string", "enum": ["Explore", "general-purpose"]},
        },
        "required": ["prompt"],
    },
}
LOAD_SKILL = {
    "name": "load_skill",
    "description": "Load a named skill's full instructions into context.",
    "input_schema": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"],
    },
}
COMPRESS = {
    "name": "compress",
    "description": "Manually compress the conversation to free context.",
    "input_schema": {"type": "object", "properties": {}},
}

_SCHEMA_BY_NAME = {
    "bash": BASH, "read_file": READ_FILE, "write_file": WRITE_FILE, "edit_file": EDIT_FILE,
    "TodoWrite": TODO_WRITE, "task": TASK, "load_skill": LOAD_SKILL, "compress": COMPRESS,
}

MAIN_TOOLS = ["bash", "read_file", "write_file", "edit_file", "TodoWrite", "task", "load_skill", "compress"]
EXPLORE_TOOLS = [t["name"] for t in READONLY_TOOLS]
WORKER_TOOLS = [t["name"] for t in FILE_TOOLS]

DISPLAY_CAP = 2000  # how much of a tool result to surface in an event (full text still goes to the model)


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


# --- s08: context estimation + micro-compaction (pure functions) -----------

def estimate_tokens(messages: list) -> int:
    return len(json.dumps(messages, default=str)) // 4


def microcompact(messages: list) -> int:
    """Blank out the body of all but the 3 most recent tool results, in place.

    Returns how many were cleared. Old tool output is the cheapest context to
    shed: the model already acted on it, so the bytes are dead weight.
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
        system: str | None = None,
        emit: EmitFn | None = None,
        llm_semaphore=None,
        tool_names: list[str] | None = None,
        max_rounds: int | None = None,
        label: str = "main",
        depth: int = 0,
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
        self.tool_names = tool_names or MAIN_TOOLS
        self.tools = [_SCHEMA_BY_NAME[n] for n in self.tool_names]
        self.system = system if system is not None else self._default_system()

        self.messages: list[dict] = []
        self.last_text: str = ""
        self._rounds_without_todo = 0

    # -- system prompt assembled at runtime (s10) --
    def _default_system(self) -> str:
        return (
            f"You are a coding agent working in {self.workspace}.\n"
            "Use the provided tools to act; prefer doing over explaining.\n"
            "For multi-step work, lay out a plan with TodoWrite and keep it updated.\n"
            "Delegate large side-quests to a subagent via `task` to keep your context clean.\n"
            "Pull in specialized knowledge with `load_skill` only when you need it.\n"
            f"Available skills:\n{self.skills.descriptions()}"
        )

    async def _send(self, event_type: str, **fields) -> None:
        if self.emit is None:
            return
        await self.emit({"type": event_type, "agent": self.label, "depth": self.depth, **fields})

    async def _create(self, messages, *, tools=None, system=None, max_tokens=None):
        kwargs: dict = {
            "model": self.settings.model,
            "messages": messages,
            "max_tokens": max_tokens or self.settings.max_tokens,
        }
        if system is not None:
            kwargs["system"] = system
        if tools is not None:
            kwargs["tools"] = tools
        async with self.semaphore:
            return await self.client.messages.create(**kwargs)

    # -- public entry: run one user turn to completion, return final text --
    async def run(self, user_text: str) -> str:
        self.messages.append({"role": "user", "content": user_text})
        await self._loop()
        return self.last_text

    # -- the loop --
    async def _loop(self) -> None:
        for _ in range(self.max_rounds):
            # s08: keep the context lean before spending tokens on it.
            cleared = microcompact(self.messages)
            if cleared:
                await self._send("compact", kind="micro", cleared=cleared)
            if estimate_tokens(self.messages) > self.settings.token_threshold:
                await self._auto_compact()

            response = await self._create(self.messages, tools=self.tools, system=self.system)
            self.messages.append({"role": "assistant", "content": response.content})

            text = "".join(getattr(b, "text", "") for b in response.content if getattr(b, "type", "") == "text")
            if text:
                self.last_text = text
                await self._send("assistant_text", text=text)

            if response.stop_reason != "tool_use":
                return

            results, used_todo, manual_compress = [], False, False
            for block in response.content:
                if getattr(block, "type", "") != "tool_use":
                    continue
                if block.name == "compress":
                    manual_compress = True
                if block.name == "TodoWrite":
                    used_todo = True
                await self._send("tool_use", name=block.name, input=block.input, id=block.id)
                output = await self._dispatch(block.name, dict(block.input))
                output = str(output)
                await self._send("tool_result", name=block.name, output=output[:DISPLAY_CAP])
                results.append({"type": "tool_result", "tool_use_id": block.id, "content": output[:50_000]})

            # s05 nag: if a plan is open but the model drifted off TodoWrite, remind it.
            self._rounds_without_todo = 0 if used_todo else self._rounds_without_todo + 1
            if self.todo.has_open_items() and self._rounds_without_todo >= 3:
                results.append({"type": "text", "text": "<reminder>Update your todos.</reminder>"})
                self._rounds_without_todo = 0

            self.messages.append({"role": "user", "content": results})

            if manual_compress:
                await self._auto_compact()
                self.last_text = self.last_text or "[context compressed]"
                return

        await self._send("error", error=f"Hit max_rounds ({self.max_rounds}) without finishing")
        self.last_text = self.last_text or f"[stopped after {self.max_rounds} rounds]"

    # -- tool routing --
    async def _dispatch(self, name: str, args: dict) -> str:
        try:
            if self.toolset.handles(name):
                return await self.toolset.dispatch(name, args)
            if name == "TodoWrite":
                render = self.todo.update(args["items"])
                await self._send("todo", items=self.todo.snapshot())
                return render
            if name == "task":
                return await self._run_subagent(args["prompt"], args.get("agent_type", "Explore"))
            if name == "load_skill":
                return self.skills.load(args["name"])
            if name == "compress":
                return "Compressing conversation..."
            return f"Unknown tool: {name}"
        except Exception as e:  # tool errors are data the model can react to, not crashes
            return f"Error: {e}"

    # -- s06: subagent = a fresh Agent, isolated context, restricted tools --
    async def _run_subagent(self, prompt: str, agent_type: str = "Explore") -> str:
        tool_names = EXPLORE_TOOLS if agent_type == "Explore" else WORKER_TOOLS
        verb = "explore and report" if agent_type == "Explore" else "complete the task"
        child = Agent(
            client=self.client,
            settings=self.settings,
            workspace=self.workspace,
            skills=self.skills,
            system=f"You are a {agent_type} subagent in {self.workspace}. "
                   f"Use tools to {verb}, then give a concise final summary. No preamble.",
            emit=self.emit,
            llm_semaphore=self.semaphore,
            tool_names=tool_names,
            max_rounds=self.settings.subagent_max_rounds,
            label=f"{self.label}>{agent_type.lower()}",
            depth=self.depth + 1,
        )
        await self._send("subagent_start", agent_type=agent_type, prompt=prompt[:DISPLAY_CAP])
        summary = await child.run(prompt)
        await self._send("subagent_end", agent_type=agent_type, summary=summary[:DISPLAY_CAP])
        return summary or "(subagent produced no summary)"

    # -- s08: auto-compaction. Persist a transcript, replace history w/ summary --
    async def _auto_compact(self) -> None:
        transcript_dir = self.workspace / ".transcripts"
        transcript_dir.mkdir(exist_ok=True)
        path = transcript_dir / f"transcript_{int(time.time() * 1000)}.jsonl"
        with open(path, "w") as f:
            for msg in self.messages:
                f.write(json.dumps(msg, default=str) + "\n")

        conv = json.dumps(self.messages, default=str)[-80_000:]
        resp = await self._create(
            [{"role": "user", "content": f"Summarize this agent session for continuity:\n{conv}"}],
            max_tokens=2000,
        )
        summary = "".join(getattr(b, "text", "") for b in resp.content if getattr(b, "type", "") == "text")
        self.messages[:] = [
            {"role": "user", "content": f"[Context compressed. Full transcript: {path}]\n{summary}"}
        ]
        await self._send("compact", kind="auto", transcript=str(path))
