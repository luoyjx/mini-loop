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
import re
import threading
import uuid
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .durable import atomic_write_text
from .problems import ProblemLog
from .registry import Tool, ToolContext, ToolRegistry


@dataclass
class Task:
    id: str
    subject: str
    description: str = ""
    status: str = "pending"          # pending | in_progress | completed
    owner: str | None = None
    blockedBy: list[str] = field(default_factory=list)
    worktree: str | None = None


_LOCKS_GUARD = threading.Lock()
_LOCKS: dict[str, threading.RLock] = {}
_SAFE_ID = re.compile(r"[A-Za-z0-9._-]{1,128}")
_SAFE_WORKTREE = re.compile(r"[A-Za-z0-9._-]{1,64}")

#: Task rows shown by `list_tasks`, and the per-row subject width. Tasks are
#: never deleted (completed ones are kept so `blockedBy` still resolves), and a
#: subject may be up to `MAX_FIELD` (16 KB), so an unbounded board renders one
#: line per task ever created, each up to 16 KB -- the round-164 unbounded-
#: aggregate-output shape, one store over. The board is bounded on both axes:
#: the most recent rows are shown and the rest summarised, and each subject is
#: previewed. Any task stays reachable in full by id.
MAX_TASK_BOARD = 50
MAX_SUBJECT_DISPLAY = 200


def _lock_for(path: Path) -> threading.RLock:
    key = str(path.resolve())
    with _LOCKS_GUARD:
        return _LOCKS.setdefault(key, threading.RLock())


class TaskStore:
    def __init__(self, root: Path, *, secrets=None) -> None:
        self.secrets = secrets
        #: Fields that were truncated on the way to disk.
        self.problems = ProblemLog()
        self.dir = Path(root) / ".tasks"
        self.dir.mkdir(parents=True, exist_ok=True)
        self._lock = _lock_for(self.dir)

    def _path(self, tid: str) -> Path:
        if not _SAFE_ID.fullmatch(str(tid)):
            raise ValueError("task id must match [A-Za-z0-9._-]{1,128}")
        return self.dir / f"{tid}.json"

    def _new_id(self) -> str:
        return f"task_{uuid.uuid4().hex[:12]}"

    #: A task description is work instructions another agent will claim and act
    #: on. One measured 2,000,000 characters.
    MAX_FIELD = 16_000

    def save(self, task: Task) -> None:
        target = self._path(task.id)
        payload = asdict(task)
        for field in ("subject", "description"):
            value = payload.get(field)
            if isinstance(value, str) and len(value) > self.MAX_FIELD:
                self.problems.append(
                    f"{task.id}: {field} truncated from {len(value):,} to "
                    f"{self.MAX_FIELD:,}"
                )
                payload[field] = value[:self.MAX_FIELD] + " [truncated]"
        # Mask the structure before serializing, not the serialized text:
        # `mask()` matches a secret's raw bytes, but `json.dumps` escapes
        # non-ASCII to `\uXXXX` and quotes/backslashes, so a credential carrying
        # any of those would survive a post-serialization mask onto this durable
        # board. `mask_payload` scrubs each value before it is escaped.
        if self.secrets is not None:
            payload = self.secrets.mask_payload(payload)
        atomic_write_text(target, json.dumps(payload, indent=2))

    def load(self, tid: str) -> Task | None:
        p = self._path(tid)
        if not p.exists():
            return None
        try:
            return Task(**json.loads(p.read_text()))
        except (OSError, json.JSONDecodeError, TypeError, ValueError) as exc:
            # Four exception types went to `return None`, which is the same
            # answer as "no such task". A board with a corrupt file therefore
            # reported one task where two existed, and an agent asking what is
            # left to do got a short answer with nothing anywhere saying it was
            # short. Writes here are atomic, so this is a foreign edit or a
            # half-finished write from a process that was killed -- either way
            # the store has a channel for it and this path never used it.
            self.problems.append(f"unreadable task {p.name}: {type(exc).__name__}")
            return None

    def list(self) -> list[Task]:
        tasks = [self.load(path.stem) for path in sorted(self.dir.glob("task_*.json"))]
        return [task for task in tasks if task is not None]

    def create(self, subject: str, description: str = "", blocked_by: list[str] | None = None,
               worktree: str | None = None) -> Task:
        with self._lock:
            dependencies = list(blocked_by or [])
            if any(not _SAFE_ID.fullmatch(str(dependency)) for dependency in dependencies):
                raise ValueError("blockedBy contains an invalid task id")
            if worktree and (worktree in (".", "..") or not _SAFE_WORKTREE.fullmatch(worktree)):
                raise ValueError("worktree name must match [A-Za-z0-9._-]{1,64}")
            task = Task(id=self._new_id(), subject=subject, description=description,
                        blockedBy=dependencies, worktree=worktree)
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
        with self._lock:
            task = self.load(tid)
            if task is None:
                return f"Error: task {tid} not found"
            if task.status != "pending":
                return f"Error: task {tid} is {task.status}, not claimable"
            if task.owner:
                return f"Error: task {tid} is already owned by {task.owner}"
            if not self.can_start(tid):
                return f"Error: task {tid} is blocked by incomplete deps {task.blockedBy}"
            task.owner, task.status = owner, "in_progress"
            self.save(task)
            return f"Claimed {tid} for {owner}"

    def complete(self, tid: str, owner: str | None = None) -> str:
        with self._lock:
            task = self.load(tid)
            if task is None:
                return f"Error: task {tid} not found"
            if task.status != "in_progress":
                return f"Error: task {tid} must be in_progress before completion (is {task.status})"
            if owner and task.owner and task.owner != owner:
                return f"Error: task {tid} is owned by {task.owner}, not {owner}"
            task.status = "completed"
            self.save(task)
            unblocked = [t.id for t in self.list()
                         if t.status == "pending" and t.blockedBy and self.can_start(t.id)]
            msg = f"Completed {tid}."
            if unblocked:
                msg += f" Now runnable: {', '.join(unblocked)}"
            return msg

    def bind_worktree(self, tid: str, name: str) -> str:
        with self._lock:
            if name in (".", "..") or not _SAFE_WORKTREE.fullmatch(name):
                return "Error: worktree name must match [A-Za-z0-9._-]{1,64}"
            task = self.load(tid)
            if task is None:
                return f"Error: task {tid} not found"
            task.worktree = name
            self.save(task)
            return f"Bound {tid} to worktree {name}"

    def runnable(self) -> list[Task]:
        with self._lock:
            return [task for task in self.list()
                    if task.status == "pending" and not task.owner and self.can_start(task.id)]

    def render(self) -> str:
        tasks = self.list()
        if not tasks:
            return "No tasks."
        # A task blocked by a dependency id that does not exist -- a typo, or a
        # blocker that was never created -- is permanently unrunnable: `can_start`
        # reads a missing dep as "not completed" and it never completes. Nothing
        # said so, so it looked exactly like a task waiting on real work. Surface
        # the missing dep in the id the agent reads (a legitimate forward
        # reference reads as MISSING only until its blocker is created, which is
        # accurate) and report it, the round-49 "does a failure report?" channel.
        existing = {t.id for t in tasks}
        glyph = {"pending": "[ ]", "in_progress": "[>]", "completed": "[x]"}
        lines = []
        # The missing-dependency scan runs over *every* task (a dead block on any
        # task must still be reported), even though only the tail is displayed --
        # the diagnostic is a property of the board, not of what fits on screen.
        for t in tasks:
            owner = f" @{t.owner}" if t.owner else ""
            worktree = f" (worktree: {t.worktree})" if t.worktree else ""
            if t.blockedBy:
                missing = [dep for dep in t.blockedBy if dep not in existing]
                if missing:
                    self.problems.append(
                        f"{t.id}: blocked by missing task(s) {missing}; it can "
                        "never start until they are created"
                    )
                    blocked = f" (blockedBy: {t.blockedBy}; MISSING: {missing})"
                else:
                    blocked = f" (blockedBy: {t.blockedBy})"
            else:
                blocked = ""
            subject = t.subject
            if len(subject) > MAX_SUBJECT_DISPLAY:
                subject = subject[:MAX_SUBJECT_DISPLAY] + "…"
            lines.append(f"{glyph.get(t.status, '[?]')} {t.id}: {subject}{owner}{blocked}{worktree}")
        if len(lines) > MAX_TASK_BOARD:
            hidden = len(lines) - MAX_TASK_BOARD
            lines = lines[-MAX_TASK_BOARD:]
            lines.insert(
                0, f"... ({hidden} older task(s) not shown; reference them by id)"
            )
        return "\n".join(lines)


