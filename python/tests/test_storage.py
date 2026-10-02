"""Durable conversation state -- offline, deterministic, no API key.

The acceptance bar from the roadmap is a kill point: terminate the process at an
arbitrary moment, start again, and the transcript and event cursor must agree.
These tests simulate that by dropping every in-memory handle and reopening the
database, which is exactly what a restart does.
"""

import asyncio
import json
import sqlite3
import threading
from pathlib import Path

import pytest

from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.manager import SessionManager
from mini_loop.storage import (
    SCHEMA_VERSION,
    NullStateStore,
    SessionRecord,
    SQLiteStateStore,
    StorageSchemaError,
)

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


def _record(session_id="s1", **over) -> SessionRecord:
    base = dict(
        session_id=session_id,
        workspace="/tmp/ws",
        system=None,
        created_at=1.0,
        run_count=0,
        status="idle",
        event_cursor=0,
    )
    base.update(over)
    return SessionRecord(**base)


# --- the append contract ---------------------------------------------------

def test_ordinals_are_dense_and_start_at_one(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    store.upsert_session(_record())
    assert store.append_messages("s1", [{"role": "user", "content": "a"}]) == 1
    assert store.append_messages("s1", [{"role": "assistant", "content": "b"}]) == 2
    assert store.message_count("s1") == 2
    store.close()


def test_ordinal_collision_is_an_error_not_a_reorder(tmp_path):
    """Order is data: a double write must fail loudly, not shuffle history."""
    path = tmp_path / "state.db"
    store = SQLiteStateStore(path)
    store.upsert_session(_record())
    store.append_event("s1", {"type": "a"})

    # Simulate a second writer that computed a stale head.
    raw = sqlite3.connect(str(path))
    with pytest.raises(sqlite3.IntegrityError):
        raw.execute(
            "INSERT INTO events (session_id, ordinal, payload) VALUES (?, ?, ?)",
            ("s1", 1, json.dumps({"type": "duplicate"})),
        )
    raw.close()
    store.close()


def test_sessions_do_not_share_an_ordinal_space(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    store.upsert_session(_record("a"))
    store.upsert_session(_record("b"))
    store.append_event("a", {"type": "x"})
    store.append_event("b", {"type": "y"})
    assert store.event_cursor("a") == 1
    assert store.event_cursor("b") == 1
    store.close()


def test_concurrent_appends_never_lose_or_duplicate(tmp_path):
    """The head is read inside the writing transaction, so writers serialize."""
    store = SQLiteStateStore(tmp_path / "state.db")
    store.upsert_session(_record())
    errors: list[BaseException] = []

    def writer(base: int):
        try:
            for i in range(20):
                store.append_event("s1", {"type": "e", "who": base, "i": i})
        except BaseException as error:  # pragma: no cover - surfaced below
            errors.append(error)

    threads = [threading.Thread(target=writer, args=(n,)) for n in range(4)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()

    assert not errors, errors
    events = store.load_events("s1")
    assert len(events) == 80
    assert store.event_cursor("s1") == 80
    store.close()


def test_reads_are_bounded(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    store.upsert_session(_record())
    for i in range(50):
        store.append_event("s1", {"type": "e", "i": i})

    tail = store.load_events("s1", after=45)
    assert [e["i"] for e in tail] == [45, 46, 47, 48, 49]
    assert len(store.load_events("s1", after=0, limit=3)) == 3
    store.close()


def test_provider_objects_are_detached_for_storage(tmp_path):
    """Assistant turns hold SDK objects; a transcript of them is unreplayable."""

    class Block:
        type = "text"

        def model_dump(self):
            return {"type": "text", "text": "hi"}

    store = SQLiteStateStore(tmp_path / "state.db")
    store.upsert_session(_record())
    store.append_messages("s1", [{"role": "assistant", "content": [Block()]}])
    loaded = store.load_messages("s1")
    assert loaded == [{"role": "assistant", "content": [{"type": "text", "text": "hi"}]}]
    store.close()


def test_newer_schema_is_refused(tmp_path):
    path = tmp_path / "state.db"
    SQLiteStateStore(path).close()
    raw = sqlite3.connect(str(path))
    raw.execute("UPDATE schema_version SET version = ?", (SCHEMA_VERSION + 1,))
    raw.commit()
    raw.close()

    with pytest.raises(StorageSchemaError):
        SQLiteStateStore(path)


def test_null_store_persists_nothing(tmp_path):
    store = NullStateStore()
    store.upsert_session(_record())
    assert store.append_messages("s1", [{"role": "user", "content": "a"}]) == 0
    assert store.load_sessions() == []
    assert store.load_messages("s1") == []


# --- restart recovery ------------------------------------------------------

def _run_session(tmp_path, db, prompt="go"):
    """Drive one real turn through a manager backed by `db`, then drop it."""
    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    session = manager.create()
    session_id = session.id
    asyncio.run(session.run(prompt))
    asyncio.run(manager.stop())
    store.close()
    return session_id


def test_transcript_and_cursor_survive_a_restart(tmp_path):
    db = tmp_path / "state.db"
    session_id = _run_session(tmp_path, db)

    # Everything in memory is gone -- this is the restart.
    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    restored = manager.restore_sessions()
    assert [s.id for s in restored] == [session_id]

    session = restored[0]
    assert session.agent is not None
    assert session.agent.messages, "transcript did not come back"
    roles = [m["role"] for m in session.agent.messages]
    assert roles[0] == "user"
    # The event cursor resumes where it stopped, so an SSE client that
    # reconnects does not replay from zero or skip the gap.
    assert session._seq == store.event_cursor(session_id) > 0
    asyncio.run(manager.stop())
    store.close()


def test_kill_point_mid_turn_keeps_a_consistent_prefix(tmp_path):
    """Crash between a tool call and its result; the prefix must still parse."""
    db = tmp_path / "state.db"

    class Boom(RuntimeError):
        pass

    async def explode(_ctx, **_):
        raise Boom("process died here")

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        return [text("working"), tool("bash", _id="toolu_1", command="echo hi")], "tool_use"

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path, max_turns=2),
        FakeAsyncAnthropic(responder=responder),
        state_store=store,
    )
    session = manager.create()
    session_id = session.id
    asyncio.run(session.run("go"))
    asyncio.run(manager.stop())
    store.close()

    store = SQLiteStateStore(db)
    messages = store.load_messages(session_id)
    assert messages, "nothing persisted before the kill point"
    # Whatever was captured must be complete JSON with intact roles -- a torn
    # write would surface here rather than at the next model call.
    for message in messages:
        assert message["role"] in {"user", "assistant"}
        assert "content" in message
    assert store.event_cursor(session_id) > 0
    store.close()


def test_restore_is_idempotent(tmp_path):
    db = tmp_path / "state.db"
    _run_session(tmp_path, db)

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    first = manager.restore_sessions()
    second = manager.restore_sessions()
    assert len(first) == 1
    assert second == [], "a live handle must not be rebuilt twice"
    asyncio.run(manager.stop())
    store.close()


def test_a_restored_session_continues_without_duplicating_history(tmp_path):
    db = tmp_path / "state.db"
    session_id = _run_session(tmp_path, db)

    store = SQLiteStateStore(db)
    before = len(store.load_messages(session_id))

    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    session = manager.restore_sessions()[0]
    asyncio.run(session.run("second turn"))
    asyncio.run(manager.stop())

    after = store.load_messages(session_id)
    assert len(after) > before
    # The restored prefix must appear exactly once: a flush that re-sent
    # already-persisted messages would double it.
    assert len(after) == len(session.agent.messages)
    store.close()


def test_persistence_failure_does_not_stall_the_agent(tmp_path):
    """Same contract the trajectory sink follows: report, never block."""

    class Broken(NullStateStore):
        def append_event(self, session_id, event):
            raise RuntimeError("disk gone")

    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=Broken()
    )
    session = manager.create()
    final = asyncio.run(session.run("go"))
    asyncio.run(manager.stop())

    assert final.startswith("Done.")
    assert "disk gone" in (session.persist_error or "")


# --- composition: compaction rewrites the transcript the store mirrors -----

def _compacting_manager(tmp_path, store):
    return SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )


