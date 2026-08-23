"""Recordings can now be purged with their session (roadmap G10).

Recordings deliberately OUTLIVE their session: the durable owner field
exists precisely so a trajectory stays readable after its session is gone
(round 74), and test_trajectory_ownership pins that contract -- this
round's first draft defaulted the other way and those tests caught it.
What was missing was the other half of G10's "can it be safely deleted":
there was NO way to purge a session's recordings at all, at any layer.

Pinned here:

* the default preserves the outlives contract -- delete() alone retains;
* delete(remove_trajectories=True) removes the session's recordings and
  spares every other session's;
* a file whose header cannot be read is left in place and reported:
  "cannot prove it is this session's" falls toward keeping bytes.
"""

import asyncio
from pathlib import Path

from mini_loop import SessionManager, Settings, TrajectoryStore
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path):
    return Settings(
        model="test-model",
        workspace_root=tmp_path / "workspaces",
        skills_dir=SKILLS_DIR,
        trajectory_root=tmp_path / "trajectories",
        trajectory_enabled=True,
    )


def test_deleting_a_session_removes_its_recordings_and_spares_others(tmp_path):
    async def main():
        manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
        doomed, kept = manager.create(), manager.create()
        await doomed.run("record me")
        await kept.run("keep me")

        assert manager.trajectories.count(doomed.id) == 1
        assert manager.trajectories.count(kept.id) == 1

        assert manager.delete(doomed.id, remove_trajectories=True) is True
        # The async close path may finish the reclamation a tick later.
        for _ in range(20):
            if manager.trajectories.count(doomed.id) == 0:
                break
            await asyncio.sleep(0.05)

        assert manager.trajectories.count(doomed.id) == 0, (
            "the deleted session's recordings survived it"
        )
        assert manager.trajectories.count(kept.id) == 1, (
            "another session's recordings were caught in the reclamation"
        )

    asyncio.run(main())


def test_the_default_preserves_the_outlives_contract(tmp_path):
    async def main():
        manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
        session = manager.create()
        await session.run("record me")

        assert manager.delete(session.id) is True
        await asyncio.sleep(0.1)
        assert manager.trajectories.count(session.id) == 1, (
            "a plain delete must keep recordings readable for their owner "
            "(test_trajectory_ownership pins the same contract end to end)"
        )

    asyncio.run(main())


def test_store_level_deletion_counts_and_scopes(tmp_path):
    store = TrajectoryStore(tmp_path / "trajectories")
    for _ in range(2):
        tid = store.start(session_id="doomed", run_index=1, input_text="x")
        store.finish(tid, status="completed", output="done")
    other = store.start(session_id="other", run_index=1, input_text="y")
    store.finish(other, status="completed", output="done")

    assert store.delete_for_session("doomed") == 2
    assert store.count("doomed") == 0
    assert store.count("other") == 1


def test_an_unreadable_header_is_left_in_place_and_reported(tmp_path):
    root = tmp_path / "trajectories"
    store = TrajectoryStore(root)
    tid = store.start(session_id="doomed", run_index=1, input_text="x")
    store.finish(tid, status="completed", output="done")
    garbage = root / "traj_deadbeefdeadbeefdeadbeef.jsonl"
    garbage.write_text("not json\n")

    removed = store.delete_for_session("doomed")

    assert removed == 1
    assert garbage.exists(), "an unprovable file was deleted on a guess"
    assert any("left in place" in p for p in store.problems)