def _store(ctx: ToolContext) -> TaskStore:
    store = ctx.state.get("tasks")
    if store is None:
        store = ctx.state["tasks"] = TaskStore(
            ctx.workspace, secrets=getattr(ctx.agent, "secrets", None)
        )
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
        "worktree": {"type": "string"},
    },
    "required": ["subject"],
}
_BY_ID = {"type": "object", "properties": {"task_id": {"type": "string"}}, "required": ["task_id"]}
_EMPTY = {"type": "object", "properties": {}}


def install_tasks(registry: ToolRegistry) -> ToolRegistry:
    async def create_task(ctx, subject, description="", blockedBy=None, worktree=None):
        t = await asyncio.to_thread(_store(ctx).create, subject, description, blockedBy, worktree)
        return f"Created {t.id}: {t.subject}"

    async def list_tasks(ctx):
        return await asyncio.to_thread(_store(ctx).render)

    async def get_task(ctx, task_id):
        t = await asyncio.to_thread(_store(ctx).load, task_id)
        return json.dumps(t.__dict__, indent=2) if t else f"Error: task {task_id} not found"

    async def claim_task(ctx, task_id):
        return await asyncio.to_thread(_store(ctx).claim, task_id, _owner(ctx))

    async def complete_task(ctx, task_id):
        return await asyncio.to_thread(_store(ctx).complete, task_id, _owner(ctx))

    registry.register(Tool("create_task", "Create a persistent task (optionally blockedBy other task ids).", _CREATE, create_task, risk="write"))
    registry.register(Tool("list_tasks", "List all persistent tasks and their status.", _EMPTY, list_tasks, readonly=True, risk="read"))
    registry.register(Tool("get_task", "Get one task's full JSON by id.", _BY_ID, get_task, readonly=True, risk="read"))
    registry.register(Tool("claim_task", "Claim a pending, unblocked task (sets you as owner).", _BY_ID, claim_task, risk="write"))
    registry.register(Tool("complete_task", "Mark a task completed; reports newly-unblocked tasks.", _BY_ID, complete_task, risk="write"))
    return registry
