"""The goal domain: durable objective, CAS, round budget, disarmed restore.

Pinned properties (DeepSeek Harness's goal domain, adapted):
* every mutation is CAS by revision -- a stale writer is refused, told the
  current revision, and nothing changes;
* only goal-sourced continuation rounds consume the budget; exhausting it
  BLOCKS the goal with the stable code `round-cap-exhausted` instead of
  silently stopping;
* blocked carries a machine-routable kebab-case code plus prose;
* create/resume (the ARMING mutations) require explicit human authority --
  a cron-fired or delegated turn cannot authorize its own unattended
  continuation;
* restore folds the goal snapshot but comes back DISARMED.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.goals import GoalContinuation, fold_goal
from mini_loop.registry import ToolCall, ToolContext
from mini_loop.run_context import RunContext

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, **kwargs):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
        enable_features=True,
        **kwargs,
    )
    return manager.create().agent


def _call(agent, name, *, human=True, **input_):
    tool = agent.tools.get(name)
    assert tool is not None, f"{name} must be registered"
    ctx = ToolContext(
        agent=agent, workspace=agent.workspace, state=agent.state,
        call=ToolCall(name=name, input=dict(input_), id="t1"),
        run_context=RunContext.explicit_human() if human else RunContext.default(),
    )
    return asyncio.run(tool.run(ctx, **input_))


def test_create_status_complete_roundtrip(tmp_path):
    agent = _agent(tmp_path)
    out = _call(agent, "goal_create", objective="ship the feature", max_rounds=3)
    assert "active" in out and "(armed)" in out
    assert "rounds: 0/3" in _call(agent, "goal_status")
    done = _call(agent, "goal_complete", revision=1)
    assert "complete" in done
    assert agent.state["goal_armed"] is False


def test_mutations_are_compare_and_set(tmp_path):
    agent = _agent(tmp_path)
    _call(agent, "goal_create", objective="obj")
    out = _call(agent, "goal_complete", revision=99)
    assert out.startswith("Error") and "stale revision" in out
    assert agent.state["goal"]["phase"] == "active"  # nothing changed


def test_blocked_carries_a_stable_code(tmp_path):
    agent = _agent(tmp_path)
    _call(agent, "goal_create", objective="obj")
    refused = _call(agent, "goal_block", revision=1, code="Not Kebab", message="m")
    assert refused.startswith("Error")
    out = _call(agent, "goal_block", revision=1, code="needs-credentials",
                message="the deploy key is missing")
    assert "blocked [needs-credentials]" in out
    assert agent.state["goal_armed"] is False


def test_arming_mutations_require_explicit_human_authority(tmp_path):
    agent = _agent(tmp_path)
    refused = _call(agent, "goal_create", objective="obj", human=False)
    assert refused.startswith("Error") and "human" in refused
    _call(agent, "goal_create", objective="obj")
    _call(agent, "goal_block", revision=1, code="stuck-on-x", message="x")
    # A cron-fired (untrusted) turn must not re-arm continuation.
    resumed = _call(agent, "goal_resume", revision=2, human=False)
    assert resumed.startswith("Error")
    assert agent.state["goal_armed"] is False
    # A human can.
    assert "(armed)" in _call(agent, "goal_resume", revision=2)


def _stop(agent):
    return asyncio.run(GoalContinuation().on_stop(agent, agent.messages, ""))


def test_continuation_consumes_rounds_and_blocks_at_the_cap(tmp_path):
    agent = _agent(tmp_path)
    _call(agent, "goal_create", objective="obj", max_rounds=2)
    first = _stop(agent)
    assert first and "round 1/2" in first
    second = _stop(agent)
    assert second and "round 2/2" in second
    third = _stop(agent)
    assert third is None
    goal = agent.state["goal"]
    assert goal["phase"] == "blocked"
    assert goal["blocked"]["code"] == "round-cap-exhausted"
    assert agent.state["goal_armed"] is False


def test_a_disarmed_or_inactive_goal_does_not_continue(tmp_path):
    agent = _agent(tmp_path)
    _call(agent, "goal_create", objective="obj")
    agent.state["goal_armed"] = False
    assert _stop(agent) is None  # disarmed
    agent.state["goal_armed"] = True
    _call(agent, "goal_complete", revision=1)
    assert _stop(agent) is None  # complete


def test_the_fold_recovers_the_snapshot_and_honors_clear():
    events = [
        {"type": "goal_change", "operation": "create",
         "goal": {"id": "g1", "revision": 1, "phase": "active"}},
        {"type": "goal_change", "operation": "round",
         "goal": {"id": "g1", "revision": 2, "phase": "active"}},
    ]
    assert fold_goal(events)["revision"] == 2
    assert fold_goal(events + [{"type": "goal_change", "operation": "clear"}]) is None
    assert fold_goal([]) is None


def test_restore_folds_the_goal_but_comes_back_disarmed(tmp_path):
    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                       skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(
        settings, FakeAsyncAnthropic(), enable_features=True, state_store=store,
    )
    session = manager.create()
    asyncio.run(session.run("hello"))
    _call(session.agent, "goal_create", objective="survive the restart")
    asyncio.run(session.emit({
        "type": "goal_change", "operation": "create",
        "goal": dict(session.agent.state["goal"]),
    }))
    assert session.agent.state["goal_armed"] is True

    second = SessionManager(
        settings, FakeAsyncAnthropic(), enable_features=True, state_store=store,
    )
    [restored] = [s for s in second.restore_sessions() if s.id == session.id]
    goal = restored.agent.state.get("goal")
    assert goal is not None and goal["objective"] == "survive the restart"
    assert restored.agent.state.get("goal_armed") is False
    assert asyncio.run(GoalContinuation().on_stop(restored.agent, [], "")) is None
    store.close()
