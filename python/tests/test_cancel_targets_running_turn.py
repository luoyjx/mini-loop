"""Cancel stops the turn that is running, not the one queued behind it.

A session serializes its turns on `self.lock` (round 87). `_running` -- the
task `cancel()` targets and `busy` reports -- was assigned at the top of
`run()`, *before* the lock. So when a second caller entered `run()` while a
turn held the lock, it overwrote `_running` with its own (still-queued) task.
The consumer that made this dangerous is cron: `_fire` does
`create_task(session.run(...))` with no busy check, straight into a session
that may be mid-turn.

Measured before the fix:

    turn A running (holds lock), cron fire B queued on the lock
    session.cancel()  ->  returns True
                          taskB (QUEUED) cancelled
                          taskA (RUNNING) keeps going, uncancelled
                          busy flips False while A still runs

The operator hit stop, was told it worked, and the runaway turn continued.
The fix sets `_running` inside the lock, so it names the task that actually
holds it; `cancel()` targets the running turn and the queued one proceeds
after it, which is what "cancel the current turn" has always meant.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.registry import Tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _blocking_manager(tmp_path, gate, entered):
    async def blocker(ctx):
        entered.set()
        await gate.wait()
        return "unblocked"

    registry = full_registry()
    registry.register(Tool("blocker", "hold the turn open",
                           {"type": "object", "properties": {}}, blocker,
                           risk="read"))

    def responder(kwargs):
        # Stateful by transcript content, not a re-created `scripted` (which
        # would replay the blocker turn every call and trip the stuck detector).
        transcript = str(kwargs["messages"])
        if "SECOND" in transcript:
            return [text("second done")], "end_turn"
        if "unblocked" in transcript:          # blocker has returned
            return [text("first done")], "end_turn"
        return [tool("blocker", _id="b1")], "tool_use"

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return SessionManager(settings, FakeAsyncAnthropic(responder=responder),
                          tool_registry=registry)


@pytest.mark.asyncio
async def test_cancel_hits_the_running_turn_not_the_queued_one(tmp_path):
    gate, entered = asyncio.Event(), asyncio.Event()
    session = _blocking_manager(tmp_path, gate, entered).create()

    running = asyncio.create_task(session.run("first request"))
    await asyncio.wait_for(entered.wait(), timeout=3)
    # A second caller (as cron does) queues on the lock, mid-turn.
    queued = asyncio.create_task(session.run("[cron] SECOND request"))
    await asyncio.sleep(0.05)

    assert session._running is running, "_running names the queued task, not the runner"
    assert session.busy

    assert await session.cancel("stop") is True

    with pytest.raises(asyncio.CancelledError):
        await running
    # The queued turn was never the cancel target; it runs once the lock frees.
    gate.set()
    assert await asyncio.wait_for(queued, timeout=3) == "second done"


@pytest.mark.asyncio
async def test_manager_stop_closes_admission_before_cancelling_the_holder(tmp_path):
    gate, entered = asyncio.Event(), asyncio.Event()
    manager = _blocking_manager(tmp_path, gate, entered)
    session = manager.create()

    running = asyncio.create_task(session.run("first request"))
    await asyncio.wait_for(entered.wait(), timeout=3)
    queued = asyncio.create_task(session.run("SECOND"))
    await asyncio.sleep(0.05)

    await manager.stop()

    with pytest.raises(asyncio.CancelledError):
        await running
    with pytest.raises(RuntimeError, match="session manager stopped"):
        await queued
    assert not session.busy


@pytest.mark.asyncio
async def test_busy_stays_true_until_the_running_turn_ends(tmp_path):
    """The queued task's lifecycle must not flip `busy` off under the runner --
    an HTTP caller seeing False mid-turn would start a third, racing turn."""

    gate, entered = asyncio.Event(), asyncio.Event()
    session = _blocking_manager(tmp_path, gate, entered).create()

    running = asyncio.create_task(session.run("first"))
    await asyncio.wait_for(entered.wait(), timeout=3)
    queued = asyncio.create_task(session.run("SECOND"))
    await asyncio.sleep(0.05)

    # Cancel only the queued task directly (not via the session): busy must
    # still be True, because the running turn holds the lock.
    queued.cancel()
    with pytest.raises(asyncio.CancelledError):
        await queued
    assert session.busy, "busy went False while the first turn was still running"

    gate.set()
    assert await asyncio.wait_for(running, timeout=3) == "first done"
    assert not session.busy


@pytest.mark.asyncio
async def test_a_cron_fire_into_a_busy_session_runs_after_the_turn(tmp_path):
    """End to end through the scheduler: a fire mid-turn queues and then runs,
    and it never disturbs the turn already in progress."""

    from mini_loop.cron import CronScheduler, CronJob

    gate, entered = asyncio.Event(), asyncio.Event()
    manager = _blocking_manager(tmp_path, gate, entered)
    session = manager.create()

    running = asyncio.create_task(session.run("first request"))
    await asyncio.wait_for(entered.wait(), timeout=3)

    scheduler = CronScheduler(manager)
    scheduler._fire(CronJob(id="j1", cron="* * * * *", prompt="SECOND",
                            session_id=session.id))
    await asyncio.sleep(0.05)
    # The running turn is untouched; the fire is queued on the lock.
    assert session._running is running

    gate.set()
    assert await asyncio.wait_for(running, timeout=3) == "first done"
    # Drain the scheduler's queued fire.
    for task in list(scheduler._running):
        await asyncio.wait_for(task, timeout=3)
    assert session.run_count == 2