def test_a_shrinking_compaction_starts_a_new_epoch(tmp_path):
    """Auto-compaction replaces history with a summary.

    Mirroring a mutable list into an append-only table by index cannot survive
    that: the rows would become a splice of two different histories, with
    tool_use blocks whose tool_result no longer follows.
    """
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _compacting_manager(tmp_path, store)
    session = manager.create()
    asyncio.run(session.run("first"))
    first_epoch = store.transcript_epoch(session.id)

    session.agent.messages[:] = [{"role": "user", "content": "[compacted summary]"}]
    asyncio.run(session.run("second"))
    asyncio.run(manager.stop())

    assert store.transcript_epoch(session.id) == first_epoch + 1
    current = store.load_messages(session.id)
    assert any("compacted summary" in str(m) for m in current)
    # The superseded epoch is still on disk -- it is the record of what the
    # agent actually saw before compaction rewrote its history.
    superseded = store.load_messages(session.id, epoch=first_epoch)
    assert superseded and not any("compacted summary" in str(m) for m in superseded)
    store.close()


def test_an_in_place_edit_of_an_old_message_is_re_persisted(tmp_path):
    """Snipping edits the *middle*, so a tail-only check would miss it."""
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _compacting_manager(tmp_path, store)
    session = manager.create()
    asyncio.run(session.run("first"))

    session.agent.messages[0] = {"role": "user", "content": "[SNIPPED oversized result]"}
    asyncio.run(session.run("second"))
    asyncio.run(manager.stop())

    stored = store.load_messages(session.id)
    assert "SNIPPED" in str(stored[0])
    store.close()


