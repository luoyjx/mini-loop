"""Cross-module tests -- the failures that only appear when seams are combined.

Every module here was tested in isolation first and passed. Each test below
corresponds to a bug that only existed at the boundary between two of them, so
they are grouped by *pair* rather than by module.
"""

import asyncio
import os
from pathlib import Path

import pytest

from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.sandbox import NullSandbox, SeatbeltSandbox
from mini_loop.secrets import MASK, SecretRegistry
from mini_loop.storage import SQLiteStateStore
from mini_loop.tools import Toolset

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"

seatbelt_only = pytest.mark.skipif(
    not SeatbeltSandbox.available(), reason="Seatbelt is macOS-only"
)


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


# --- sandbox x workspace switching -----------------------------------------

@seatbelt_only
def test_a_sandbox_rebinds_when_the_workspace_moves(tmp_path):
    """`enter_workspace` moves the agent into a worktree (s18).

    A sandbox holding the *previous* workspace as its only writable root denies
    every write in the new one -- silently, and only for sandboxed deployments.
    """
    first, second = tmp_path / "ws1", tmp_path / "ws2"
    first.mkdir()
    second.mkdir()
    sandbox = SeatbeltSandbox(writable_roots=[first])

    assert "ok" in Toolset(first, sandbox=sandbox).run_bash("echo ok > f.txt && cat f.txt")
    # Same sandbox object, different workspace.
    assert "ok" in Toolset(second, sandbox=sandbox).run_bash("echo ok > f.txt && cat f.txt")


def test_rebinding_keeps_policy_and_extra_roots(tmp_path):
    shared = tmp_path / "shared"
    protected = tmp_path / "protected"
    sandbox = SeatbeltSandbox(
        writable_roots=[tmp_path / "ws1"],
        unreadable_roots=[protected],
        allow_network=True,
        _extra_writable=[shared],
    )
    rebound = sandbox.for_workspace(tmp_path / "ws2")

    assert str((tmp_path / "ws2").resolve()) in rebound.writable_roots
    assert str(shared.resolve()) in rebound.writable_roots
    assert str((tmp_path / "ws1").resolve()) not in rebound.writable_roots
    assert rebound.unreadable_roots == sandbox.unreadable_roots
    assert rebound.allow_network is True


def test_null_sandbox_rebinding_is_a_no_op(tmp_path):
    sandbox = NullSandbox()
    assert sandbox.for_workspace(tmp_path) is sandbox


# --- secrets x sandbox ------------------------------------------------------

@seatbelt_only
def test_secret_handling_survives_sandboxed_execution(tmp_path):
    """Narrow injection and wide masking must both still work under sandbox-exec."""
    ws = tmp_path / "ws"
    ws.mkdir()
    name = "MINILOOP_COMPOSE_TOKEN"
    value = "sk-compose-0123456789abcdef"
    os.environ[name] = value
    try:
        toolset = Toolset(
            ws,
            secrets=SecretRegistry.from_environ(extra_names=[name]),
            sandbox=SeatbeltSandbox(writable_roots=[ws]),
        )
        # Not named -> never handed over. Pattern avoids the name on purpose.
        blind = toolset.run_bash("printenv | grep -c 'sk-compose' || true")
        assert blind.strip().startswith("0")
        # Named -> injected, and still masked on the way out.
        named = toolset.run_bash(f'echo "${name}"')
        assert value not in named
        assert MASK in named
    finally:
        os.environ.pop(name, None)


# --- storage x agent-side state --------------------------------------------

def test_the_todo_board_survives_a_restart(tmp_path):
    """The transcript mentions the plan; nothing rebuilt it.

    A restored session with an empty board silently loses the s05 nag and the
    runtime-state reminder, while its own transcript still shows the plan.
    """
    db = tmp_path / "state.db"
    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    session = manager.create()
    asyncio.run(session.run("go"))
    session.agent.todo.update(
        [{"content": "step A", "status": "in_progress", "activeForm": "doing A"}]
    )
    asyncio.run(session.run("continue"))
    asyncio.run(manager.stop())
    store.close()

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    restored = manager.restore_sessions()[0]
    assert [t["content"] for t in restored.agent.todo.items] == ["step A"]
    assert restored.agent.todo.has_open_items()
    asyncio.run(manager.stop())
    store.close()


