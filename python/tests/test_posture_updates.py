"""A mid-conversation permission change is told to the model, not sprung on it.

Before this, flipping `permission_mode` changed what the hook asked or
refused and said nothing: the model discovered the new rules by colliding
with them. Codex renders permission state into every step's world state and
marks changes explicitly; the miniature here is `change_permission_mode`
queueing one note that `posture_injector` delivers at the next round,
wrapped in <posture_update> -- deliberately NOT <user_interjection>,
because a rule change is harness-authored fact, not user prose.

Silent paths stay silent on purpose: creation-time sets (no conversation to
notify yet) and direct attribute writes (process-local callers own their
sessions).
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, turns=4):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=scripted(
            [([text(f"turn {i}")], "end_turn") for i in range(turns)]
        )),
    )


def test_a_mid_session_change_reaches_the_model(tmp_path):
    manager = _manager(tmp_path)
    session = manager.create()

    asyncio.run(session.run("hello"))
    session.change_permission_mode("auto")
    asyncio.run(session.run("continue"))

    flat = str(session.agent.messages)
    assert "<posture_update>" in flat
    assert "interactive -> auto" in flat
    assert "without asking" in flat, "the note lost its meaning gloss"


def test_a_pre_first_run_change_stays_silent(tmp_path):
    """The first turn meets the posture as a fact, not a change."""

    manager = _manager(tmp_path)
    session = manager.create()
    session.change_permission_mode("auto")

    asyncio.run(session.run("hello"))

    assert "<posture_update>" not in str(session.agent.messages)
    assert session.permission_mode == "auto"


def test_an_unknown_mode_is_rejected_and_changes_nothing(tmp_path):
    manager = _manager(tmp_path)
    session = manager.create()
    with pytest.raises(ValueError, match="permission mode"):
        session.change_permission_mode("yolo")
    assert session.permission_mode == "interactive"
    assert session._posture_notes == []


def test_a_no_op_change_queues_no_note(tmp_path):
    manager = _manager(tmp_path)
    session = manager.create()
    asyncio.run(session.run("hello"))
    session.change_permission_mode("interactive")
    assert session._posture_notes == []


def test_the_http_edge_delivers_the_note(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic(responder=scripted(
        [([text(f"turn {i}")], "end_turn") for i in range(4)]
    )))
    app = create_app(manager=manager, settings=settings)
    with TestClient(app) as client:
        sid = client.post("/sessions", json={}).json()["id"]
        client.post(f"/sessions/{sid}/messages", json={"message": "hello"})
        client.post(f"/sessions/{sid}/mode", json={"mode": "readonly"})
        client.post(f"/sessions/{sid}/messages", json={"message": "again"})

        flat = str(manager._sessions[sid].agent.messages)
        assert "<posture_update>" in flat
        assert "interactive -> readonly" in flat
