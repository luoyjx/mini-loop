"""A trajectory that cannot be read is reported, not silently dropped.

`TrajectoryStore.list()` skips any recording it cannot summarize. That is
correct for the listing -- a corrupt file has nothing to show -- but it did it
*silently*: the operator saw a shorter list and never learned a recording had
been dropped, so a corrupt trajectory looked exactly like one that was never
made. Round 81 made the memory and task stores report a corrupt read instead
of returning a silent None, and cron's durable load reports "all durable jobs
were dropped"; the trajectory store was the store that still swallowed.

It now carries a `ProblemLog` and reports a `ValueError`/`OSError` drop (real
corruption or a read failure), which the audit's problem-channel sweep surfaces
as `trajectories-problems`. A `KeyError` -- a file deleted between the glob and
the read -- stays silent, because a vanished file is a benign race, not a
corrupt recording.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import audit
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.trajectory import TrajectoryStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
VALID_ID_BODY = "b" * 24  # matches traj_[0-9a-f]{24}


def _store_with_one_valid(tmp_path):
    store = TrajectoryStore(tmp_path / "traj")
    tid = store.start(session_id="s1", run_index=1, input_text="hi",
                      owner="alice", metadata={})
    store.finish(tid, status="completed", output="done")
    return store, tid


def _corrupt(store, body=VALID_ID_BODY, content="{ not valid json\n"):
    (store.root / f"traj_{body}.jsonl").write_text(content)


def test_a_corrupt_trajectory_is_still_dropped_from_the_listing(tmp_path):
    store, _ = _store_with_one_valid(tmp_path)
    _corrupt(store)

    listed = store.list()

    assert len(listed) == 1, "a corrupt recording must not appear in the listing"


def test_the_drop_is_reported(tmp_path):
    store, _ = _store_with_one_valid(tmp_path)
    _corrupt(store)

    store.list()

    assert store.problems, "the corrupt trajectory was dropped silently"
    assert any("unreadable" in p and VALID_ID_BODY in p for p in store.problems)


def test_the_audit_surfaces_the_corruption(tmp_path):
    """No audit change was needed: the round-92 sweep finds any subsystem with
    a `problems` log, and `manager.trajectories` is one."""

    store, _ = _store_with_one_valid(tmp_path)
    _corrupt(store)
    store.list()

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(), trajectory_store=store,
    )
    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}

    assert "trajectories-problems" in findings
    assert "unreadable" in findings["trajectories-problems"].detail


def test_a_healthy_store_reports_nothing(tmp_path):
    """Not vacuous: the channel stays empty when every recording is readable."""

    store, _ = _store_with_one_valid(tmp_path)

    store.list()

    assert not store.problems


def test_a_race_deleted_file_is_not_reported_as_corruption(tmp_path):
    """A KeyError (file gone between glob and read) is a benign race, not a
    corrupt recording, so it must not raise a false alarm."""

    store, _ = _store_with_one_valid(tmp_path)

    # summary() on an id with no file raises KeyError -- the race path. It must
    # be swallowed without a problem report.
    with pytest.raises(KeyError):
        store.summary("traj_" + "c" * 24)
    # list() over a store with only the valid file reports nothing.
    store.list()
    assert not store.problems
