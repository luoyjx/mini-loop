"""Paging a long file is not a repeated read.

The N=2 real reading (docs/RSI_RESEARCH_AND_PLAN.md §5, 2026-09-02) put the
restricted paging task's `repeated_reads` at 1.5 vs 0.5 between two arms of
IDENTICAL code -- a 67% swing on the one task built to observe paging. The
instrument was booking every second read of a path as waste, offsets or
not, so the legitimate motion of paging through data.log with offsets was
the "waste" it reported. A repeat is the same WINDOW (path, offset, limit)
asked for twice; the bench and the miner now share that rule.
"""

import asyncio
import pathlib

from mini_loop import Settings
from mini_loop.benchmark import BenchTask, run_arm
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.mining import mine, mine_trajectory
from mini_loop.trajectory import TrajectoryStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path):
    return Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                    skills_dir=SKILLS, spill_dir=None)


def _seed(workspace):
    (workspace / "data.log").write_text(
        "".join(f"line-{n:05d}\n" for n in range(1, 301)))


def test_the_bench_counts_a_repeat_by_window_not_by_path(tmp_path):
    task = BenchTask("page", "find line 250", lambda ws, final: "done" in final,
                     setup=_seed)
    paging = FakeAsyncAnthropic(responder=scripted([
        ([tool("read_file", _id="r1", path="data.log", limit=100)], "tool_use"),
        ([tool("read_file", _id="r2", path="data.log", offset=200, limit=100)],
         "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    (row,) = asyncio.run(run_arm("x", _settings(tmp_path), paging, (task,)))
    assert row["tool_calls"] == 2 and row["tool_errors"] == 0
    assert row["repeated_reads"] == 0, (
        "two different windows of one file are paging, not a repeat"
    )

    same_window = FakeAsyncAnthropic(responder=scripted([
        ([tool("read_file", _id="r1", path="data.log", offset=200, limit=100)],
         "tool_use"),
        ([tool("read_file", _id="r2", path="data.log", offset=200, limit=100)],
         "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    (row,) = asyncio.run(run_arm("y", _settings(tmp_path / "2"), same_window,
                                 (task,)))
    assert row["repeated_reads"] == 1, "the same window twice is still waste"


def test_the_miner_shares_the_window_rule(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s1", run_index=1, input_text="page")
    for offset in (None, 100, 200):
        inputs = {"path": "data.log"} if offset is None else {
            "path": "data.log", "offset": offset}
        store.append(tid, {"type": "tool_use", "name": "read_file",
                           "input": inputs, "id": "c"})
        store.append(tid, {"type": "tool_result", "name": "read_file",
                           "output": "lines", "id": "c"})
    # ...and one honest repeat of the last page.
    store.append(tid, {"type": "tool_use", "name": "read_file",
                       "input": {"path": "data.log", "offset": 200}, "id": "c"})
    store.append(tid, {"type": "tool_result", "name": "read_file",
                       "output": "lines", "id": "c"})
    store.finish(tid, status="completed", duration_ms=10.0)

    mined = mine_trajectory(store, tid)
    assert mined["tool_calls"] == 4
    assert mined["repeated_reads"] == 1
    assert mined["reread_paths"] == {"data.log": 2}, (
        "the hotspot names the path, counting only same-window repeats"
    )
    assert mine(store)["reread_hotspots"] == {"data.log": 1}
