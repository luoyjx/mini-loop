"""A timed-out command left its children running forever.

`bash_timeout` is the only resource limit this harness has, and it did not do
what it claims. `subprocess.run(timeout=...)` kills the *direct child* -- the
shell -- and anything that shell backgrounded survives. So:

    run_bash("for i in 1 2 3; do sh -c 'while :; do :; done' & done; sleep 30")
    -> 'Error: Timeout (3s)'
    -> 3 spin loops still burning CPU, indefinitely

The agent is told the command timed out and moves on. Nothing in the transcript,
the event stream or the audit says the host is now permanently loaded, and a few
repetitions make the machine unusable.

**This was not hypothetical for long.** A probe written earlier in the same round
left 44 orphaned spinners on the development machine; the load average reached
96 and the test suite went from 21s to 66s with three timing tests failing. The
defect demonstrated itself while being investigated, which is also a reminder
that a slow suite is sometimes the environment rather than the code.

The fix is a process group per command (`start_new_session=True`) and
`killpg(SIGKILL)` on timeout or cancellation, in both paths to the shell --
round 58's lesson that two routes to the same primitive drift apart.

`SIGKILL` rather than `SIGTERM`: the group is being killed because it ignored a
deadline, and a grace period for something already past one is how orphans
survive.
"""

import asyncio
import io
import pathlib
import subprocess
import time

import pytest

from mini_loop.background import BackgroundManager
from mini_loop.tools import Toolset

FOREGROUND_MARK = "MINILOOPFGORPHANTEST"
BACKGROUND_MARK = "MINILOOPBGORPHANTEST"


def _alive(mark: str) -> list[str]:
    found = subprocess.run(["pgrep", "-f", mark], capture_output=True, text=True)
    return [pid for pid in found.stdout.split() if pid]


def _reap(mark: str) -> None:
    survivors = _alive(mark)
    if survivors:
        subprocess.run(["kill", "-9", *survivors], capture_output=True)


@pytest.fixture
def workspace(tmp_path):
    directory = tmp_path / "ws"
    directory.mkdir()
    return directory


@pytest.fixture(autouse=True)
def no_leftovers():
    """A test that leaks spinners slows every test after it -- 44 of them took
    this suite from 21s to 66s."""
    yield
    for mark in (FOREGROUND_MARK, BACKGROUND_MARK):
        _reap(mark)


def _spawner(mark: str) -> str:
    return f"for i in 1 2 3; do sh -c 'x={mark}; while :; do :; done' & done; sleep 30"


# --- the foreground shell -------------------------------------------------

def test_a_timeout_reaps_the_whole_process_group(workspace):
    toolset = Toolset(workspace, bash_timeout=2)
    assert _alive(FOREGROUND_MARK) == [], "the environment was already dirty"

    result = toolset.run_bash(_spawner(FOREGROUND_MARK))
    assert "Timeout" in result

    time.sleep(1.0)
    assert _alive(FOREGROUND_MARK) == [], (
        "children survived the timeout and will burn CPU indefinitely"
    )


def test_foreground_command_requests_own_process_group(workspace, monkeypatch):
    """Pin group creation without relying on host process-inspection access."""

    captured = {}

    class FakeProcess:
        pid = 12345
        returncode = 0
        stdout = io.StringIO("")
        stderr = io.StringIO("")

        def poll(self):
            return 0

        def wait(self, timeout=None):
            return 0

    def fake_popen(*args, **kwargs):
        captured["args"] = args
        captured["kwargs"] = kwargs
        return FakeProcess()

    monkeypatch.setattr("mini_loop.tools.subprocess.Popen", fake_popen)
    result = Toolset(workspace).run_bash_result("echo ok")

    assert result.timed_out is False
    assert captured["kwargs"]["start_new_session"] is True


def test_an_ordinary_command_is_unaffected(workspace):
    """Reaping that breaks normal commands is not a fix."""
    toolset = Toolset(workspace)
    assert toolset.run_bash("echo hello").strip() == "hello"


def test_stderr_is_still_captured(workspace):
    """The rewrite swapped `capture_output` for explicit pipes; stderr has to
    keep arriving, since that is where the failure is."""
    toolset = Toolset(workspace)
    assert "No such file" in toolset.run_bash("ls /nonexistent-path-xyz")


def test_both_streams_arrive_in_order(workspace):
    toolset = Toolset(workspace)
    assert toolset.run_bash("echo out; echo err >&2").strip() == "out\nerr"


def test_a_command_that_finishes_is_not_killed(workspace):
    toolset = Toolset(workspace)
    assert "done" in toolset.run_bash("sleep 0.2; echo done")


# --- the background shell -------------------------------------------------

def test_a_background_timeout_reaps_the_group(workspace):
    """The same primitive by a different route -- round 58's lesson."""
    manager = BackgroundManager(workspace, default_timeout=2)

    async def scenario():
        manager.run(_spawner(BACKGROUND_MARK))
        for _ in range(60):
            await asyncio.sleep(0.1)
            if manager._completed:
                break
        return manager.drain()

    done = asyncio.run(scenario())
    assert done and "Timeout" in done[0]["result"]

    time.sleep(1.0)
    assert _alive(BACKGROUND_MARK) == [], (
        "a background command's children survived its timeout"
    )


def test_cancelling_a_background_task_reaps_the_group(workspace):
    """`close()` cancels in-flight tasks; the children must go too."""
    manager = BackgroundManager(workspace, default_timeout=60)

    async def scenario():
        manager.run(_spawner(BACKGROUND_MARK))
        await asyncio.sleep(0.6)
        await manager.close()

    asyncio.run(scenario())
    time.sleep(1.0)
    assert _alive(BACKGROUND_MARK) == []


def test_a_background_command_still_returns_output(workspace):
    manager = BackgroundManager(workspace, default_timeout=10)

    async def scenario():
        manager.run("echo background-hello")
        for _ in range(60):
            await asyncio.sleep(0.1)
            if manager._completed:
                break
        return manager.drain()

    done = asyncio.run(scenario())
    assert done and "background-hello" in done[0]["result"]


# --- both paths agree -----------------------------------------------------

def test_neither_path_leaks_children():
    """Stated as parity, because the two drifted apart once already."""
    import inspect

    from mini_loop import background, tools

    for module in (tools, background):
        source = inspect.getsource(module)
        assert "start_new_session=True" in source, module.__name__
        assert "_kill_group" in source, module.__name__