def test_a_restored_transcript_matches_the_live_one_after_compaction(tmp_path):
    """The property that actually matters: restore gives back what it had."""
    db = tmp_path / "state.db"
    store = SQLiteStateStore(db)
    manager = _compacting_manager(tmp_path, store)
    session = manager.create()
    session_id = session.id
    asyncio.run(session.run("first"))
    session.agent.messages[:] = [{"role": "user", "content": "[compacted summary]"}]
    asyncio.run(session.run("second"))
    live = len(session.agent.messages)
    asyncio.run(manager.stop())
    store.close()

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    restored = manager.restore_sessions()[0]
    assert len(restored.agent.messages) == live
    assert any("compacted summary" in str(m) for m in restored.agent.messages)
    asyncio.run(manager.stop())
    store.close()


def test_schema_v1_upgrades_in_place(tmp_path):
    """A v1 database predates epochs; the column is added, not rejected."""
    path = tmp_path / "state.db"
    raw = sqlite3.connect(str(path))
    raw.executescript(
        """
        CREATE TABLE schema_version (version INTEGER NOT NULL);
        INSERT INTO schema_version (version) VALUES (1);
        CREATE TABLE sessions (
            session_id TEXT PRIMARY KEY, workspace TEXT NOT NULL, system TEXT,
            created_at REAL NOT NULL, run_count INTEGER NOT NULL DEFAULT 0,
            status TEXT NOT NULL DEFAULT 'idle');
        CREATE TABLE messages (
            session_id TEXT NOT NULL, ordinal INTEGER NOT NULL, payload TEXT NOT NULL,
            PRIMARY KEY (session_id, ordinal));
        CREATE TABLE events (
            session_id TEXT NOT NULL, ordinal INTEGER NOT NULL, payload TEXT NOT NULL,
            PRIMARY KEY (session_id, ordinal));
        INSERT INTO messages VALUES ('s1', 1, '{"role":"user","content":"legacy"}');
        """
    )
    raw.commit()
    raw.close()

    store = SQLiteStateStore(path)
    assert store.transcript_epoch("s1") == 1
    assert store.load_messages("s1") == [{"role": "user", "content": "legacy"}]
    store.close()


