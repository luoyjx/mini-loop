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


def test_the_era_table_lines_up_the_acceptance_gauges_by_build(tmp_path):
    """One report, one row per build, newest first: the comparison an
    experiment is read on, with its sample sizes, instead of two CLI runs
    lined up by hand."""
    import time as time_module

    from mini_loop.mining import era_table, render_eras

    store = TrajectoryStore(tmp_path / "t")
    clock = {"now": 1000.0}
    real_time = time_module.time
    time_module.time = lambda: clock["now"]
    try:
        before = store.start(session_id="s", run_index=1, input_text="x",
                             metadata={"workspace": "/ws", "build": "aaaa1111"})
        for command in ("cd /ws && ls", "cd /repo && make", "ls"):
            store.append(before, {"type": "tool_use", "name": "bash",
                                  "input": {"command": command}, "id": "c"})
            store.append(before, {"type": "tool_result", "name": "bash",
                                  "output": "ok", "id": "c"})
        store.append(before, {"type": "tool_use", "name": "read_file",
                              "input": {"path": "/repo/a"}, "id": "r"})
        store.append(before, {"type": "tool_result", "name": "read_file",
                              "output": "Error: Path escapes workspace", "id": "r"})
        store.finish(before, status="completed", duration_ms=1.0)

        clock["now"] = 2000.0
        after = store.start(session_id="s", run_index=2, input_text="x",
                            metadata={"workspace": "/ws", "build": "bbbb2222"})
        store.append(after, {"type": "tool_use", "name": "bash",
                             "input": {"command": "ls"}, "id": "c"})
        store.append(after, {"type": "tool_result", "name": "bash",
                             "output": "ok", "id": "c"})
        store.finish(after, status="completed", duration_ms=1.0)
    finally:
        time_module.time = real_time

    rows = era_table(store)
    assert [r["build"] for r in rows] == ["bbbb2222", "aaaa1111"], "newest first"
    old, new = rows[1], rows[0]
    assert old["trajectories"] == 1 and old["commands"] == 3
    assert old["cwd_home"] == round(1 / 3, 3) and old["cwd_foreign"] == round(1 / 3, 3)
    assert old["read_calls"] == 1 and old["read_errors"] == 1
    assert old["read_error_rate"] == 1.0
    assert new["commands"] == 1 and new["cwd_home"] == 0.0 and new["cwd_foreign"] == 0.0
    assert new["read_calls"] == 0 and new["read_error_rate"] == 0.0

    text = render_eras(rows)
    assert text.splitlines()[0].startswith("# by build")
    assert "bbbb2222" in text.splitlines()[2] and "aaaa1111" in text.splitlines()[3]
    assert "1/1" in text.splitlines()[3]
    assert render_eras([]).endswith("(no trajectories in the window)")


def test_the_era_table_carries_cost_beside_behavior(tmp_path):
    """One row per build is the whole story only if it also says what the
    build cost: model calls, cache share, median call time (成本进报表不进
    裁决). A build with no model_end events reports zero cost, not a crash."""
    from mini_loop.mining import era_table, render_eras

    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s", run_index=1, input_text="x",
                      metadata={"workspace": "/ws", "build": "cccc3333"})
    for duration, usage in (
        (100.0, {"input_tokens": 1000, "cache_read_input_tokens": 0}),
        (300.0, {"input_tokens": 200, "cache_read_input_tokens": 1800}),
        (200.0, {"input_tokens": 100, "cache_read_input_tokens": 900}),
    ):
        store.append(tid, {"type": "model_end", "stop_reason": "tool_use",
                           "duration_ms": duration, "usage": usage})
    store.append(tid, {"type": "tool_use", "name": "bash",
                       "input": {"command": "ls"}, "id": "c"})
    store.append(tid, {"type": "tool_result", "name": "bash", "output": "ok", "id": "c"})
    store.finish(tid, status="completed", duration_ms=1.0)
    silent = store.start(session_id="s", run_index=2, input_text="x",
                         metadata={"workspace": "/ws", "build": "dddd4444"})
    store.finish(silent, status="completed", duration_ms=1.0)

    rows = {row["build"]: row for row in era_table(store)}
    priced = rows["cccc3333"]
    assert priced["calls"] == 3
    assert priced["cache_share"] == round(2700 / 4000, 3)
    assert priced["median_call_ms"] == 200.0
    assert "durations" not in priced
    free = rows["dddd4444"]
    assert free["calls"] == 0 and free["cache_share"] == 0.0 and free["median_call_ms"] == 0.0

    text = render_eras(era_table(store))
    assert "calls  cache  median ms" in text.splitlines()[1]
    line = next(l for l in text.splitlines() if l.startswith("cccc3333"))
    assert line.rstrip().endswith("3    68%       200")
