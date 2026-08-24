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


def test_mismatched_task_sets_are_incomparable():
    import pytest

    with pytest.raises(ValueError, match="different task sets"):
        compare([{"task": "a", "passed": True}], [{"task": "z", "passed": True}])
