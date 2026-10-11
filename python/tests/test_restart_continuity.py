"""Restoring a session is only half of it; the next request is the other half.

Rounds 11, 23 and 24 built durable state, restore, and the repair that closes a
`tool_use` a crash left unanswered. The tests for it check that the messages
come back. **None of them then makes another request**, which is the shape round
55 found in teams: the layer below is tested and the one above is not.

That matters here specifically because round 43 measured the real API rejecting
an unanswered `tool_use` with a 400, and made the offline model reject it too. A
restored transcript that is subtly malformed comes back looking fine and fails
on the first request after.

Both paths were verified against the real endpoint before these were written:

    restart, clean            -> restored 4 messages, agent recalled PELICAN
    restart, crashed mid-tool -> stored transcript ended with an unanswered
                                 tool_use; restore synthesized a tool_result and
                                 the real API accepted it

These pin the same two against the contract-enforcing double, where a malformed
restore raises instead of costing an API call to discover.
"""

import asyncio
import pathlib
import sqlite3

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.actions import UNKNOWN_RESULT
from mini_loop.fake_llm import FakeAsyncAnthropic, InvalidTranscript, validate_transcript
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, store):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        state_store=store,
    )


def _tool_use_ids(messages):
    return [
        block["id"]
        for message in messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and block.get("type") == "tool_use"
    ]


def _worked_session(tmp_path):
    """A session that ran a turn with a tool call, then flushed."""
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()
    asyncio.run(session.agent.run("do the thing"))
    session._flush_messages()
    messages = list(session.agent.messages)
    store.close()
    return session.id, messages


