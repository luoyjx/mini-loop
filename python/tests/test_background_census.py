"""Background-result census: the foreground's lessons, re-learned here.

Round 62 taught silent truncation, round 140 taught bounded capture,
micro-experiment E taught exit-code visibility -- all fixed on the
foreground path. The background path ran the same shell with none of the
render rules: a bare [:OUTPUT_CAP] slice (silent, head-keeping, though
for command output the tail is the part worth running the command for),
a vanished exit code (a failed long build injected as a clean
"completed"), and communicate() reading unbounded output into memory.

All three are fixed and pinned here (docs/RSI_RESEARCH_AND_PLAN.md §5):
the render follows the foreground rules, and _bounded_read is the async
sibling of round 140's _BoundedCapture -- peak memory tracks the capture
limit, an overflow stops the capture and ends the command, flagged.
"""

import asyncio
import inspect

import mini_loop.background as background
from mini_loop.background import BackgroundManager
from mini_loop.secrets import SecretRegistry
from mini_loop.tools import OUTPUT_CAP


def _run(tmp_path, command, wait=10.0):
    workspace = tmp_path / "ws"
    workspace.mkdir(exist_ok=True)
    manager = BackgroundManager(
        workspace, secrets=SecretRegistry.from_environ(), sandbox=None)

    async def go():
        manager.run(command)
        for _ in range(int(wait * 20)):
            await asyncio.sleep(0.05)
            if manager._completed:
                break
        return manager._completed[-1]

    return asyncio.run(go())


def test_a_failed_background_command_names_its_exit_code(tmp_path):
    """A background `exit 3` used to complete as a clean "(no output)"
    -- the model could not see the failure. The render now follows the
    foreground rules; `status` stays lifecycle (ran-to-end), so failure
    visibility lives in the text, same as CommandResult.render()."""

    quiet_fail = _run(tmp_path, "exit 3")
    assert quiet_fail["result"] == "(exit 3)"
    assert quiet_fail["status"] == "completed"

    with_output = _run(tmp_path, "echo partial; exit 5")
    assert with_output["result"] == "partial\n(exit 5)"

    clean = _run(tmp_path, "echo ok")
    assert clean["result"] == "ok", "a clean success stays unannotated"


def test_a_long_background_output_keeps_the_tail_and_says_it_was_cut(tmp_path):
    """The bare [:OUTPUT_CAP] slice kept the head and said nothing; for
    command output the tail carries the summary, so the cut threw away
    exactly the part worth running the command for."""

    row = _run(tmp_path,
               "for i in $(seq 1 40000); do echo LINE; done; echo THE_ANSWER")
    assert row["status"] == "completed"
    assert len(row["result"]) <= OUTPUT_CAP
    assert "THE_ANSWER" in row["result"], "the tail must survive the cap"
    assert "truncated" in row["result"] or "omitted" in row["result"], (
        "a cut without a notice is the round-62 defect again"
    )


def test_background_capture_is_memory_bounded_not_just_capped(
        tmp_path, monkeypatch):
    """The round-140 rule, async edition: `communicate()` held the whole
    stream in memory, so a high-output background command filled the
    process until its timeout. The capture is now chunk-bounded -- the
    overflow stops it, ends the command, and is flagged to the model."""

    import shlex
    import sys

    monkeypatch.setattr(background, "MAX_BASH_CAPTURE", 64 * 1024)
    big = 64 * 1024 * 16
    script = f"import sys; sys.stdout.write('a' * {big})"
    row = _run(tmp_path, f"{shlex.quote(sys.executable)} -c {shlex.quote(script)}")

    assert row["status"] == "completed"
    assert "output exceeded 65,536 bytes" in row["result"]
    # The tripwire from the census round, flipped: communicate() is gone.
    assert "communicate()" not in inspect.getsource(BackgroundManager._exec)
    assert "_bounded_read" in inspect.getsource(BackgroundManager._exec)


def test_an_exactly_full_capture_is_not_an_overflow():
    """The boundary: output that fills the limit exactly ends at EOF
    with no overflow -- killing a command for fitting would be absurd."""

    class _Stream:
        def __init__(self, payload):
            self._pieces = [payload, b""]

        async def read(self, _n):
            return self._pieces.pop(0)

    async def go():
        return await BackgroundManager._bounded_read(
            _Stream(b"x" * 100), 100, lambda: (_ for _ in ()).throw(
                AssertionError("overflow fired on an exact fit")))

    data, overflowed = asyncio.run(go())
    assert data == b"x" * 100 and overflowed is False
