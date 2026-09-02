"""Census: era slicing by build, not by clock (§5, 2026-09-02, round 4).

Three organic trajectories landed in one hour, two of them opening without
the habitual `cd` -- the shape experiment J was meant to produce -- but J
had not been committed yet. Either the server was reloading working-tree
edits mid-experiment or the model simply varied; the trajectory could not
say, because it recorded when it ran and not on what code. identity.py
already tells this story from the /healthz side (a whole round of
measurements once taken against a stale process). The evidence side now
carries the same fingerprint: every trajectory header records build_id(),
the listing row exposes it, and the miner slices by it and reports the
builds a reading mixes.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.identity import build_id
from mini_loop.mining import bash_profile, mine, render
from mini_loop.trajectory import TrajectoryStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def test_a_recorded_run_names_the_build_it_executed_on(tmp_path):
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    session = manager.create()
    asyncio.run(session.run("hello"))

    (row,) = manager.trajectories.list(session_id=session.id)
    assert row["build"] == build_id()
    assert len(row["build"]) >= 8


def _record(store, command, build):
    tid = store.start(session_id="s", run_index=1, input_text="x",
                      metadata={"workspace": "/ws", "build": build})
    store.append(tid, {"type": "tool_use", "name": "bash",
                       "input": {"command": command}, "id": "c"})
    store.append(tid, {"type": "tool_result", "name": "bash", "output": "ok",
                       "id": "c"})
    store.finish(tid, status="completed", duration_ms=1.0)
    return tid


def test_the_miner_slices_by_build_prefix_and_names_the_mix(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    old = _record(store, "cd /ws && ls", "aaaa1111")
    new = _record(store, "ls", "bbbb2222")
    legacy = store.start(session_id="s", run_index=1, input_text="x")
    store.finish(legacy, status="completed", duration_ms=1.0)

    everything = mine(store)
    assert everything["trajectories"] == 3
    assert everything["builds"] == {"aaaa1111": 1, "bbbb2222": 1, "(unrecorded)": 1}
    assert "builds: " in render(everything) and "(unrecorded) x1" in render(everything)

    before = mine(store, build="aaaa")
    assert [r["trajectory_id"] for r in before["rows"]] == [old]
    after = mine(store, build="bbbb")
    assert [r["trajectory_id"] for r in after["rows"]] == [new]
    assert mine(store, build="cccc")["trajectories"] == 0

    # The same slice reaches every profile: the cwd gauges read per build.
    assert bash_profile(store, build="aaaa")["cwd_distrust"] == 1.0
    assert bash_profile(store, build="bbbb")["cwd_distrust"] == 0.0
