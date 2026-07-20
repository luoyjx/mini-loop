"""Git worktree isolation (s18): session provisioning and task binding.

In learn-claude-code each task binds to a git worktree so concurrent teammates
editing the same repo don't collide. We already provision one workspace per
session, so the natural mapping is: make the *workspace itself* a git worktree
on its own branch. Drop in `worktree_workspace_factory(repo)` as the manager's
`workspace_factory` and every session gets an isolated branch + directory.

    SessionManager(settings, client,
                   workspace_factory=worktree_workspace_factory("/path/to/repo"))
"""

from __future__ import annotations

import asyncio
import re
import json
import subprocess
import time
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry
from .tasks import TaskStore

_NAME_OK = re.compile(r"^[A-Za-z0-9._-]{1,64}$")


def _git(repo: Path, *args: str) -> tuple[bool, str]:
    try:
        r = subprocess.run(
            ["git", *args], cwd=str(repo), capture_output=True, text=True, timeout=30
        )
        return r.returncode == 0, (r.stdout + r.stderr).strip()
    except (FileNotFoundError, NotADirectoryError, OSError, subprocess.TimeoutExpired) as e:
        return False, str(e)  # missing cwd or no git binary -> "not a git repo"


def is_git_repo(path: Path) -> bool:
    ok, _ = _git(Path(path), "rev-parse", "--is-inside-work-tree")
    return ok


def worktree_workspace_factory(repo_root, *, base: str = ".worktrees", branch_prefix: str = "wt/"):
    """Return a `workspace_factory(session_id) -> Path` that creates a git
    worktree per session. Falls back to a plain directory if `repo_root` isn't
    a git repo, so it's always safe to use."""
    repo_root = Path(repo_root).resolve()

    def factory(session_id: str) -> Path:
        candidate = str(session_id)
        if _NAME_OK.fullmatch(candidate) and candidate not in (".", ".."):
            name = candidate
        else:
            name = re.sub(r"[^A-Za-z0-9._-]", "_", candidate)[:64].strip(".") or "session"
        path = repo_root / base / name
        if path.exists():
            return path
        if not is_git_repo(repo_root):
            path.mkdir(parents=True, exist_ok=True)
            return path
        path.parent.mkdir(parents=True, exist_ok=True)
        ok, out = _git(repo_root, "worktree", "add", str(path), "-b", f"{branch_prefix}{name}", "HEAD")
        if not ok:  # branch may already exist, or other issue: degrade to plain dir
            path.mkdir(parents=True, exist_ok=True)
        return path

    return factory


def remove_worktree(repo_root, name: str, *, discard_changes: bool = False, branch_prefix: str = "wt/") -> str:
    """Remove a session's worktree. Refuses if it has uncommitted changes unless
    `discard_changes`."""
    return WorktreeManager(Path(repo_root), branch_prefix=branch_prefix).remove(
        name, discard_changes=discard_changes
    )


def list_worktrees(repo_root) -> str:
    ok, out = _git(Path(repo_root).resolve(), "worktree", "list")
    return out if ok else "(not a git repo)"