def test_run_count_and_status_do_not_freeze_at_creation(tmp_path):
    """`create()` wrote the row once; nothing refreshed it afterwards."""
    db = tmp_path / "state.db"
    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    session = manager.create()
    asyncio.run(session.run("one"))
    asyncio.run(session.run("two"))
    asyncio.run(manager.stop())
    store.close()

    store = SQLiteStateStore(db)
    record = store.load_sessions()[0]
    assert record.run_count == 2, "run_count froze at its creation value"
    store.close()


def test_schema_v2_upgrades_to_v3(tmp_path):
    """A v2 database predates the todo column."""
    import sqlite3

    path = tmp_path / "state.db"
    raw = sqlite3.connect(str(path))
    raw.executescript(
        """
        CREATE TABLE schema_version (version INTEGER NOT NULL);
        INSERT INTO schema_version (version) VALUES (2);
        CREATE TABLE sessions (
            session_id TEXT PRIMARY KEY, workspace TEXT NOT NULL, system TEXT,
            created_at REAL NOT NULL, run_count INTEGER NOT NULL DEFAULT 0,
            status TEXT NOT NULL DEFAULT 'idle');
        CREATE TABLE messages (
            session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
            epoch INTEGER NOT NULL DEFAULT 1, payload TEXT NOT NULL,
            PRIMARY KEY (session_id, ordinal));
        CREATE TABLE events (
            session_id TEXT NOT NULL, ordinal INTEGER NOT NULL, payload TEXT NOT NULL,
            PRIMARY KEY (session_id, ordinal));
        INSERT INTO sessions VALUES ('s1', '/tmp/ws', NULL, 1.0, 3, 'idle');
        """
    )
    raw.commit()
    raw.close()

    store = SQLiteStateStore(path)
    record = store.load_sessions()[0]
    assert record.run_count == 3
    assert record.todos == ()
    store.close()


# --- recovery x cache policy ------------------------------------------------

def test_reactive_compaction_shrinks_the_live_conversation(tmp_path):
    """Recovery's contract predates the cache policy that broke it.

    `reactive_compact` used to mutate `kwargs["messages"]` on the assumption it
    aliased `agent.messages`. A `CachePolicy` annotates onto a *copy*, so the
    aliasing silently stopped holding -- and only when caching was enabled,
    which made the behaviour depend on an unrelated seam.
    """
    from mini_loop.agent import Agent
    from mini_loop.caching import DefaultCachePolicy, NullCachePolicy
    from mini_loop.skills import SkillLoader

    class PromptTooLong(Exception):
        def __init__(self):
            super().__init__("prompt is too long: 300000 tokens")

    for policy in (DefaultCachePolicy(), NullCachePolicy()):
        calls = {"n": 0}

        class Client:
            class messages:
                @staticmethod
                async def create(**kwargs):
                    calls["n"] += 1
                    if calls["n"] == 1:
                        raise PromptTooLong()
                    return type(
                        "R", (), {"content": [], "stop_reason": "end_turn", "usage": None}
                    )()

        agent = Agent(
            client=Client(),
            settings=_settings(tmp_path),
            workspace=tmp_path / "ws",
            skills=SkillLoader(SKILLS_DIR),
            cache_policy=policy,
        )
        agent.messages = [
            {"role": "user", "content": [{"type": "text", "text": f"m{i}"}]}
            for i in range(40)
        ]
        before = len(agent.messages)
        asyncio.run(agent._create(agent.messages, tools=[], system="s"))
        assert len(agent.messages) < before, (
            f"{type(policy).__name__}: live history was not compacted, so the "
            "next turn rebuilds the same oversized prompt"
        )


# --- cron restore x storage -------------------------------------------------

def test_the_cron_restore_path_rehydrates_too(tmp_path):
    """A durable cron job resolves its session by id, not through
    `restore_sessions` -- and that path skipped rehydration entirely, so the
    next flush appended a second history into the first one's epoch."""
    db = tmp_path / "state.db"
    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    session = manager.create()
    session_id = session.id
    asyncio.run(session.run("first"))
    persisted = store.message_count(session_id)
    asyncio.run(manager.stop())
    store.close()

    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    revived = manager.restore_scheduled_session(session_id)
    assert len(revived.agent.messages) == persisted, "cron path started blind"
    assert revived.run_count > 0
    asyncio.run(manager.stop())
    store.close()


