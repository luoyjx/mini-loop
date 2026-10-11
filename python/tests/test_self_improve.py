"""Self-modification proposes on a branch; landing it stays human (L4).

The composition of the verified loop and a git checkout, pinned at its
stopping point:

* a verified proposal carries the branch, the diff stat, and the receipt
  trail -- and the source repository's history is untouched: no merge, no
  push, no checkout mutation outside the worktree;
* a workspace that is not a git checkout is refused -- an unreviewable
  "improvement" is just a mutation;
* an empty acceptance command is refused -- without an auditor command,
  "verified" would be a vibe;
* an unverified outcome is reported as such, diff and all, so the human
  sees what was attempted and what the auditor refused.
"""

import asyncio
import pathlib
import subprocess

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.self_improve import propose_improvement

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _git(cwd, *args):
    return subprocess.run(["git", *args], cwd=cwd, capture_output=True,
                          text=True, check=True)


def _repo_workspace_session(tmp_path, responder):
    """A session whose workspace is its own git checkout on a branch."""

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder),
        tool_registry=full_registry(),
    )
    session = manager.create()
    ws = session.workspace
    _git(ws, "init", "-b", "main")
    _git(ws, "config", "user.email", "t@t")
    _git(ws, "config", "user.name", "t")
    (ws / "README.md").write_text("baseline\n")
    _git(ws, "add", "-A")
    _git(ws, "commit", "-m", "baseline")
    _git(ws, "checkout", "-b", "proposal/x")
    return session


def test_a_verified_proposal_carries_branch_and_diff_and_merges_nothing(tmp_path):
    executor = scripted([
        ([text("improving"),
          tool("write_file", _id="w1", path="improved.txt", content="better")],
         "tool_use"),
        ([text("improved")], "end_turn"),
    ])
    session = _repo_workspace_session(tmp_path, executor)
    events = []
    queue = session.subscribe()

    proposal = asyncio.run(propose_improvement(
        session, "add improved.txt",
        acceptance_command="test -f improved.txt",
    ))

    assert proposal["verified"] is True
    assert proposal["branch"] == "proposal/x"
    assert "improved.txt" in proposal["diff_stat"]
    assert "merge only after" in proposal["next"]
    # The human's gate is real: main still holds only the baseline commit.
    main_files = _git(session.workspace, "ls-tree", "--name-only", "main").stdout
    assert "improved.txt" not in main_files
    while not queue.empty():
        events.append(queue.get_nowait())
    assert any(e["type"] == "improvement_proposed" and e["verified"]
               for e in events)


def test_a_non_git_workspace_is_refused(tmp_path):
    executor = scripted([([text("hi")], "end_turn")])
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=executor),
        tool_registry=full_registry(),
    )
    session = manager.create()

    with pytest.raises(ValueError, match="git checkout"):
        asyncio.run(propose_improvement(
            session, "anything", acceptance_command="true",
        ))


def test_an_empty_acceptance_command_is_refused(tmp_path):
    executor = scripted([([text("hi")], "end_turn")])
    session = _repo_workspace_session(tmp_path, executor)
    with pytest.raises(ValueError, match="acceptance command"):
        asyncio.run(propose_improvement(
            session, "anything", acceptance_command="   ",
        ))


def test_an_unverified_outcome_is_reported_not_hidden(tmp_path):
    executor = scripted([
        ([text("attempting"),
          tool("write_file", _id="w1", path="wrong.txt", content="miss")],
         "tool_use"),
        ([text("done, I think")], "end_turn"),
    ])
    session = _repo_workspace_session(tmp_path, executor)

    proposal = asyncio.run(propose_improvement(
        session, "add improved.txt",
        acceptance_command="test -f improved.txt",
        max_rounds=1,
    ))

    assert proposal["verified"] is False
    assert "without verification" in proposal["summary"]
    assert "wrong.txt" in proposal["diff_stat"], (
        "the human must see what was attempted, especially when it failed"
    )
