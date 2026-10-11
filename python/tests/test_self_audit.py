"""The runtime reads its own ledgers (self-evolution, observation half).

Every subsystem keeps a deduplicating bounded ProblemLog, every turn
records a trajectory summary, info() names live activity -- and nothing
ever read any of it. `build_report` folds those surfaces into one bounded,
read-only report; the `self_audit` tool serves it to a session so a
scheduled prompt can act on it.

Pinned here:

* the report names problems from manager-level ledgers with their counts;
* session activity distribution and trajectory outcomes appear;
* a broken source becomes a line in the report, never a missing report;
* the report is hard-capped, because a self-audit that grows with runtime
  age would be the bounded-work defect reporting on itself;
* the tool is registered read-only and answers inside a managed session.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.self_audit import MAX_REPORT_CHARS, build_report, install_self_audit

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, **kw):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None, **kw),
        FakeAsyncAnthropic(),
    )


def test_problems_and_activity_reach_the_report(tmp_path):
    manager = _manager(tmp_path)
    manager.create()
    manager.cron.problems.append("job_x: firing failed (RuntimeError); the occurrence was lost")
    manager.cron.problems.append("job_x: firing failed (RuntimeError); the occurrence was lost")

    report = build_report(manager)

    assert "# self-audit" in report
    assert "cron: 2 reported" in report
    assert "firing failed" in report
    assert "sessions (1)" in report
    assert "1 idle" in report


def test_trajectory_outcomes_appear(tmp_path):
    async def main():
        manager = _manager(tmp_path, trajectory_enabled=True,
                           trajectory_root=tmp_path / "trajectories")
        session = manager.create()
        await session.run("record me")
        return manager

    manager = asyncio.run(main())
    report = build_report(manager)
    assert "1 completed" in report
    assert "most recent recordings" in report


def test_a_broken_source_is_a_line_not_a_missing_report(tmp_path):
    manager = _manager(tmp_path)

    class _Exploding:
        @property
        def problems(self):
            raise RuntimeError("boom")

    manager.cron = _Exploding()

    report = build_report(manager)
    assert "# self-audit" in report
    assert "unreadable" in report
    assert "problems" in report


def test_the_report_is_hard_capped(tmp_path):
    manager = _manager(tmp_path)
    for i in range(400):
        manager.cron.problems.append(f"distinct problem number {i}: " + "x" * 200)

    report = build_report(manager)
    assert len(report) <= MAX_REPORT_CHARS + 40
    assert "truncated at the cap" in report


def test_the_tool_serves_the_report_inside_a_session(tmp_path):
    from mini_loop.registry import ToolRegistry

    manager = _manager(tmp_path)
    session = manager.create()
    registry = ToolRegistry()
    install_self_audit(registry)
    tool = registry.get("self_audit")
    assert tool is not None and tool.readonly

    class _Ctx:
        state = session.agent.state

    result = asyncio.run(tool.handler(_Ctx()))
    assert "# self-audit" in result

    class _Orphan:
        state: dict = {}

    assert "Error" in asyncio.run(tool.handler(_Orphan()))


def test_skill_usage_correlates_loads_with_outcomes(tmp_path):
    """The feedback half of skill evolution: loads per skill, with how the
    loading turns ended. Correlation surfaced as a lead, never a verdict."""

    from mini_loop.trajectory import TrajectoryStore

    manager = _manager(tmp_path)
    store = TrajectoryStore(tmp_path / "trajectories")
    manager.trajectories = store

    good = store.start(session_id="s1", run_index=1, input_text="x")
    store.append(good, {"type": "tool_use", "name": "load_skill",
                        "input": {"name": "deploy"}})
    store.finish(good, status="completed", output="done")

    bad = store.start(session_id="s1", run_index=2, input_text="y")
    store.append(bad, {"type": "tool_use", "name": "load_skill",
                       "input": {"name": "deploy"}})
    store.append(bad, {"type": "tool_use", "name": "load_skill",
                       "input": {"name": "lint"}})
    store.finish(bad, status="error", error="boom")

    report = build_report(manager)
    assert "deploy: 2 load(s), 1 in turns that ended error/interrupted" in report
    assert "lint: 1 load(s), 1 in turns" in report
    assert "correlation, not causation" in report
