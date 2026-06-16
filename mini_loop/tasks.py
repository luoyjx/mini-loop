"""Persistent task graph (s12).

Unlike the in-memory TodoWrite checklist (s05), this is a file-backed task
graph that survives across sessions: one JSON file per task under
`<workspace>/.tasks/`, with `blockedBy` declaring upstream dependencies. A task
can be claimed (by an owner) only once every dependency is `completed`;
completing a task reports which downstream tasks just became runnable.

Exposed as five tools via `install_tasks(registry)`. The store lives on the
agent's per-session `state`, so each session has its own board (and teammates
sharing a workspace share the board).
"""

from __future__ import annotations

import asyncio
import json
import random
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry


@dataclass
class Task:
    id: str
    subject: str
    description: str = ""
    status: str = "pending"          # pending | in_progress | completed
    owner: str | None = None
    blockedBy: list[str] = field(default_factory=list)


class TaskStore:
    def __init__(self, root: Path) -> None:
        self.dir = Path(root) / ".tasks"
        self.dir.mkdir(parents=True, exist_ok=True)

    def _path(self, tid: str) -> Path:
        return self.dir / f"{tid}.json"

    def _new_id(self) -> str:
        return f"task_{int(time.time())}_{random.randint(0, 9999):04d}"

    def save(self, task: Task) -> None:
        self._path(task.id).write_text(json.dumps(asdict(task), indent=2))

    def load(self, tid: str) -> Task | None:
        p = self._path(tid)
        return Task(**json.loads(p.read_text())) if p.exists() else None

    def list(self) -> list[Task]:
        return [Task(**json.loads(p.read_text())) for p in sorted(self.dir.glob("task_*.json"))]

    def create(self, subject: str, description: str = "", blocked_by: list[str] | None = None) -> Task:
        task = Task(id=self._new_id(), subject=subject, description=description,
                    blockedBy=list(blocked_by or []))
        self.save(task)
        return task

    def can_start(self, tid: str) -> bool:
        task = self.load(tid)
        if task is None:
            return False
        for dep in task.blockedBy:
            d = self.load(dep)
            if d is None or d.status != "completed":
                return False
        return True

    def claim(self, tid: str, owner: str) -> str:
        task = self.load(tid)
        if task is None:
            return f"Error: task {tid} not found"
        if task.status != "pending":
            return f"Error: task {tid} is {task.status}, not claimable"
        if not self.can_start(tid):
            return f"Error: task {tid} is blocked by incomplete deps {task.blockedBy}"
        task.owner, task.status = owner, "in_progress"
        self.save(task)
        return f"Claimed {tid} for {owner}"

    def complete(self, tid: str) -> str:
        task = self.load(tid)
        if task is None:
            return f"Error: task {tid} not found"
        task.status = "completed"
        self.save(task)
        unblocked = [t.id for t in self.list()
                     if t.status == "pending" and t.blockedBy and self.can_start(t.id)]
        msg = f"Completed {tid}."
        if unblocked:
            msg += f" Now runnable: {', '.join(unblocked)}"
        return msg

    def render(self) -> str:
        tasks = self.list()
        if not tasks:
            return "No tasks."
        glyph = {"pending": "[ ]", "in_progress": "[>]", "completed": "[x]"}
        lines = []
        for t in tasks:
            owner = f" @{t.owner}" if t.owner else ""
            blocked = f" (blockedBy: {t.blockedBy})" if t.blockedBy else ""
            lines.append(f"{glyph.get(t.status, '[?]')} {t.id}: {t.subject}{owner}{blocked}")
        return "\n".join(lines)


def _store(ctx: ToolContext) -> TaskStore:
    store = ctx.state.get("tasks")
    if store is None:
        store = ctx.state["tasks"] = TaskStore(ctx.workspace)
    return store


def _owner(ctx: ToolContext) -> str:
    return ctx.state.get("agent_name") or ctx.agent.label


# --- tools ------------------------------------------------------------------

_CREATE = {
    "type": "object",
    "properties": {
        "subject": {"type": "string"},
        "description": {"type": "string"},
        "blockedBy": {"type": "array", "items": {"type": "string"}},
    },
    "required": ["subject"],
}
_BY_ID = {"type": "object", "properties": {"task_id": {"type": "string"}}, "required": ["task_id"]}
_EMPTY = {"type": "object", "properties": {}}


def install_tasks(registry: ToolRegistry) -> ToolRegistry:
    async def create_task(ctx, subject, description="", blockedBy=None):
        t = await asyncio.to_thread(_store(ctx).create, subject, description, blockedBy)
        return f"Created {t.id}: {t.subject}"

    async def list_tasks(ctx):
        return await asyncio.to_thread(_store(ctx).render)

    async def get_task(ctx, task_id):
        t = await asyncio.to_thread(_store(ctx).load, task_id)
        return json.dumps(t.__dict__, indent=2) if t else f"Error: task {task_id} not found"

    async def claim_task(ctx, task_id):
        return await asyncio.to_thread(_store(ctx).claim, task_id, _owner(ctx))

    async def complete_task(ctx, task_id):
        return await asyncio.to_thread(_store(ctx).complete, task_id)

    registry.register(Tool("create_task", "Create a persistent task (optionally blockedBy other task ids).", _CREATE, create_task))
    registry.register(Tool("list_tasks", "List all persistent tasks and their status.", _EMPTY, list_tasks, readonly=True))
    registry.register(Tool("get_task", "Get one task's full JSON by id.", _BY_ID, get_task, readonly=True))
    registry.register(Tool("claim_task", "Claim a pending, unblocked task (sets you as owner).", _BY_ID, claim_task))
    registry.register(Tool("complete_task", "Mark a task completed; reports newly-unblocked tasks.", _BY_ID, complete_task))
    return registry
