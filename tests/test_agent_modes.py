"""The interaction axis: agent / plan / ask, orthogonal to permission.

The mode says what the session is FOR; the permission says what runs
without asking. The two postures behind the axis make opposite enforcement
choices, deliberately:

* `plan` is the MODEL's collaboration state -- soft guidance, pinned by
  test_plan_mode.py, untouched here.
* `ask` is the HUMAN's Q&A posture. The model cannot leave it, so the
  permission hook refuses mutating-risk calls outright while it is active
  -- in every permission mode, `auto` included: full access governs what
  is asked about, ask mode cuts what is allowed.

Ask mode follows plan mode's log-only pattern, so a restored ask session
STAYS ask -- unlike permission_mode, which restores to `interactive`,
because "comes back asking" is fail-safe and "comes back mutating" is not.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.ask_mode import ASK_SECTION, fold_ask_mode
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, calls, **kwargs):
    client = FakeAsyncAnthropic(responder=scripted(
        [(blocks, "tool_use") for blocks in calls] + [([text("done")], "end_turn")]
    ))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    return SessionManager(settings, client, tool_registry=full_registry(),
                          **kwargs)


def _tool_results(session):
    return [
        block for message in session.agent.messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and block.get("type") == "tool_result"
    ]


# -- ask mode is hard --------------------------------------------------------


@pytest.mark.asyncio
async def test_ask_mode_refuses_mutation_even_with_full_access(tmp_path):
    """write and exec deny with the mode named, without parking an approval
    -- and `auto` permission does not open the gate: the axes compose."""

    manager = _manager(tmp_path, [
        [tool("bash", command="echo hi", _id="t1")],
        [tool("write_file", path="a.txt", content="x", _id="t2")],
    ])
    session = manager.create(permission_mode="auto")
    await session.set_interaction_mode("ask")

    await asyncio.wait_for(session.run("try to change things"), timeout=10)

    results = [r["content"] for r in _tool_results(session)]
    assert len(results) == 2
    assert all("ask mode" in r for r in results), results
    assert manager.approvals.list(session.id) == [], (
        "ask mode parked an approval; the human's posture is not negotiable"
    )
    assert not (session.workspace / "a.txt").exists()


@pytest.mark.asyncio
async def test_ask_mode_still_answers(tmp_path):
    """Reads pass, and the model is told about the posture it is held to."""

    manager = _manager(tmp_path, [[tool("glob", pattern="*", _id="t1")]])
    session = manager.create()
    await session.set_interaction_mode("ask")

    assert ASK_SECTION in session.agent.system
    await asyncio.wait_for(session.run("what is here?"), timeout=10)

    [result] = _tool_results(session)
    assert "ask mode" not in result["content"]


# -- the axis is single-valued -----------------------------------------------


@pytest.mark.asyncio
async def test_setting_one_mode_clears_the_other(tmp_path):
    manager = _manager(tmp_path, [])
    session = manager.create()
    assert session.interaction_mode == "agent"

    assert await session.set_interaction_mode("plan") == "plan"
    assert session.agent.state.get("plan_mode") is True

    assert await session.set_interaction_mode("ask") == "ask"
    assert session.agent.state.get("plan_mode") is False
    assert session.agent.state.get("ask_mode") is True

    assert await session.set_interaction_mode("agent") == "agent"
    assert session.agent.state.get("ask_mode") is False
    assert ASK_SECTION not in session.agent.system

    with pytest.raises(ValueError, match="interaction mode"):
        await session.set_interaction_mode("yolo")


def test_the_fold_recovers_the_last_logged_value():
    events = [
        {"type": "ask_mode", "active": True},
        {"type": "other"},
        {"type": "ask_mode", "active": False},
        {"type": "ask_mode", "active": True},
    ]
    assert fold_ask_mode(events) is True
    assert fold_ask_mode(events[:3]) is False
    assert fold_ask_mode([]) is False


@pytest.mark.asyncio
async def test_a_restored_ask_session_stays_ask(tmp_path):
    """The fail-safe direction, pinned: the human said "answer, don't act";
    a process restart must not quietly turn that back into an acting agent."""

    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic(), state_store=store)
    session = manager.create()
    await session.run("hello")  # give the log a transcript
    await session.set_interaction_mode("ask")

    second = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(), state_store=store,
    )
    [restored] = [s for s in second.restore_sessions() if s.id == session.id]
    assert restored.agent.state.get("ask_mode") is True
    assert restored.interaction_mode == "ask"
    store.close()


# -- over HTTP: one route, two axes ------------------------------------------


def test_both_axes_over_http(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager, settings=settings)
    with TestClient(app) as client:
        created = client.post("/sessions", json={
            "mode": "ask", "permission": "approve",
        }).json()
        assert created["mode"] == "ask"
        assert created["permission_mode"] == "approve"

        sid = created["id"]
        flipped = client.post(f"/sessions/{sid}/mode",
                              json={"mode": "plan"}).json()
        assert flipped["mode"] == "plan"
        assert flipped["permission_mode"] == "approve", (
            "an interaction-mode flip must not touch the permission axis"
        )

        # Back-compat: a permission token in `mode` still lands there.
        legacy = client.post(f"/sessions/{sid}/mode",
                             json={"mode": "auto"}).json()
        assert legacy["permission_mode"] == "auto"
        assert legacy["mode"] == "plan", (
            "a permission change must not touch the interaction axis"
        )

        assert client.post(f"/sessions/{sid}/mode", json={}).status_code == 400
        assert client.post(f"/sessions/{sid}/mode",
                           json={"mode": "yolo"}).status_code == 422
