"""The trajectory miner folds real recordings into friction reports.

Pinned: the fold speaks the benchmark's behavioral vocabulary (rounds,
tool_calls, tool_errors, repeated_reads) computed from recorded events;
error and re-read hotspots aggregate across trajectories; the render is
a bounded text projection. First real run (2026-09-02, 76 trajectories)
immediately surfaced a 64/66 read_file error hotspot -- every one an
absolute path refused by workspace confinement -- which is exactly the
kind of observed friction this module exists to surface.
"""

import pathlib

from mini_loop.mining import mine, mine_trajectory, render
from mini_loop.trajectory import TrajectoryStore


def _record(store, session, events, status="completed"):
    tid = store.start(session_id=session, run_index=1, input_text="work")
    for event in events:
        store.append(tid, event)
    store.finish(tid, status=status, duration_ms=100.0)
    return tid


def _use(name, **inputs):
    return {"type": "tool_use", "name": name, "input": inputs, "id": "c1"}


def _result(name, output):
    return {"type": "tool_result", "name": name, "output": output, "id": "c1"}


def test_one_trajectory_folds_into_behavioral_metrics(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    tid = _record(store, "s1", [
        {"type": "model_start", "span_id": "m1"},
        _use("read_file", path="a.txt"),
        _result("read_file", "Error: Path escapes workspace: /abs/a.txt"),
        {"type": "model_start", "span_id": "m2"},
        _use("read_file", path="a.txt"),
        _result("read_file", "contents"),
        _use("bash", command="echo hi"),
        _result("bash", "hi"),
        {"type": "model_start", "span_id": "m3"},
        _use("task", prompt="delegate"),
        _result("task", "Unknown tool: task"),
    ])

    mined = mine_trajectory(store, tid)
    assert mined["rounds"] == 3
    assert mined["tool_calls"] == 4
    assert mined["tool_errors"] == 2, "Error and Unknown tool both count"
    assert mined["repeated_reads"] == 1
    assert mined["per_tool"]["read_file"] == {"calls": 2, "errors": 1}
    assert mined["reread_paths"] == {"a.txt": 2}


def test_mining_aggregates_and_ranks_hotspots(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    for _ in range(2):
        _record(store, "s1", [
            _use("read_file", path="hot.log"),
            _result("read_file", "Error: nope"),
            _use("read_file", path="hot.log"),
            _result("read_file", "fine"),
            _use("bash", command="true"),
            _result("bash", "(no output)"),
        ])

    report = mine(store)
    assert report["trajectories"] == 2
    assert report["totals"]["tool_calls"] == 6
    assert report["totals"]["tool_errors"] == 2
    assert report["per_tool"]["read_file"] == {"calls": 4, "errors": 2}
    assert report["reread_hotspots"] == {"hot.log": 2}
    assert list(report["error_hotspots"]) == ["read_file"], (
        "only erroring tools appear, ranked by error count"
    )

    text = render(report)
    assert "2 trajectories" in text
    assert "read_file: 4 calls (2/4 errors)" in text
    assert "hot.log: 2 redundant read(s)" in text


def test_the_bash_profile_names_cwd_distrust_and_repeats(tmp_path):
    """The corpus's first profile showed 97% of commands re-establishing
    the working directory with a cd prefix -- workload/workspace
    mismatch, the same root as the absolute-path read errors. The
    profile names that rate, the head histogram, error heads, and
    repeated identical commands."""

    from mini_loop.mining import bash_profile, render_bash

    store = TrajectoryStore(tmp_path / "t")
    _record(store, "s1", [
        _use("bash", command="cd /repo && make test"),
        _result("bash", "FAILED\n(exit 2)"),
        _use("bash", command="cd /repo && make test"),
        _result("bash", "ok"),
        _use("bash", command="ls -la"),
        _result("bash", "files"),
        _use("read_file", path="a.txt"),
        _result("read_file", "ignored by the bash profile"),
    ])

    profile = bash_profile(store)
    assert profile["commands"] == 3
    assert profile["cwd_distrust"] == round(2 / 3, 3)
    assert profile["heads"] == {"cd": 2, "ls": 1}
    assert profile["error_heads"] == {"cd": 1}, (
        "the (exit N) note marks a failed command"
    )
    assert profile["repeated_commands"] == {"cd /repo && make test": 1}

    text = render_bash(profile)
    assert "3 commands" in text
    assert "67% of commands" in text
    assert "1x extra: cd /repo && make test" in text


def test_the_miner_is_read_only(tmp_path):
    """Mining must not grow a write surface: the store's files are
    byte-identical before and after a full mine+render pass."""

    store = TrajectoryStore(tmp_path / "t")
    _record(store, "s1", [_use("bash", command="true"),
                          _result("bash", "ok")])
    before = {p: p.read_bytes() for p in (tmp_path / "t").rglob("*.jsonl")}
    render(mine(store))
    after = {p: p.read_bytes() for p in (tmp_path / "t").rglob("*.jsonl")}
    assert before == after