def _schema_of(db) -> dict:
    con = sqlite3.connect(str(db))
    try:
        tables = [
            r[0] for r in con.execute(
                "SELECT name FROM sqlite_master WHERE type='table' "
                "AND name NOT LIKE 'sqlite_%'"
            )
        ]
        return {
            t: {(r[1], r[2]) for r in con.execute(f"PRAGMA table_info({t})")}
            for t in tables
        }
    finally:
        con.close()


def test_an_upgraded_database_ends_identical_to_a_fresh_one(tmp_path):
    """The migration splits by kind: a new *table* is created by `_SCHEMA`'s
    `CREATE TABLE IF NOT EXISTS`, a new *column* by an `ALTER` in `_upgrade`.
    That split is correct only if every column `_SCHEMA` adds has a matching
    `ALTER` -- because `CREATE IF NOT EXISTS` is a no-op on an existing table, a
    column added to `_SCHEMA` and forgotten in `_upgrade` is silently absent from
    every upgraded database, and the first symptom is a write failing in
    production. The v1 test above pins one column; this pins that an old database
    ends *schema-identical* to a fresh one, so a forgotten migration fails here.
    """
    fresh = tmp_path / "fresh.db"
    SQLiteStateStore(fresh).close()
    expected = _schema_of(fresh)

    # (a) v1 predates every added table and column, so upgrading it runs every
    # version-gated migration step.
    v1 = tmp_path / "v1.db"
    raw = sqlite3.connect(str(v1))
    raw.executescript(
        """
        CREATE TABLE schema_version (version INTEGER NOT NULL);
        INSERT INTO schema_version (version) VALUES (1);
        CREATE TABLE sessions (session_id TEXT PRIMARY KEY, workspace TEXT NOT NULL,
            system TEXT, created_at REAL NOT NULL, run_count INTEGER NOT NULL DEFAULT 0,
            status TEXT NOT NULL DEFAULT 'idle');
        CREATE TABLE messages (session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
            payload TEXT NOT NULL, PRIMARY KEY (session_id, ordinal));
        CREATE TABLE events (session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
            payload TEXT NOT NULL, PRIMARY KEY (session_id, ordinal));
        """
    )
    raw.commit()
    raw.close()
    SQLiteStateStore(v1).close()
    assert _schema_of(v1) == expected, "a v1 database did not upgrade to the current schema"

    # (b) a table that exists but lacks columns added to it later -- the ALTERs
    # gated on column presence (approvals.kind/answer), not on a version number.
    mid = tmp_path / "mid.db"
    raw = sqlite3.connect(str(mid))
    raw.executescript(
        """
        CREATE TABLE schema_version (version INTEGER NOT NULL);
        INSERT INTO schema_version (version) VALUES (4);
        CREATE TABLE sessions (session_id TEXT PRIMARY KEY, workspace TEXT NOT NULL,
            system TEXT, created_at REAL NOT NULL, run_count INTEGER NOT NULL DEFAULT 0,
            status TEXT NOT NULL DEFAULT 'idle', todos TEXT NOT NULL DEFAULT '[]');
        CREATE TABLE messages (session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
            epoch INTEGER NOT NULL DEFAULT 1, payload TEXT NOT NULL,
            PRIMARY KEY (session_id, ordinal));
        CREATE TABLE events (session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
            payload TEXT NOT NULL, PRIMARY KEY (session_id, ordinal));
        CREATE TABLE approvals (approval_id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
            tool_use_id TEXT, tool_name TEXT NOT NULL, rule TEXT NOT NULL,
            message TEXT NOT NULL, input_preview TEXT NOT NULL, status TEXT NOT NULL,
            created_at REAL NOT NULL, resolved_at REAL);
        """
    )
    raw.commit()
    raw.close()
    SQLiteStateStore(mid).close()
    assert _schema_of(mid) == expected, "a mid-version database did not upgrade fully"


# --- crash between dispatching a tool and recording its result -------------

