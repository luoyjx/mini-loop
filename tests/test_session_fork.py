"""Fork: branch a new session from an idle session's transcript.

dsh forks only at a durable completed-turn boundary -- a mid-turn prefix
"need not be a valid provider transcript" (its 2026-08-02 note rejected
cutting at an arbitrary assistant message for exactly that reason). For
mini-loop an idle session's tail IS that boundary: the transcript repair
invariant keeps tool pairs balanced whenever no turn is in flight. So fork
copies an idle transcript whole and refuses a busy one.

The conversation forks; the workspace does not (fresh and empty, lineage
in state) -- `worktrees` is the file-level branching tool, and silently
copying a working tree is the kind of surprise this codebase refuses.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, responder=None, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
        **kwargs,
    )


def test_a_fork_carries_the_conversation_and_diverges(tmp_path):
    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text("noted")], "end_turn"

    manager = _manager(tmp_path, responder)
    source = manager.create()
    asyncio.run(source.run("the codeword is xyzzy"))

    child = asyncio.run(manager.fork_session(source.id))
    assert child.id != source.id
    assert child.agent.state["forked_from"]["session"] == source.id

    asyncio.run(child.run("what was the codeword?"))
    # The child's request carries the parent's history...
    assert any("xyzzy" in str(m.get("content", "")) for m in seen[-1])
    # ...and diverging the child leaves the parent untouched.
    assert len(child.agent.messages) > len(source.agent.messages)
    assert not any(
        "what was the codeword" in str(m.get("content", ""))
        for m in source.agent.messages
    )


def test_the_transcripts_share_no_mutable_row(tmp_path):
    manager = _manager(tmp_path)
    source = manager.create()
    asyncio.run(source.run("original"))
    child = asyncio.run(manager.fork_session(source.id))
    child.agent.messages[0]["content"] = "EDITED-IN-CHILD"
    assert source.agent.messages[0]["content"] != "EDITED-IN-CHILD"


def test_a_busy_session_refuses_to_fork(tmp_path):
    async def probe():
        manager = SessionManager(
            Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                     skills_dir=SKILLS, spill_dir=None),
            # An async provider delay keeps the turn genuinely in flight
            # while the fork attempt lands mid-turn.
            FakeAsyncAnthropic(delay=0.2),
        )
        source = manager.create()
        turn = asyncio.create_task(source.run("slow turn"))
        for _ in range(400):
            if source.busy:
                break
            await asyncio.sleep(0.005)
        assert source.busy, "the turn never reached its in-flight state"
        with pytest.raises(RuntimeError, match="open turn"):
            await manager.fork_session(source.id)
        await turn

    asyncio.run(probe())


def test_the_fork_is_durable_before_its_first_turn(tmp_path):
    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, state_store=store)
    source = manager.create()
    asyncio.run(source.run("remember me"))
    child = asyncio.run(manager.fork_session(source.id))
    stored = store.load_messages(child.id)
    assert stored, "the forked transcript never reached the store"
    assert any("remember me" in str(m.get("content", "")) for m in stored)
    store.close()


def test_fork_over_http_is_owner_scoped(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    alice, bob = "tok-alice-000000000000", "tok-bob-1111111111111"
    manager = _manager(tmp_path)
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({alice: "alice", bob: "bob"})
        headers = {"Authorization": f"Bearer {alice}"}
        session_id = client.post("/sessions", json={}, headers=headers).json()["id"]
        client.post(f"/sessions/{session_id}/messages",
                    json={"message": "hello"}, headers=headers)

        forked = client.post(f"/sessions/{session_id}/fork", headers=headers)
        assert forked.status_code == 200
        assert forked.json()["id"] != session_id
        # The child belongs to alice, not to the route's caller-of-the-moment.
        assert manager._sessions[forked.json()["id"]].owner == "alice"

        foreign = {"Authorization": f"Bearer {bob}"}
        assert client.post(f"/sessions/{session_id}/fork",
                           headers=foreign).status_code == 404


# -- observability (round 202) -----------------------------------------------
# The fork existed for one round with no trace in either session's stream:
# the source's log did not know it had been duplicated, and listings could
# not tell a fork from an original. Round 196's rule: what happened shows
# where people look.

def test_the_source_stream_records_the_fork(tmp_path):
    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, state_store=store)
    source = manager.create()
    asyncio.run(source.run("about to be duplicated"))
    child = asyncio.run(manager.fork_session(source.id))

    events = store.load_events(source.id)
    [forked] = [e for e in events if e.get("type") == "session_forked"]
    assert forked["child"] == child.id
    assert forked["message_count"] == len(source.agent.messages)
    store.close()


def test_listings_tell_a_fork_from_an_original(tmp_path):
    manager = _manager(tmp_path)
    source = manager.create()
    asyncio.run(source.run("origin story"))
    child = asyncio.run(manager.fork_session(source.id))
    assert source.info()["forked_from"] is None
    assert child.info()["forked_from"]["session"] == source.id
