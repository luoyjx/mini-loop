"""Three defects found by auditing DeepSeek Harness's defensive patterns.

Each pattern is a bug class that shipped (or nearly shipped) in dsh, stated
as a rule; auditing mini-loop against the list found three live instances:

* **Report orthogonal outcomes independently** -- a command can time out AND
  have printed the diagnostic that explains why. `CommandResult.render()`
  nested the whole report inside the error branch, so the model saw
  `Error: Timeout` and the loop's last line -- the actual clue -- was
  discarded (test in test_command_result.py; the workspace variants here).
* **Unlink link-shaped paths** -- `shutil.rmtree` refuses a path that is
  itself a symlink and `ignore_errors=True` swallowed the refusal, so a
  workspace replaced by a link was silently never reclaimed. The rule:
  unlink deletes the link and never follows it into the target; recursive
  deletion is reserved for known real directories.
* **Contain callback exceptions in the dispatcher** -- the injected event
  sink is a user-supplied observer, and one that throws killed the turn
  that emitted. Contained now, and reported through `info()` rather than
  swallowed.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import _remove_workspace

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


# --- unlink link-shaped paths ----------------------------------------------


def test_a_real_workspace_is_removed_recursively(tmp_path):
    workspace = tmp_path / "ws"
    (workspace / "sub").mkdir(parents=True)
    (workspace / "sub" / "f.txt").write_text("x")
    _remove_workspace(workspace)
    assert not workspace.exists()


def test_a_link_shaped_workspace_is_unlinked_never_followed(tmp_path):
    victim = tmp_path / "victim"
    victim.mkdir()
    (victim / "precious.txt").write_text("do not delete")
    link = tmp_path / "ws"
    link.symlink_to(victim)

    _remove_workspace(link)

    assert not link.exists(), "the link itself must be reclaimed"
    assert (victim / "precious.txt").read_text() == "do not delete", (
        "removal must never reach through the link into its target"
    )


# --- contain callback exceptions in the dispatcher -------------------------


def test_a_throwing_event_sink_cannot_kill_the_turn(tmp_path):
    calls = {"n": 0}

    def bad_sink(event):
        calls["n"] += 1
        raise RuntimeError("observer exploded")

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
        event_sink=bad_sink,
    )
    session = manager.create()
    result = asyncio.run(session.run("hello"))
    assert result  # the turn completed despite the sink
    assert calls["n"] >= 1  # the sink genuinely ran and threw
    assert "observer exploded" in (session.sink_error or "")
    assert session.info()["sink_error"] == session.sink_error
