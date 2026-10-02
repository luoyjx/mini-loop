"""Census: two kinds of `cd`, two levers (§5, 2026-09-02, loop round 3).

Every organic session in the corpus opened the same way: `cd <its own
workspace> && ls -la`, then `cd <the repository it was really asked
about>`. The single cwd_distrust gauge (share of cd-prefixed commands)
folded both into one number, so neither lever could be read on its own:
a clearer cwd contract in the bash tool should move the first kind
("home"), workspace binding should move the second ("foreign"). The
profile now reports both, from the workspace each trajectory recorded.

Experiment J rides in the same change: the bash description states that
the workspace IS the working directory. Its instrument is the "home"
share on organic sessions after this lands.
"""

import pathlib

from mini_loop.mining import bash_profile, render_bash
from mini_loop.tools import BASH
from mini_loop.trajectory import TrajectoryStore


def _bash(store, tid, command, output="ok"):
    store.append(tid, {"type": "tool_use", "name": "bash",
                       "input": {"command": command}, "id": "c"})
    store.append(tid, {"type": "tool_result", "name": "bash",
                       "output": output, "id": "c"})


def test_the_bash_profile_splits_home_from_foreign(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s1", run_index=1, input_text="look",
                      metadata={"workspace": "/srv/ws/s1"})
    _bash(store, tid, "cd /srv/ws/s1 && ls -la")          # home: absolute
    _bash(store, tid, "cd sub && ls")                      # home: relative
    _bash(store, tid, "cd /srv/repo && make test")         # foreign
    _bash(store, tid, "cd .. && ls")                       # foreign: escapes
    _bash(store, tid, "ls")                                # no cd at all
    store.finish(tid, status="completed", duration_ms=1.0)

    profile = bash_profile(store)
    assert profile["commands"] == 5
    assert profile["cwd_distrust"] == 0.8
    assert profile["cwd_home"] == 0.4
    assert profile["cwd_foreign"] == 0.4
    assert profile["foreign_targets"] == {"/srv/repo": 1, "..": 1}

    text = render_bash(profile)
    assert "home 40%" in text and "foreign 40%" in text
    assert "- /srv/repo: 1" in text


def test_a_run_without_a_recorded_workspace_cannot_be_classified(tmp_path):
    """No workspace on the header: the cd is still distrust, but neither
    home nor foreign -- reported as neither rather than guessed."""
    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s1", run_index=1, input_text="look")
    _bash(store, tid, "cd /anywhere && ls")
    store.finish(tid, status="completed", duration_ms=1.0)

    profile = bash_profile(store)
    assert profile["cwd_distrust"] == 1.0
    assert profile["cwd_home"] == 0.0 and profile["cwd_foreign"] == 0.0
    assert profile["foreign_targets"] == {}


def test_the_listing_row_carries_the_recorded_workspace(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s1", run_index=1, input_text="x",
                      metadata={"workspace": "/srv/ws/s1"})
    store.finish(tid, status="completed", duration_ms=1.0)
    assert store.summary(tid)["workspace"] == "/srv/ws/s1"
    assert store.list()[0]["workspace"] == "/srv/ws/s1"


def test_the_bash_tool_states_its_cwd_contract():
    """Experiment J: the description names the working directory and says
    cd is unnecessary; "in the workspace" alone left the model hedging."""
    description = BASH["description"]
    assert "working directory" in description
    assert "no cd is needed" in description
    assert "stdout+stderr" in description, "the return contract survives"
