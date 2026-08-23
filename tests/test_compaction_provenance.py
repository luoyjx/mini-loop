"""Compaction records its cost and provenance (Pi P1-4).

The research doc: summary, retained tail, original history, generation
usage and provenance persisted SEPARATELY, and the recovery path proven
not to depend on implicit memory beyond the compacted text. Before this
the compact event carried only kind + transcript path -- an audit could
not answer "what did this compaction cost, and what did it replace" from
the log. The summary itself is prose; these facts are structured.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _big_turn(n):
    # A responder that emits a large assistant text, forcing the token
    # threshold and an eventual summary.
    steps = [([text("padding " * 4000)], "end_turn") for _ in range(n)]
    return scripted(steps + [([text("summary of the session so far")], "end_turn")])


def test_the_compact_event_records_usage_and_provenance(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None, token_threshold=500),
        FakeAsyncAnthropic(responder=_big_turn(3)),
        state_store=store,
    )
    session = manager.create()
    for _ in range(4):
        asyncio.run(session.run("keep working"))

    events = store.load_events(session.id)
    autos = [e for e in events
             if e.get("type") == "compact" and e.get("kind") == "auto"]
    assert autos, "no auto-compaction fired; raise the padding or lower threshold"
    event = autos[0]
    assert event["replaced_messages"] >= 1
    assert event["replaced_tokens_estimate"] > 0
    # The summary's own generation cost is recorded, separately from the
    # compacted content's estimate.
    assert "summary_input_tokens" in event
    assert "summary_output_tokens" in event
    assert event["summary_model"]
    store.close()


def test_recovery_needs_only_the_durable_log_not_live_memory(tmp_path):
    """After a compaction, a SECOND process restores from disk alone and
    runs a further turn -- the recovery path depends on nothing the first
    process held in memory beyond what the log carries."""
    store = SQLiteStateStore(tmp_path / "state.db")

    def build():
        return SessionManager(
            Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                     skills_dir=SKILLS, spill_dir=None, token_threshold=500),
            FakeAsyncAnthropic(responder=_big_turn(3)),
            state_store=store,
        )

    manager = build()
    session = manager.create()
    for _ in range(4):
        asyncio.run(session.run("work"))
    # Confirm a compaction actually happened.
    assert any(
        e.get("type") == "compact" and e.get("kind") == "auto"
        for e in store.load_events(session.id)
    )
    store.release_lease(session.id, session.lease_owner)
    manager._sessions.clear()

    # A fresh process: no shared memory with the first.
    second = build()
    restored = second.restore_scheduled_session(session.id)
    answer = asyncio.run(restored.run("continue after the restart"))
    assert "[Error]" not in answer, (
        "recovery after compaction failed: it relied on memory the log "
        "did not carry"
    )
    store.close()
