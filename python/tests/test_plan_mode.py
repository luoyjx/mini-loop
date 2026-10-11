"""Plan mode: soft guidance, log-only state, and a catalog that holds still.

The three DeepSeek Harness properties pinned here:

* **stable catalog** -- `exit_plan_mode` is registered while plan mode is
  OFF and fails there; entering/leaving plan mode changes the prompt, never
  the tool list. A catalog flip would invalidate the cached prompt prefix
  on every mode change and advertise the mode to the provider.
* **log-only state** -- the value in force is the last logged `plan_mode`
  event; restore folds it from the event log rather than trusting a mirror.
* **soft guidance** -- the section rides the system prompt; sandbox and
  permission policy are untouched by construction (neither module imports
  plan state).

Keep-planning is a FAILED `exit_plan_mode` call carrying reviewer feedback.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.plan_mode import PLAN_SECTION, fold_plan_mode, install_plan_mode
from mini_loop.registry import ToolRegistry

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _session(tmp_path, **kwargs):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
        enable_features=True,
        **kwargs,
    )
    return manager.create()


def _dispatch(agent, name, **input_):
    from mini_loop.registry import ToolCall, ToolContext

    tool = agent.tools.get(name)
    assert tool is not None, f"{name} must be registered"
    ctx = ToolContext(
        agent=agent, workspace=agent.workspace, state=agent.state,
        call=ToolCall(name=name, input=dict(input_), id="t1"),
    )
    return asyncio.run(tool.run(ctx, **input_))


def test_both_tools_are_always_registered(tmp_path):
    agent = _session(tmp_path).agent
    assert agent.tools.get("enter_plan_mode") is not None
    assert agent.tools.get("exit_plan_mode") is not None


def test_exit_outside_plan_mode_fails_without_changing_anything(tmp_path):
    agent = _session(tmp_path).agent
    before = agent.tools.snapshot().sent_names
    out = _dispatch(agent, "exit_plan_mode", plan="# My plan")
    assert out.startswith("Error")
    assert agent.tools.snapshot().sent_names == before


def test_entering_adds_the_section_and_keeps_the_catalog(tmp_path):
    agent = _session(tmp_path).agent
    catalog_before = agent.tools.snapshot().fingerprint
    assert PLAN_SECTION not in agent.system
    _dispatch(agent, "enter_plan_mode")
    assert PLAN_SECTION in agent.system
    assert agent.tools.snapshot().fingerprint == catalog_before


def test_a_plan_without_a_heading_is_refused(tmp_path):
    agent = _session(tmp_path).agent
    _dispatch(agent, "enter_plan_mode")
    out = _dispatch(agent, "exit_plan_mode", plan="just some prose")
    assert out.startswith("Error") and "#" in out
    assert agent.state.get("plan_mode") is True  # still planning


def test_an_approved_exit_leaves_plan_mode(tmp_path):
    agent = _session(tmp_path).agent
    _dispatch(agent, "enter_plan_mode")
    out = _dispatch(agent, "exit_plan_mode", plan="# The plan\n1. do it")
    assert "approved" in out.lower()
    assert agent.state.get("plan_mode") is False
    assert PLAN_SECTION not in agent.system


def test_keep_planning_is_a_failed_call_with_feedback(tmp_path):
    async def reviewer(ctx, plan):
        return False, "split step 2 into smaller pieces"

    registry = ToolRegistry()
    install_plan_mode(registry, approval=reviewer)
    agent = _session(tmp_path).agent
    agent.tools = registry
    agent.state["plan_mode"] = True
    out = _dispatch(agent, "exit_plan_mode", plan="# The plan")
    assert out.startswith("Error") and "split step 2" in out
    assert agent.state.get("plan_mode") is True  # still planning


def test_the_fold_recovers_the_last_logged_value():
    events = [
        {"type": "plan_mode", "active": True},
        {"type": "other"},
        {"type": "plan_mode", "active": False},
        {"type": "plan_mode", "active": True},
    ]
    assert fold_plan_mode(events) is True
    assert fold_plan_mode(events[:3]) is False
    assert fold_plan_mode([]) is False


def test_restore_folds_plan_mode_from_the_log(tmp_path):
    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
        enable_features=True,
        state_store=store,
    )
    session = manager.create()
    agent = session.agent
    asyncio.run(session.run("hello"))  # give the log a transcript
    _dispatch(agent, "enter_plan_mode")
    # The flip only lands durably via the event stream; emit through the
    # session so it is captured like the real path.
    asyncio.run(session.emit({"type": "plan_mode", "active": True}))

    # A separate process: a new manager over the same store. The live handle
    # would answer from memory; only a genuine restore exercises the fold.
    second = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
        enable_features=True,
        state_store=store,
    )
    [restored] = [
        s for s in second.restore_sessions() if s.id == session.id
    ]
    assert restored.agent.state.get("plan_mode") is True
    store.close()


def test_plan_mode_does_not_touch_sandbox_or_permissions():
    """Soft guidance by construction: neither enforcement module reads it."""

    import pathlib

    root = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"
    for module in ("sandbox.py", "permissions.py"):
        source = (root / module).read_text()
        assert "plan_mode" not in source, (
            f"{module} must not read plan state; plan mode is guidance, "
            "enforcement is composed separately"
        )