def _restore(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    manager.restore_sessions()
    return manager, store


# --- a clean restart ------------------------------------------------------

def test_a_restored_session_can_make_another_request(tmp_path):
    """The half no existing test covered."""
    session_id, before = _worked_session(tmp_path)
    manager, store = _restore(tmp_path)
    restored = manager.get(session_id)

    assert restored is not None
    assert len(restored.agent.messages) == len(before)
    assert asyncio.run(restored.agent.run("and again"))
    store.close()


def test_the_restored_transcript_satisfies_the_contract(tmp_path):
    session_id, _ = _worked_session(tmp_path)
    manager, store = _restore(tmp_path)
    validate_transcript(manager.get(session_id).agent.messages)
    store.close()


def test_work_continues_to_persist_after_a_restart(tmp_path):
    session_id, before = _worked_session(tmp_path)
    manager, store = _restore(tmp_path)
    asyncio.run(manager.get(session_id).agent.run("and again"))
    manager.get(session_id)._flush_messages()
    assert store.message_count(session_id) > len(before)
    store.close()


def test_a_session_keeps_its_owner_across_a_restart(tmp_path):
    """The tenant owner is durable. `SessionRecord` carried no owner, so a
    restart rebuilt every session as the `anonymous` default -- under
    authentication that orphans it: `_require` compares `session.owner` to the
    caller, and "anonymous" matches no real principal, so the owner is refused
    access to their own restored session. Restore now carries the owner through
    both paths (`restore_sessions` and a cron job's `restore_scheduled_session`,
    which share `_rehydrate`).
    """
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()
    session_id = session.id
    session.owner = "alice"
    asyncio.run(session.agent.run("do the thing"))
    session._flush_messages()
    store.close()

    manager2, store2 = _restore(tmp_path)
    assert manager2.get(session_id).owner == "alice", (
        "a restart orphaned the session from its owner"
    )
    store2.close()

    # The cron restore path (restore_scheduled_session) carries it too.
    store3 = SQLiteStateStore(tmp_path / "state.db")
    manager3 = _manager(tmp_path, store3)
    assert manager3.restore_scheduled_session(session_id).owner == "alice"
    store3.close()


def test_a_deleted_session_does_not_come_back_after_a_restart(tmp_path):
    """`delete()` is permanent, unlike `stop()`.

    Its durable `sessions` row used to survive -- there was no way to remove one
    -- so `restore_sessions()` on the next startup rebuilt the deleted session,
    re-creating its workspace and rehydrating its transcript. A restart restores
    what merely stopped, not what was deleted; conflating the two turned every
    restart into a resurrection of everything a user had ever deleted.
    """
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()
    session_id, workspace = session.id, session.workspace
    asyncio.run(session.agent.run("do the thing"))
    session._flush_messages()

    manager.delete(session_id)
    # The durable record is gone, and with it the lease that lived in its row.
    assert store.load_messages(session_id) == []
    assert all(r.session_id != session_id for r in store.load_sessions())
    store.close()

    manager2, store2 = _restore(tmp_path)
    assert manager2.get(session_id) is None, "restore rebuilt a deleted session"
    assert not workspace.exists(), "restore re-created the deleted session's workspace"
    store2.close()


# --- a crash between dispatching a tool and recording its result ----------

def _crash_mid_tool(tmp_path):
    """Leave on disk exactly what a kill between the two writes would leave."""
    session_id, messages = _worked_session(tmp_path)
    cut = max(
        index + 1
        for index, message in enumerate(messages)
        if isinstance(message.get("content"), list)
        and any(isinstance(b, dict) and b.get("type") == "tool_use"
                for b in message["content"])
    )
    truncated = messages[:cut]
    assert _tool_use_ids(truncated), "the fixture produced no tool call to orphan"

    database = sqlite3.connect(tmp_path / "state.db")
    database.execute("DELETE FROM messages WHERE session_id=?", (session_id,))
    database.commit()
    database.close()

    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages(session_id, truncated, epoch=1)
    store.close()
    return session_id, truncated


def test_the_stored_shape_really_is_the_broken_one(tmp_path):
    """Without this the repair tests could be passing on a healthy transcript."""
    session_id, truncated = _crash_mid_tool(tmp_path)
    with pytest.raises(InvalidTranscript, match="without `tool_result`"):
        validate_transcript(truncated)


def test_restore_repairs_the_dangling_call(tmp_path):
    session_id, _ = _crash_mid_tool(tmp_path)
    manager, store = _restore(tmp_path)
    messages = manager.get(session_id).agent.messages

    validate_transcript(messages)
    # One constant, not two. `session.py` carried its own wording of the same
    # message -- "The process terminated after this tool was dispatched" against
    # `actions.py`'s "This tool was dispatched but the process terminated" -- so
    # a change to either would have left the two paths saying different things
    # about the same situation.
    assert UNKNOWN_RESULT in str(messages[-1])
    store.close()


def test_a_repaired_session_can_make_another_request(tmp_path):
    """The one that would have cost a 400 against a real provider."""
    session_id, _ = _crash_mid_tool(tmp_path)
    manager, store = _restore(tmp_path)
    assert asyncio.run(manager.get(session_id).agent.run("carry on"))
    store.close()


def test_the_agent_is_told_not_to_retry_the_side_effect(tmp_path):
    """`unknown` exists so a side effect that may already have happened is not
    repeated; the message has to say that, not merely report a failure."""
    session_id, _ = _crash_mid_tool(tmp_path)
    manager, store = _restore(tmp_path)
    repaired = str(manager.get(session_id).agent.messages[-1]).lower()

    assert "not known" in repaired
    assert "do not retry" in repaired
    assert "already took effect" in repaired
    assert "error" not in repaired, (
        "calling it an error invites exactly the retry this is preventing"
    )
    store.close()


def test_there_is_one_unknown_result_message(tmp_path):
    """Both paths that can produce it must produce the same words.

    The session-level repair and the action journal's reconciliation describe
    the same event, and each had its own phrasing. Round 27's lesson about a
    stale claim being duplicated applies to a message the model reads too.
    """
    import mini_loop

    package = pathlib.Path(mini_loop.__file__).parent
    spelling_it = sorted(
        path.name for path in package.rglob("*.py")
        if "[unknown] " in path.read_text()
    )
    assert spelling_it == ["actions.py"], (
        f"the message is written out in {spelling_it}; it should be defined once "
        "in actions.py and imported everywhere else"
    )
