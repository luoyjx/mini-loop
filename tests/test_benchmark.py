"""The paired benchmark judges effects and falls conservative (Phase 0).

Pinned here:

* a task passes on observable workspace/final effects, never self-report;
* an arm that crashes on a task scores a loud failure, not a skip;
* the verdict is conservative: any regression sinks the candidate and
  wins do not buy it back; mismatched task sets are incomparable, loudly.
"""

import asyncio
import pathlib

from mini_loop import Settings
from mini_loop.benchmark import BenchTask, DEFAULT_TASKS, compare, run_arm
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, name):
    return Settings(fake_llm=True, workspace_root=tmp_path / name,
                    skills_dir=SKILLS, spill_dir=None)


def _client_passing_write_file():
    # One scripted conversation per task, in DEFAULT_TASKS order: writes the
    # greeting, answers 12, then skips the config edit (a deliberate miss).
    return FakeAsyncAnthropic(responder=scripted([
        ([tool("write_file", _id="t1", path="greeting.txt", content="hello")],
         "tool_use"),
        ([text("done")], "end_turn"),
        ([text("The answer is 12.")], "end_turn"),
        ([text("skipping the config task")], "end_turn"),
    ]))


def test_tasks_are_judged_on_effects(tmp_path):
    results = asyncio.run(run_arm(
        "baseline", _settings(tmp_path, "a"), _client_passing_write_file(),
        DEFAULT_TASKS,
    ))
    by_task = {r["task"]: r["passed"] for r in results}
    assert by_task == {"write-file": True, "arithmetic": True,
                       "edit-config": False}


def test_a_crashing_expectation_is_a_loud_failure(tmp_path):
    def _boom(workspace, final):
        raise RuntimeError("bad predicate")

    tasks = (BenchTask("boom", "say hi", _boom),)
    client = FakeAsyncAnthropic(responder=scripted([([text("hi")], "end_turn")]))
    (result,) = asyncio.run(run_arm("x", _settings(tmp_path, "b"), client, tasks))
    assert result["passed"] is False
    assert "expect raised" in result["error"]


def test_the_verdict_is_conservative():
    base = [{"task": "a", "passed": True}, {"task": "b", "passed": False}]
    cand_regressed = [{"task": "a", "passed": False}, {"task": "b", "passed": True}]
    verdict = compare(base, cand_regressed)
    assert verdict["verdict"] == "regression"
    assert verdict["regressions"] == ["a"] and verdict["wins"] == ["b"]

    cand_tied = [{"task": "a", "passed": True}, {"task": "b", "passed": False}]
    assert compare(base, cand_tied)["verdict"] == "not_worse"

    cand_better = [{"task": "a", "passed": True}, {"task": "b", "passed": True}]
    assert compare(base, cand_better)["verdict"] == "improvement"


def test_arms_record_the_context_cost(tmp_path):
    results = asyncio.run(run_arm(
        "baseline", _settings(tmp_path, "c"), _client_passing_write_file(),
        DEFAULT_TASKS,
    ))
    assert all(r["context_tokens_estimate"] > 0 for r in results), (
        "an arm that ran a conversation burned context; zero means the "
        "cost dimension is not being measured"
    )


def test_arms_record_behavioral_dimensions(tmp_path):
    """The instrument measures wasted motion from the transcript itself:
    provider rounds, tool calls, reads of an already-read path, tool calls
    that came back as errors. Without these, a tool-ergonomics experiment
    (reword a truncation notice, change a default) has nothing sensitive
    enough to register its effect."""

    tasks = (BenchTask("probe", "read notes.txt twice, then say done",
                       lambda ws, final: "done" in final),)
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("read_file", _id="r1", path="notes.txt")], "tool_use"),
        ([tool("read_file", _id="r2", path="notes.txt")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    (row,) = asyncio.run(run_arm("x", _settings(tmp_path, "d"), client, tasks))
    assert row["rounds"] == 3
    assert row["tool_calls"] == 2
    assert row["repeated_reads"] == 1, (
        "the second read of one path is wasted motion and must be counted"
    )
    assert row["tool_errors"] == 2  # notes.txt never exists: both reads error


def test_wasted_motion_enters_the_report_not_the_verdict():
    """A candidate that churns (more rounds, repeated reads, tool errors)
    but still passes stays not_worse -- with the churn named. Same doctrine
    as cost: dimensions inform the human, they never enter the verdict."""

    base = [{"task": "a", "passed": True, "rounds": 2,
             "repeated_reads": 0, "tool_errors": 0}]
    churn = [{"task": "a", "passed": True, "rounds": 4,
              "repeated_reads": 3, "tool_errors": 2}]
    verdict = compare(base, churn)
    assert verdict["verdict"] == "not_worse"
    assert verdict["dimensions"]["rounds"]["delta_pct"] == 100.0
    assert any("rounds" in w for w in verdict["dimension_warnings"])
    # A zero baseline has no percentage; the numbers still reach the report.
    assert verdict["dimensions"]["repeated_reads"] == {
        "baseline": 0, "candidate": 3, "delta_pct": None,
    }
    assert verdict["dimensions"]["tool_errors"]["candidate"] == 2


def test_dimensions_inform_and_never_judge():
    """A candidate that passes everything but costs 3x reports not_worse
    WITH a named warning: cost enters the report, never the verdict --
    folding it in would make the utility formula itself a gaming surface
    (SICA practice #8, adapted to the fall-toward-the-human rule)."""

    base = [{"task": "a", "passed": True, "duration_ms": 100,
             "context_tokens_estimate": 1000}]
    slow = [{"task": "a", "passed": True, "duration_ms": 300,
             "context_tokens_estimate": 4000}]
    verdict = compare(base, slow)
    assert verdict["verdict"] == "not_worse"
    assert verdict["dimensions"]["duration_ms"]["delta_pct"] == 200.0
    assert verdict["dimensions"]["context_tokens_estimate"]["candidate"] == 4000
    assert any("duration_ms" in w for w in verdict["dimension_warnings"])
    assert any("context_tokens_estimate" in w
               for w in verdict["dimension_warnings"])

    # Cheaper-and-equal carries the numbers and no warning.
    cheap = [{"task": "a", "passed": True, "duration_ms": 50,
              "context_tokens_estimate": 500}]
    improved = compare(base, cheap)
    assert improved["dimension_warnings"] == []
    assert improved["dimensions"]["duration_ms"]["delta_pct"] == -50.0


def test_rows_without_dimensions_stay_comparable():
    base = [{"task": "a", "passed": True}]
    cand = [{"task": "a", "passed": True}]
    verdict = compare(base, cand)
    assert verdict["verdict"] == "not_worse"
    assert verdict["dimensions"] == {}
    assert verdict["dimension_warnings"] == []


def test_heldout_tasks_are_disjoint_from_the_visible_set():
    """The whole point of a held-out set: no overlap with the tasks the
    optimization loop sees, so overfit shows instead of hiding."""

    from mini_loop.benchmark import HELDOUT_TASKS

    visible = {t.name for t in DEFAULT_TASKS}
    heldout = {t.name for t in HELDOUT_TASKS}
    assert heldout, "the held-out set is empty"
    assert visible.isdisjoint(heldout), (
        f"held-out tasks overlap the visible set: {visible & heldout}"
    )


def test_mismatched_task_sets_are_incomparable():
    import pytest

    with pytest.raises(ValueError, match="different task sets"):
        compare([{"task": "a", "passed": True}], [{"task": "z", "passed": True}])
