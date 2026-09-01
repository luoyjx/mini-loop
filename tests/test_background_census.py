"""Background-result census: the foreground's lessons, re-learned here.

Round 62 taught silent truncation, round 140 taught bounded capture,
micro-experiment E taught exit-code visibility -- all fixed on the
foreground path. The background path ran the same shell with none of the
render rules: a bare [:OUTPUT_CAP] slice (silent, head-keeping, though
for command output the tail is the part worth running the command for),
a vanished exit code (a failed long build injected as a clean
"completed"), and communicate() reading unbounded output into memory.

The first two are fixed and pinned here (docs/RSI_RESEARCH_AND_PLAN.md
§5). The third stays a named FINDING:

* FINDING: _exec reads the whole stdout via communicate() -- a
  background `yes` fills memory until the timeout. The foreground's
  _BoundedCapture (round 140) has no async sibling yet; building one is
  a deliberate follow-up, not a drive-by.
"""

import asyncio
import inspect

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


def test_unbounded_capture_is_a_named_finding():
    """FINDING tripwire: _exec still gathers output via communicate(),
    which holds the whole stream in memory (the round-140 hazard, async
    edition). When a bounded async reader lands, this pin flips together
    with the §5 record."""

    source = inspect.getsource(BackgroundManager._exec)
    assert "communicate()" in source, (
        "if communicate() is gone, the bounded-capture follow-up landed "
        "-- flip this pin and the §5 finding together"
    )
