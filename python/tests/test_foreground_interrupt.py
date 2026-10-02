"""Stopping a turn stops the shell the turn started.

OpenWorker's stop contract names both halves: "Stop interrupts the model
stream and the foreground shell" (OPENWORKER_RESEARCH.md 6.1). Ours covered
the model stream (round 88 repairs the transcript) and the *background*
sibling (round 94 reclaims on delete), but the foreground shell fell through
the seam between asyncio and threads: `dispatch` runs `run_bash` via
`to_thread`, and cancelling the await abandons the worker thread -- the
subprocess keeps burning until `bash_timeout` (120s by default). Measured:
one second after `session.cancel()`, the cancelled command was still alive.

`Toolset.interrupt()` ends every live foreground group, and `Agent.run`'s
cancellation path calls it beside `close_unanswered_tools()` -- the transcript
repair and the process reaping are the same event's two halves.
"""

import asyncio
import os
import pathlib
import subprocess

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.fake_llm import validate_transcript
from mini_loop.tools import Toolset

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
MARKER = f"41337.{os.getpid()}"


def _alive() -> bool:
    probe = subprocess.run(["pgrep", "-f", f"sleep {MARKER}"],
                           capture_output=True, text=True)
    return bool(probe.stdout.strip())


async def _wait(condition, *, timeout=5.0):
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        if condition():
            return True
        await asyncio.sleep(0.05)
    return condition()


@pytest.fixture(autouse=True)
def _no_stray_sleeps():
    yield
    subprocess.run(["pkill", "-f", f"sleep {MARKER}"], capture_output=True)


def _manager(tmp_path, command):
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("bash", command=command)], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return SessionManager(settings, client, tool_registry=full_registry())


@pytest.mark.asyncio
async def test_cancel_kills_the_foreground_shell(tmp_path):
    manager = _manager(tmp_path, f"sleep {MARKER}")
    session = manager.create()

    turn = asyncio.create_task(session.run("wait"))
    assert await _wait(_alive), "the probe shell never started"

    await session.cancel("user hit stop")
    with pytest.raises(asyncio.CancelledError):
        await turn

    assert await _wait(lambda: not _alive()), (
        "the foreground shell survived cancellation "
        "(reaped only at bash_timeout)"
    )


@pytest.mark.asyncio
async def test_cancel_kills_the_whole_group_not_just_the_shell(tmp_path):
    """The command's children die with it: ending only the wrapping shell
    would orphan whatever it spawned -- the round-94 grandchild problem."""

    manager = _manager(tmp_path, f"sleep {MARKER} & wait")
    session = manager.create()

    turn = asyncio.create_task(session.run("wait"))
    assert await _wait(_alive)

    await session.cancel("stop")
    with pytest.raises(asyncio.CancelledError):
        await turn

    assert await _wait(lambda: not _alive()), (
        "a backgrounded child survived the interrupt"
    )


@pytest.mark.asyncio
async def test_the_transcript_repair_still_happens_beside_the_kill(tmp_path):
    """Round 88's invariant is untouched: both halves of the same event."""

    manager = _manager(tmp_path, f"sleep {MARKER}")
    session = manager.create()

    turn = asyncio.create_task(session.run("wait"))
    assert await _wait(_alive)
    await session.cancel("stop")
    with pytest.raises(asyncio.CancelledError):
        await turn

    validate_transcript(session.agent.messages)


def test_interrupt_is_a_no_op_when_nothing_runs(tmp_path):
    """Not vacuous in the other direction: a completed command has left the
    live set, so a later cancel kills nothing that already finished."""

    toolset = Toolset(tmp_path / "ws")
    assert toolset.run_bash("echo done").strip() == "done"

    assert not toolset._live, "a finished process lingered in the live set"
    assert toolset.interrupt() == 0
