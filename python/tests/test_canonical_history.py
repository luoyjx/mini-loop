"""Superseded epochs are the canonical record, and someone can read them.

Three separate comments (session.py, storage.py twice) make the same claim:
"superseded epochs stay on disk as the record of what the agent actually saw
before compaction rewrote its history." Until round 99 nothing executed it --
no test read an old epoch back, and no operator surface could reach one. A
claim only a comment makes is documentation, not a property (rounds 92/94/95
each relearned this on a different module).

The design is OpenWorker's principle 6 (OPENWORKER_RESEARCH.md section 11):
canonical history is permanent; compaction only changes the outbound
projection. Here the projection is the *current* epoch -- what a restart
restores and the provider sees -- and every earlier epoch is the canonical
record, now pinned readable and exposed at
GET /sessions/{id}/transcript?epoch=N under session ownership.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.compaction import microcompact
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
BODY = "ORIGINAL-" * 60  # > 100 chars, so microcompact clears it


def _manager(tmp_path, store):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS),
        FakeAsyncAnthropic(), state_store=store,
    )


def _transcript(count=8):
    messages = []
    for index in range(count):
        messages.append({"role": "assistant", "content": [
            {"type": "tool_use", "id": f"t{index}", "name": "bash",
             "input": {"command": "echo hi"}}]})
        messages.append({"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"t{index}", "content": BODY}]})
    return messages


def _bodies(messages):
    return [
        part.get("content")
        for message in messages
        if isinstance(message.get("content"), list)
        for part in message["content"]
        if isinstance(part, dict) and part.get("type") == "tool_result"
    ]


def _compacted_session(tmp_path, store):
    session = _manager(tmp_path, store).create()
    session.agent.messages.extend(_transcript())
    session._flush_messages()
    assert microcompact(session.agent.messages) > 0
    session._flush_messages()
    return session


def test_the_superseded_epoch_keeps_the_original_bodies(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _compacted_session(tmp_path, store)

    assert store.transcript_epoch(session.id) == 2
    current = _bodies(store.load_messages(session.id))
    assert any(str(body).startswith("[cleared") for body in current), (
        "the projection did not compact"
    )

    canonical = _bodies(store.load_messages(session.id, epoch=1))
    assert canonical and all(body == BODY for body in canonical), (
        "the superseded epoch is not the record of what the agent saw"
    )
    assert not any(str(body).startswith("[cleared") for body in canonical), (
        "compacted rows leaked into the canonical epoch"
    )
    store.close()


def test_each_rewrite_opens_its_own_epoch(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _compacted_session(tmp_path, store)
    # A later turn produces more clearable results; a second rewrite follows.
    session.agent.messages.extend(_transcript(count=6))
    session._flush_messages()
    assert microcompact(session.agent.messages) > 0
    session._flush_messages()

    assert store.transcript_epoch(session.id) == 3
    for epoch in (1, 2, 3):
        assert store.load_messages(session.id, epoch=epoch), (
            f"epoch {epoch} is unreadable"
        )
    store.close()


def test_the_canonical_record_is_readable_over_http(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    alice, bob = "tok-alice-000000000000", "tok-bob-1111111111111"
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({alice: "alice", bob: "bob"})
        headers = {"Authorization": f"Bearer {alice}"}
        session_id = client.post("/sessions", json={}, headers=headers).json()["id"]

        session = manager._sessions[session_id]
        session.agent.messages.extend(_transcript())
        session._flush_messages()
        assert microcompact(session.agent.messages) > 0
        session._flush_messages()

        projection = client.get(f"/sessions/{session_id}/transcript",
                                headers=headers).json()
        assert projection["epoch"] == projection["epochs"] == 2
        assert any(str(body).startswith("[cleared")
                   for body in _bodies(projection["messages"]))

        canonical = client.get(f"/sessions/{session_id}/transcript?epoch=1",
                               headers=headers).json()
        assert all(body == BODY for body in _bodies(canonical["messages"]))

        # Ownership and bounds behave like every other session surface.
        foreign = {"Authorization": f"Bearer {bob}"}
        assert client.get(f"/sessions/{session_id}/transcript",
                          headers=foreign).status_code == 404
        assert client.get(f"/sessions/{session_id}/transcript?epoch=9",
                          headers=headers).status_code == 404
        assert client.get(f"/sessions/{session_id}/transcript?epoch=0",
                          headers=headers).status_code == 404