class WorktreeManager:
    """Audited task <-> worktree lifecycle used by the s18 tools."""

    def __init__(self, repo_root: Path, *, base: str = ".worktrees",
                 branch_prefix: str = "wt/") -> None:
        self.repo_root = Path(repo_root).resolve()
        self.base = base
        self.branch_prefix = branch_prefix
        self.root = self.repo_root / base
        self.events_path = self.root / "events.jsonl"

    def validate_name(self, name: str) -> str | None:
        if name in (".", "..") or not _NAME_OK.fullmatch(name):
            return "worktree name must match [A-Za-z0-9._-]{1,64}"
        return None

    def path_for(self, name: str) -> Path:
        error = self.validate_name(name)
        if error:
            raise ValueError(error)
        return self.root / name

    def _log(self, event_type: str, name: str, task_id: str = "") -> None:
        self.root.mkdir(parents=True, exist_ok=True)
        event = {"type": event_type, "worktree": name, "task_id": task_id, "ts": time.time()}
        with self.events_path.open("a") as stream:
            stream.write(json.dumps(event) + "\n")

    def create(self, name: str, *, task_id: str = "",
               task_store: TaskStore | None = None) -> str:
        error = self.validate_name(name)
        if error:
            return f"Error: {error}"
        if not is_git_repo(self.repo_root):
            return f"Error: {self.repo_root} is not a git repository"
        if task_id and (task_store is None or task_store.load(task_id) is None):
            return f"Error: task {task_id} not found"
        path = self.path_for(name)
        if path.exists():
            return f"Error: worktree {name} already exists"
        self.root.mkdir(parents=True, exist_ok=True)
        ok, output = _git(
            self.repo_root, "worktree", "add", str(path), "-b",
            f"{self.branch_prefix}{name}", "HEAD",
        )
        if not ok:
            return f"Error: {output}"
        if task_id and task_store is not None:
            task_store.bind_worktree(task_id, name)
        self._log("create", name, task_id)
        return f"Worktree '{name}' created at {path}"

    def _changes(self, name: str) -> tuple[int, int]:
        path = self.path_for(name)
        ok, status = _git(path, "status", "--porcelain")
        if not ok:
            return -1, -1
        files = len(status.splitlines()) if status else 0
        ok, base_head = _git(self.repo_root, "rev-parse", "HEAD")
        if not ok:
            return -1, -1
        ahead_ok, ahead = _git(path, "rev-list", "--count", f"{base_head}..HEAD")
        if not ahead_ok or not ahead.isdigit():
            return -1, -1
        return files, int(ahead)

    def remove(self, name: str, *, discard_changes: bool = False) -> str:
        error = self.validate_name(name)
        if error:
            return f"Error: {error}"
        path = self.path_for(name)
        if not path.exists():
            return f"No worktree {name}"
        files, commits = self._changes(name)
        if not discard_changes and files < 0:
            return (f"Refusing: could not verify worktree {name} status; "
                    "keep it or pass discard_changes=True")
        if not discard_changes and (files or commits):
            return (f"Refusing: worktree {name} has {files} changed file(s) and "
                    f"{commits} commit(s); keep it or pass discard_changes=True")
        ok, output = _git(self.repo_root, "worktree", "remove", str(path), "--force")
        if not ok:
            return f"Error: {output}"
        _git(self.repo_root, "branch", "-D", f"{self.branch_prefix}{name}")
        self._log("remove", name)
        return f"Removed worktree {name}"

    def keep(self, name: str) -> str:
        error = self.validate_name(name)
        if error:
            return f"Error: {error}"
        if not self.path_for(name).exists():
            return f"No worktree {name}"
        self._log("keep", name)
        return f"Worktree '{name}' kept for review (branch: {self.branch_prefix}{name})"

    def list(self) -> str:
        return list_worktrees(self.repo_root)


_CREATE = {
    "type": "object",
    "properties": {"name": {"type": "string"}, "task_id": {"type": "string"}},
    "required": ["name"],
}
_NAME = {"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}
_REMOVE = {
    "type": "object",
    "properties": {"name": {"type": "string"}, "discard_changes": {"type": "boolean"}},
    "required": ["name"],
}
_EMPTY = {"type": "object", "properties": {}}


def install_worktrees(registry: ToolRegistry) -> ToolRegistry:
    def service(ctx: ToolContext) -> WorktreeManager | None:
        return ctx.state.get("worktrees")

    def board(ctx: ToolContext) -> TaskStore:
        store = ctx.state.get("tasks")
        if store is None:
            root = ctx.state.get("team_workspace", ctx.workspace)
            store = ctx.state["tasks"] = TaskStore(root)
        return store

    async def create_worktree(ctx, name, task_id=""):
        manager = service(ctx)
        return (await asyncio.to_thread(manager.create, name, task_id=task_id, task_store=board(ctx))
                if manager else "Error: worktree repository is not configured")

    async def remove_worktree_tool(ctx, name, discard_changes=False):
        manager = service(ctx)
        return (await asyncio.to_thread(manager.remove, name, discard_changes=discard_changes)
                if manager else "Error: worktree repository is not configured")

    async def keep_worktree(ctx, name):
        manager = service(ctx)
        return (await asyncio.to_thread(manager.keep, name)
                if manager else "Error: worktree repository is not configured")

    async def list_worktrees_tool(ctx):
        manager = service(ctx)
        return (await asyncio.to_thread(manager.list)
                if manager else "Error: worktree repository is not configured")

    async def enter_worktree(ctx, name):
        manager = service(ctx)
        if manager is None:
            return "Error: worktree repository is not configured"
        path = manager.path_for(name)
        if not path.exists():
            return f"Error: no worktree {name}"
        ctx.agent.enter_workspace(path)
        return f"Entered worktree '{name}' at {path}"

    registry.register(Tool("create_worktree", "Create an isolated git worktree and optionally bind a task.",
                           _CREATE, create_worktree))
    registry.register(Tool("remove_worktree", "Remove a clean worktree, or explicitly discard its changes.",
                           _REMOVE, remove_worktree_tool))
    registry.register(Tool("keep_worktree", "Keep a worktree and record it for review.", _NAME, keep_worktree))
    registry.register(Tool("list_worktrees", "List git worktrees.", _EMPTY, list_worktrees_tool, readonly=True))
    registry.register(Tool("enter_worktree", "Switch this agent's file tools into a worktree.", _NAME, enter_worktree))
    return registry
