"""Census: the failure vocabulary the instruments count (§5, 2026-09-02).

Status quo probed on the recorded corpus: 1,176 bash results, 16 ending
with the "(exit N)" note the renderer appends to a failed command -- and
zero of them counted by `tool_errors`, in the benchmark or the miner,
because both recognized only the "Error"/"Unknown tool" prefixes. The
model saw those commands fail; the instruments reported the corpus as
error-free for bash and selected experiments blind to it. The miner's
own bash profile disagreed with its own totals in the same report
("tool_errors 0" beside "cd: 22 (1 errored)").

FINDING -> RESOLVED in the same change: one rule, `tools.is_failed_result`,
lives beside the renderer that produces the note, and both instruments
use it. Pinned edges: the three shapes count; a legitimately-quiet success
and a command whose output merely mentions an exit note mid-text do not;
an overflowed command carries no exit note by design (the overflow note
replaces it), so it stays uncounted -- a known limit, stated.
"""

import asyncio
import pathlib

from mini_loop import Settings
from mini_loop.benchmark import BenchTask, run_arm
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.mining import mine_trajectory
from mini_loop.tools import CommandResult, is_failed_result
from mini_loop.trajectory import TrajectoryStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _cmd(stdout="", exit_code=0, **fields):
    base = dict(stdout=stdout, stderr="", exit_code=exit_code, timed_out=False,
                overflowed=False, duration_ms=1)
    return CommandResult(**{**base, **fields})


def test_the_shared_rule_names_every_failure_shape():
    assert is_failed_result("Error: Path escapes workspace: /abs")
    assert is_failed_result("Unknown tool: task")
    assert is_failed_result("grep: no match\n(exit 1)")
    assert is_failed_result("(exit 2)"), "a failed command with no output"
    assert is_failed_result(_cmd(exit_code=3).render())

    assert not is_failed_result("(no output)")
    assert not is_failed_result("ok\n")
    assert not is_failed_result(None)
    assert not is_failed_result("the script prints (exit 1) and then keeps going\nfine"), (
        "the note is anchored at the end: output that merely mentions one is not a failure"
    )
    assert not is_failed_result(_cmd("fine").render())


def test_an_overflowed_failure_is_a_stated_blind_spot():
    """The overflow note replaces the exit note (background/foreground
    census), so a command that both overflowed and failed reports overflow
    only. The counter follows the renderer; this pin makes the limit visible
    rather than accidental."""
    rendered = _cmd("x" * 10, exit_code=1, overflowed=True, capture_limit=10).render()
    assert "(exit 1)" not in rendered
    assert not is_failed_result(rendered)


def _settings(tmp_path):
    return Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                    skills_dir=SKILLS, spill_dir=None)


def test_a_failed_command_counts_as_a_tool_error_in_both_instruments(tmp_path):
    task = BenchTask("probe", "look for a launch script",
                     lambda ws, final: "done" in final)
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("bash", _id="b1", command="ls launch-scripts-that-do-not-exist")],
         "tool_use"),
        ([tool("bash", _id="b2", command="echo fine")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    (row,) = asyncio.run(run_arm("x", _settings(tmp_path), client, (task,)))
    assert row["tool_calls"] == 2
    assert row["tool_errors"] == 1, (
        "the failed ls ended with an exit note the model saw; the bench must "
        "count it, and the clean echo must not"
    )

    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s1", run_index=1, input_text="probe")
    for name, output in (
        ("bash", "ls: cannot access 'x': No such file or directory\n(exit 2)"),
        ("bash", "fine"),
        ("read_file", "Error: Path escapes workspace: /abs"),
    ):
        store.append(tid, {"type": "tool_use", "name": name, "input": {}, "id": "c"})
        store.append(tid, {"type": "tool_result", "name": name, "output": output,
                           "id": "c"})
    store.finish(tid, status="completed", duration_ms=1.0)
    mined = mine_trajectory(store, tid)
    assert mined["tool_errors"] == 2
    assert mined["per_tool"]["bash"] == {"calls": 2, "errors": 1}, (
        "the miner's totals now agree with its own bash profile"
    )
