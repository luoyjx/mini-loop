"""Git worktree isolation (s18), expressed through our `workspace_factory` seam.

In learn-claude-code each task binds to a git worktree so concurrent teammates
editing the same repo don't collide. We already provision one workspace per
session, so the natural mapping is: make the *workspace itself* a git worktree
on its own branch. Drop in `worktree_workspace_factory(repo)` as the manager's
`workspace_factory` and every session gets an isolated branch + directory.

    SessionManager(settings, client,
                   workspace_factory=worktree_workspace_factory("/path/to/repo"))
"""

from __future__ import annotations

import re
import subprocess
from pathlib import Path

_NAME_OK = re.compile(r"^[A-Za-z0-9._-]{1,64}$")


def _git(repo: Path, *args: str) -> tuple[bool, str]:
    try:
        r = subprocess.run(["git", *args], cwd=str(repo), capture_output=True, text=True)
        return r.returncode == 0, (r.stdout + r.stderr).strip()
    except (FileNotFoundError, NotADirectoryError, OSError) as e:
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
        name = session_id if _NAME_OK.match(session_id) else re.sub(r"[^A-Za-z0-9._-]", "_", session_id)[:64]
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
    repo_root = Path(repo_root).resolve()
    path = repo_root / ".worktrees" / name
    if not path.exists():
        return f"No worktree {name}"
    if not discard_changes:
        ok, status = _git(path, "status", "--porcelain")
        if ok and status:
            return f"Refusing: worktree {name} has uncommitted changes (pass discard_changes=True)"
    _git(repo_root, "worktree", "remove", str(path), "--force")
    _git(repo_root, "branch", "-D", f"{branch_prefix}{name}")
    return f"Removed worktree {name}"


def list_worktrees(repo_root) -> str:
    ok, out = _git(Path(repo_root).resolve(), "worktree", "list")
    return out if ok else "(not a git repo)"