# --- workflow workers x secrets --------------------------------------------

def test_workflow_workers_inherit_masking_and_confinement(tmp_path):
    """A fresh workflow worker still reads repository files.

    It was the one agent path constructed directly rather than through the
    manager, so it inherited neither the secret registry nor the sandbox.
    """
    import inspect

    from mini_loop.workflows.runner import FreshAgentRunner
    from mini_loop.workflows.service import WorkflowService

    for target in (FreshAgentRunner.__init__, WorkflowService.__init__):
        params = inspect.signature(target).parameters
        assert "secrets" in params, f"{target.__qualname__} drops the registry"
        assert "sandbox" in params, f"{target.__qualname__} drops the sandbox"

    registry = SecretRegistry()
    registry.register("WF_TOKEN", "sk-wf-0123456789abcdef")
    runner = FreshAgentRunner(
        client=object(),
        settings=_settings(tmp_path),
        workspace=tmp_path / "ws",
        context_resolver=lambda attempt: None,
        secrets=registry,
    )
    assert runner.secrets is registry


# --- rewrite detection: correctness and cost -------------------------------

def _flushed_session(tmp_path, db):
    store = SQLiteStateStore(db)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=store
    )
    session = manager.create()
    asyncio.run(session.run("go"))
    return manager, session, store


def test_a_rebuilt_transcript_is_detected(tmp_path):
    """`messages[:] = [...]` -- what both compaction layers do."""
    manager, session, store = _flushed_session(tmp_path, tmp_path / "s.db")
    live = session.agent.messages
    assert not session._transcript_was_rewritten(live)

    live[:] = [dict(m) for m in live]  # same content, new objects
    assert session._transcript_was_rewritten(live)
    asyncio.run(manager.stop())
    store.close()


def test_a_shortened_transcript_is_detected(tmp_path):
    manager, session, store = _flushed_session(tmp_path, tmp_path / "s.db")
    session.agent.messages[:] = [{"role": "user", "content": "[summary]"}]
    assert session._transcript_was_rewritten(session.agent.messages)
    asyncio.run(manager.stop())
    store.close()


def test_appending_alone_is_not_a_rewrite(tmp_path):
    """Growth is the normal case and must stay on the cheap path."""
    manager, session, store = _flushed_session(tmp_path, tmp_path / "s.db")
    session.agent.messages.append({"role": "user", "content": "next"})
    assert not session._transcript_was_rewritten(session.agent.messages)
    asyncio.run(manager.stop())
    store.close()


def test_in_place_mutation_is_a_documented_blind_spot(tmp_path):
    """Recorded, not pretended away.

    Detection compares object identity, so editing a message dict *without*
    replacing it is invisible. Nothing in the codebase does that -- every
    rewrite goes through `messages[:] = [...]`. If this test starts failing,
    someone introduced in-place mutation and needs to say so rather than
    inherit a guarantee that no longer holds.
    """
    manager, session, store = _flushed_session(tmp_path, tmp_path / "s.db")
    session.agent.messages[0]["content"] = "edited in place"
    assert not session._transcript_was_rewritten(session.agent.messages)
    asyncio.run(manager.stop())
    store.close()


def test_detection_does_not_scale_with_transcript_bytes(tmp_path):
    """The regression this replaced: O(bytes) hashing on *every* event.

    Measured at 5.5 ms per pass over a 1.6 MB transcript, i.e. ~550 ms of pure
    overhead across 100 events. Identity comparison is O(pointers).
    """
    import time

    manager, session, store = _flushed_session(tmp_path, tmp_path / "s.db")

    def timed(kb: int) -> float:
        blob = "x" * (kb * 1024)
        messages = [
            {"role": "user", "content": [{"type": "text", "text": blob}]}
            for _ in range(200)
        ]
        session._persisted_refs = list(messages)
        session._persisted_messages = len(messages)
        start = time.perf_counter()
        for _ in range(20):
            session._transcript_was_rewritten(messages)
        return time.perf_counter() - start

    small, large = timed(1), timed(16)
    # 16x the bytes must not cost anywhere near 16x the time.
    assert large < small * 4, f"cost tracked payload size: {small=} {large=}"
    asyncio.run(manager.stop())
    store.close()
