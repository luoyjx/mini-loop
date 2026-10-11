"""Removing a worktree told git not to look.

Same instrument as round 55, next-worst module: `worktrees.py` was at 70%, and
the uncovered block was the removal path and the tool handlers -- the same
region that held the previous round's defect.

`remove()` refuses when the worktree has uncommitted changes or unmerged
commits, which is a careful check. It then ran:

    git worktree remove <path> --force

**unconditionally**, so git's own identical check was disabled and the entire
guarantee rested on the Python one, with a window between them. Verified against
a real repository: with work that lands after the check, git refuses (rc=128)
and `--force` removes it anyway.

`--force` and `branch -D` are now used only when the caller asked to discard, so
git backs up the harness rather than being overruled by it. Two independent
checks that agree are worth more than one check plus an override.
"""

import pathlib
import subprocess

import pytest

from mini_loop.worktrees import (
    WorktreeManager,
    is_git_repo,
    list_worktrees,
    worktree_workspace_factory,
)


def _git(repo, *args):
    return subprocess.run(["git", *args], cwd=str(repo),
                          capture_output=True, text=True)


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "repo"
    root.mkdir()
    _git(root, "init", "-q")
    _git(root, "config", "user.email", "t@example.com")
    _git(root, "config", "user.name", "Test")
    (root / "a.txt").write_text("hello")
    _git(root, "add", ".")
    _git(root, "commit", "-qm", "init")
    return root


@pytest.fixture
def manager(repo):
    return WorktreeManager(repo)


# --- the defect -----------------------------------------------------------

def test_work_arriving_after_the_check_is_not_destroyed(manager):
    """The window the unconditional `--force` left open."""
    manager.create("raced")
    path = manager.path_for("raced")
    assert manager._changes("raced") == (0, 0)

    (path / "raced.txt").write_text("work that arrived after the check")
    result = manager.remove("raced")

    assert result.startswith("Refusing:")
    assert path.exists()
    assert (path / "raced.txt").read_text() == "work that arrived after the check"


def test_git_refuses_what_the_python_check_missed(manager, monkeypatch):
    """The second line of defence, exercised on its own.

    The test above is stopped by `_changes`, so it never reaches git and cannot
    tell `--force` from its absence -- the mutation runner said so. Here the
    harness's check is made to report clean while the worktree really is dirty,
    which is the only state where git's independent check is what saves the
    work.
    """
    manager.create("blind-spot")
    path = manager.path_for("blind-spot")
    (path / "unseen.txt").write_text("work the harness did not notice")
    monkeypatch.setattr(manager, "_changes", lambda name: (0, 0))

    result = manager.remove("blind-spot")

    assert result.startswith("Error:"), (
        "the harness passed it through and git was told not to look"
    )
    assert path.exists()
    assert (path / "unseen.txt").exists()


def test_discarding_is_still_possible_when_asked(manager):
    manager.create("scratch")
    path = manager.path_for("scratch")
    (path / "junk.txt").write_text("throwaway")

    assert manager.remove("scratch", discard_changes=True).startswith("Removed")
    assert not path.exists()


def test_a_clean_worktree_is_removed_without_force(manager):
    """The fix must not make ordinary cleanup fail."""
    manager.create("clean-one")
    assert manager.remove("clean-one").startswith("Removed")
    assert not manager.path_for("clean-one").exists()


def test_a_branch_with_unmerged_commits_survives_a_plain_remove(manager):
    manager.create("has-commits")
    path = manager.path_for("has-commits")
    (path / "new.txt").write_text("committed work")
    _git(path, "add", ".")
    _git(path, "commit", "-qm", "work")

    assert manager.remove("has-commits").startswith("Refusing:")
    branches = _git(manager.repo_root, "branch", "--list", "wt/has-commits").stdout
    assert "wt/has-commits" in branches


# --- the guard that was already right ------------------------------------

def test_a_dirty_worktree_is_refused(manager):
    manager.create("dirty")
    (manager.path_for("dirty") / "changed.txt").write_text("uncommitted")
    assert manager.remove("dirty").startswith("Refusing:")


def test_an_unverifiable_worktree_is_kept(manager, monkeypatch):
    """`_changes` returns (-1, -1) when it cannot tell, and not knowing must
    mean keep."""
    manager.create("opaque")
    monkeypatch.setattr(manager, "_changes", lambda name: (-1, -1))
    assert "could not verify" in manager.remove("opaque")
    assert manager.path_for("opaque").exists()


# --- names, which come from the model ------------------------------------

@pytest.mark.parametrize("name", [
    "..", ".", "../escape", "a/b", "", "x" * 65, "with space", "semi;colon",
])
def test_an_unsafe_name_is_refused(manager, name):
    assert manager.validate_name(name) is not None
    assert manager.create(name).startswith("Error:")
    with pytest.raises(ValueError):
        manager.path_for(name)


@pytest.mark.parametrize("name", ["feature-x", "a.b_c-1", "X9"])
def test_an_ordinary_name_is_accepted(manager, name):
    assert manager.validate_name(name) is None
    assert manager.create(name).startswith("Worktree")


def test_creating_twice_is_refused(manager):
    manager.create("once")
    assert manager.create("once").startswith("Error:")


def test_creating_outside_a_repository_is_refused(tmp_path):
    plain = tmp_path / "not-a-repo"
    plain.mkdir()
    assert WorktreeManager(plain).create("x").startswith("Error:")


def test_removing_something_that_is_not_there(manager):
    assert manager.remove("never-made") == "No worktree never-made"


# --- the fallback that makes the factory safe anywhere -------------------

def test_the_factory_falls_back_outside_a_repository(tmp_path):
    """Documented as "always safe to use", which had no test."""
    plain = tmp_path / "plain"
    plain.mkdir()
    assert not is_git_repo(plain)

    factory = worktree_workspace_factory(plain)
    workspace = factory("session-1")
    assert workspace.exists() and workspace.is_dir()


def test_the_factory_creates_a_worktree_inside_a_repository(repo):
    factory = worktree_workspace_factory(repo)
    workspace = factory("session-1")
    assert workspace.exists()
    assert "session-1" in list_worktrees(repo)


def test_keeping_a_worktree_records_the_branch(manager):
    manager.create("for-review")
    kept = manager.keep("for-review")
    assert "wt/for-review" in kept
    assert manager.path_for("for-review").exists()


def test_the_lifecycle_is_logged(manager):
    manager.create("logged")
    manager.remove("logged")
    events = manager.events_path.read_text().splitlines()
    kinds = [line for line in events if "logged" in line]
    assert any('"create"' in line for line in kinds)
    assert any('"remove"' in line for line in kinds)
