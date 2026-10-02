"""Deleting one session must reclaim what stopping the server reclaims.

The question came from outside: OpenWorker's own architecture review
(docs/OPENWORKER_RESEARCH.md, section 9.2.7) flags "background shell can
outlive session/server lifetime" as an open risk in *their* harness. Asked of
ours: `SessionManager.stop()` closed background managers and MCP clients, and
`SessionManager.delete()` -- the path behind `DELETE /sessions/{id}` -- closed
neither. Measured before the fix:

    after session.run(background_run "sleep ...")   process alive = True
    after manager.delete(session_id)                process alive = True
    after manager.stop()                            process alive = True

The third line is the compounding half: delete() pops the session from
`_sessions` first, so stop()'s sweep can no longer see the orphan's manager.
Background commands start with `start_new_session=True`, so such a process
survives the server process itself -- unreachable (`check_background` left
with the session), unkillable by shutdown, running in a workspace that was
rmtree'd out from under it.

The close path existed the whole time. Nothing on this route called it: a rule
only holds when something executes it.
"""

import asyncio
import os
import pathlib
import subprocess

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

#: Unique per test process, so parallel CI runs cannot see each other's sleeps.
MARKER = f"31337.{os.getpid()}"


def _manager(tmp_path, client=None):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 memory_root=tmp_path / "mem", skills_dir=SKILLS),
        client or FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )


def _background_client():
    return FakeAsyncAnthropic(responder=scripted([
        ([tool("background_run", command=f"sleep {MARKER}")], "tool_use"),
        ([text("started")], "end_turn"),
    ]))


def _process_alive() -> bool:
    probe = subprocess.run(["pgrep", "-f", f"sleep {MARKER}"],
                           capture_output=True, text=True)
    return bool(probe.stdout.strip())


async def _wait_for(condition, *, timeout=5.0):
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        if condition():
            return True
        await asyncio.sleep(0.05)
    return condition()


async def _drain_cleanup(manager):
    while manager._cleanup_tasks:
        await asyncio.gather(*tuple(manager._cleanup_tasks),
                             return_exceptions=True)


@pytest.fixture(autouse=True)
def _no_stray_sleeps():
    yield
    subprocess.run(["pkill", "-f", f"sleep {MARKER}"], capture_output=True)


class StubMCPClient:
    def __init__(self, name="stub"):
        self.name = name
        self.closed = False

    async def close(self):
        self.closed = True


# -- the leak --------------------------------------------------------------


@pytest.mark.asyncio
async def test_delete_kills_the_sessions_background_process(tmp_path):
    manager = _manager(tmp_path, _background_client())
    session = manager.create()
    await session.run("run the build in the background")
    assert await _wait_for(_process_alive), (
        "the probe never started; every assertion below would be vacuous"
    )

    manager.delete(session.id)
    await _drain_cleanup(manager)

    assert await _wait_for(lambda: not _process_alive()), (
        "the background process survived session deletion"
    )


@pytest.mark.asyncio
async def test_delete_stops_the_sessions_scheduled_cron_work(tmp_path):
    """A cron job is a per-session resource, and delete reclaimed every other one.

    Left scheduled, it outlives its session: on the next tick `_fire` calls
    `restore_scheduled_session`, which rebuilds the deleted session -- re-creating
    the workspace delete removed and rehydrating its transcript -- so the delete
    silently did not stop the one kind of work that fires unattended.
    """
    from datetime import datetime

    manager = _manager(tmp_path)
    session = manager.create()
    sid, workspace = session.id, session.workspace
    manager.cron.schedule(sid, "* * * * *", "scheduled work")
    assert workspace.exists()

    manager.delete(sid)
    assert [j for j in manager.cron.jobs.values() if j.session_id == sid] == [], (
        "the deleted session's cron job was left scheduled"
    )

    # A tick at a minute the job would have matched must not bring it back.
    manager.cron._tick_once(datetime(2026, 8, 3, 3, 0))
    assert sid not in manager._sessions, "a cron tick resurrected the deleted session"
    assert not workspace.exists(), "restore re-created the deleted session's workspace"


@pytest.mark.asyncio
async def test_stop_after_delete_still_reaps_the_orphan(tmp_path):
    """delete() pops the session before stop() can sweep it; the scheduled
    close is tracked in `_cleanup_tasks`, which stop() awaits."""

    manager = _manager(tmp_path, _background_client())
    session = manager.create()
    await session.run("run the build in the background")
    assert await _wait_for(_process_alive)

    manager.delete(session.id)
    await manager.stop()

    assert await _wait_for(lambda: not _process_alive())


@pytest.mark.asyncio
async def test_the_workspace_outlives_the_shell(tmp_path):
    """Removal is ordered after the close: rmtree on a directory that a live
    process has as its cwd is a race, not a cleanup."""

    manager = _manager(tmp_path, _background_client())
    session = manager.create()
    await session.run("run the build in the background")
    assert await _wait_for(_process_alive)
    workspace = pathlib.Path(session.workspace)

    manager.delete(session.id)
    # Synchronously after delete(): the close is scheduled, not yet run, so
    # the workspace must still be there for the still-live shell.
    assert workspace.exists()

    await _drain_cleanup(manager)
    assert not _process_alive()
    assert not workspace.exists()


@pytest.mark.asyncio
async def test_delete_closes_the_sessions_mcp_clients(tmp_path):
    manager = _manager(tmp_path)
    session = manager.create()
    await session.run("hello")
    stub = StubMCPClient()
    session.agent.state.setdefault("mcp_clients", {})["stub"] = stub

    manager.delete(session.id)
    await _drain_cleanup(manager)

    assert stub.closed, "the MCP child connection leaked with the session"


@pytest.mark.asyncio
async def test_a_shared_mcp_client_survives_a_single_delete(tmp_path):
    """One client object may serve several sessions; it closes with the last
    holder, the same rule as a workspace shared by teammates."""

    manager = _manager(tmp_path)
    first, second = manager.create(), manager.create()
    await first.run("hello")
    await second.run("hello")
    shared = StubMCPClient()
    first.agent.state.setdefault("mcp_clients", {})["shared"] = shared
    second.agent.state.setdefault("mcp_clients", {})["shared"] = shared

    manager.delete(first.id)
    await _drain_cleanup(manager)
    assert not shared.closed, "closed while another session still held it"

    manager.delete(second.id)
    await _drain_cleanup(manager)
    assert shared.closed


@pytest.mark.asyncio
async def test_delete_with_no_services_still_removes_the_workspace(tmp_path):
    """The ordinary path is unchanged: no services, immediate removal."""

    manager = _manager(tmp_path)
    session = manager.create()
    await session.run("hello")
    workspace = pathlib.Path(session.workspace)
    session.agent.state.pop("background", None)

    manager.delete(session.id)

    assert not workspace.exists()