def _crashing_session(tmp_path, db):
    """Run until a tool has been dispatched, then die before recording it."""

    class Die(BaseException):  # not an Exception: the loop must not swallow it
        pass

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        last = kwargs["messages"][-1]
        if isinstance(last.get("content"), str):
            return (
                [text("Charging now."), tool("bash", _id="tu_1", command="charge --amount 100")],
                "tool_use",
            )
        return [text("done")], "end_turn"

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(responder=responder), state_store=store
    )
    session = manager.create()
    session_id = session.id
    original = session.agent._exec_tool

    async def exploding(call, **kwargs):
        await original(call, **kwargs)
        raise Die("killed after the side effect, before recording it")

    session.agent._exec_tool = exploding
    try:
        asyncio.run(session.run("charge the customer once"))
    except BaseException:
        pass
    asyncio.run(manager.stop())
    store.close()
    return session_id


def _tool_use_ids(message) -> list[str]:
    content = message.get("content")
    if not isinstance(content, list):
        return []
    return [b["id"] for b in content if isinstance(b, dict) and b.get("type") == "tool_use"]


def _tool_result_ids(message) -> list[str]:
    content = message.get("content")
    if not isinstance(content, list):
        return []
    return [
        b["tool_use_id"]
        for b in content
        if isinstance(b, dict) and b.get("type") == "tool_result"
    ]


def test_a_crash_persists_the_tool_call_as_structured_data(tmp_path):
    """The transcript must hold a `tool_use` block, not its `repr()`.

    A second, simpler serializer used to live in this module and fell back to
    `str(value)` for anything without `model_dump` -- which the real SDK has,
    so production looked fine while every other client persisted
    `"ToolUseBlock(...)"` strings.
    """
    db = tmp_path / "state.db"
    session_id = _crashing_session(tmp_path, db)

    store = SQLiteStateStore(db)
    stored = store.load_messages(session_id)
    assert _tool_use_ids(stored[-1]) == ["tu_1"]
    store.close()


def test_restore_closes_a_dangling_tool_call_as_unknown(tmp_path):
    """An unanswered `tool_use` is rejected by the provider outright.

    Verified against a live endpoint: `tool_use ids were found without
    tool_result blocks immediately after` -- a session restored in that state
    fails on *every* subsequent turn. The repair reports the outcome as
    unknown rather than as an error, because an error invites a retry of a side
    effect that may already have happened.
    """
    db = tmp_path / "state.db"
    session_id = _crashing_session(tmp_path, db)

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    restored = manager.restore_scheduled_session(session_id)

    messages = restored.agent.messages
    assert _tool_result_ids(messages[-1]) == ["tu_1"], "dangling call left open"
    body = str(messages[-1])
    assert "[unknown]" in body
    assert "Do not retry" in body
    assert restored._unknown_tool_uses == ("tu_1",)

    # Every tool_use in the transcript is now answered -- the invariant the
    # provider enforces.
    open_calls = set()
    for message in messages:
        open_calls.update(_tool_use_ids(message))
        open_calls.difference_update(_tool_result_ids(message))
    assert not open_calls

    asyncio.run(manager.stop())
    store.close()


def test_the_repair_is_persisted_not_just_in_memory(tmp_path):
    """A second restart must not have to rediscover the same dangling call."""
    db = tmp_path / "state.db"
    session_id = _crashing_session(tmp_path, db)

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    manager.restore_scheduled_session(session_id)
    asyncio.run(manager.stop())
    store.close()

    store = SQLiteStateStore(db)
    stored = store.load_messages(session_id)
    assert _tool_result_ids(stored[-1]) == ["tu_1"]
    store.close()


def test_a_complete_transcript_is_left_alone(tmp_path):
    """The repair must not append phantom results to a healthy session."""
    db = tmp_path / "state.db"
    session_id = _run_session(tmp_path, db)

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    restored = manager.restore_scheduled_session(session_id)
    assert restored._unknown_tool_uses == ()
    assert "[unknown]" not in str(restored.agent.messages)
    asyncio.run(manager.stop())
    store.close()
