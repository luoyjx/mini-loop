"""The action journal as a replay guard, not an audit log.

`begin()` already returned the existing record on replay -- and `_exec_tool`
discarded it and executed anyway, so the journal recorded side effects without
preventing a second one. The properties below come from durable-execution
engines (Temporal, Restate, Azure Durable Task): a journalled step is not
re-executed, and a step that was dispatched but never accounted for is
*unknown* rather than failed.
"""

import asyncio
from pathlib import Path

import pytest

from mini_loop.actions import (
    UNKNOWN_RESULT,
    ActionJournalConflict,
    DurableActionJournal,
    InMemoryActionJournal,
)
from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import Hooks, Tool, ToolCall, ToolRegistry
from mini_loop.run_context import RunContext
from mini_loop.skills import SkillLoader
from mini_loop.storage import SQLiteStateStore

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _journal(tmp_path):
    return DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))


def _begin(journal, action_id="a1", **over):
    payload = dict(
        action_id=action_id,
        session_id="s1",
        message_id="m1",
        tool_use_id="tu_1",
        tool_name="bash",
        input_value={"command": "charge"},
    )
    payload.update(over)
    return journal.begin(**payload)


# --- the journal ------------------------------------------------------------

def test_a_record_survives_the_process_that_wrote_it(tmp_path):
    db = tmp_path / "state.db"
    store = SQLiteStateStore(db)
    DurableActionJournal(store).begin(
        action_id="a1", session_id="s1", message_id="m1", tool_use_id="tu_1",
        tool_name="bash", input_value={"command": "charge"},
    )
    store.close()

    store = SQLiteStateStore(db)
    record = DurableActionJournal(store).get("a1")
    assert record is not None and record.status == "started"
    store.close()


def test_replaying_with_a_different_payload_is_a_conflict(tmp_path):
    journal = _journal(tmp_path)
    _begin(journal)
    with pytest.raises(ActionJournalConflict):
        _begin(journal, input_value={"command": "charge twice"})
    journal.store.close()


def test_in_flight_actions_become_unknown_not_failed(tmp_path):
    """`failed` invites a retry; the outcome is genuinely not known."""
    journal = _journal(tmp_path)
    _begin(journal, "a1")
    _begin(journal, "a2")
    journal.finish("a2", status="completed", result="ok")

    assert journal.mark_inflight_unknown() == ["a1"]
    assert journal.get("a1").status == "unknown"
    assert journal.get("a2").status == "completed", "terminal records are untouched"
    journal.store.close()


def test_marking_unknown_is_idempotent(tmp_path):
    journal = _journal(tmp_path)
    _begin(journal)
    assert journal.mark_inflight_unknown() == ["a1"]
    assert journal.mark_inflight_unknown() == []
    journal.store.close()


def test_unknown_can_be_scoped_to_one_session(tmp_path):
    journal = _journal(tmp_path)
    _begin(journal, "a1", session_id="s1")
    _begin(journal, "a2", session_id="s2")
    assert journal.mark_inflight_unknown("s1") == ["a1"]
    assert journal.get("a2").status == "started"
    journal.store.close()


def test_the_durable_and_memory_journals_agree(tmp_path):
    """One interface, two backings -- divergence here is a latent bug."""
    for journal in (_journal(tmp_path), InMemoryActionJournal()):
        record = _begin(journal)
        assert record.status == "started"
        assert _begin(journal).status == "started", "replay must return the record"
        finished = journal.finish("a1", status="completed", result="ok")
        assert finished.status == "completed"
        # A second finish is a no-op, not an error.
        assert journal.finish("a1", status="failed").status == "completed"


# --- the guard, through the loop -------------------------------------------

def _counting_agent(tmp_path, journal):
    runs = {"n": 0}

    async def charge(_ctx):
        runs["n"] += 1
        return f"charged #{runs['n']}"

    registry = ToolRegistry()
    registry.register(Tool("charge", "charge once", {"type": "object", "properties": {}}, charge))

    agent = Agent(
        client=FakeAsyncAnthropic(),
        settings=Settings(
            fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR
        ),
        workspace=tmp_path / "ws",
        skills=SkillLoader(SKILLS_DIR),
        tools=registry,
        hooks=Hooks(),
        state={"session_id": "s1", "action_journal": journal},
    )
    return agent, runs


def _call():
    return ToolCall("charge", {}, "tu_1")


def test_a_recorded_action_is_not_executed_twice(tmp_path):
    """The property the journal existed for but did not provide."""
    journal = _journal(tmp_path)
    agent, runs = _counting_agent(tmp_path, journal)
    context = RunContext.default()

    first = asyncio.run(agent._exec_tool(_call(), run_context=context))
    assert runs["n"] == 1
    assert "charged #1" in first

    # Same session, same message, same tool_use id -> same action id.
    second = asyncio.run(agent._exec_tool(_call(), run_context=context))
    assert runs["n"] == 1, "the side effect ran a second time"
    assert "charged #1" in second, "the recorded result was not returned"
    journal.store.close()


def _only_action_id(journal) -> str:
    rows = journal.store._db.execute("SELECT action_id FROM actions").fetchall()
    assert len(rows) == 1, f"expected one action, found {len(rows)}"
    return rows[0]["action_id"]


def test_an_unknown_action_reports_itself_instead_of_re_running(tmp_path):
    """A dead process dispatched it; nobody knows whether it landed."""
    journal = _journal(tmp_path)
    agent, runs = _counting_agent(tmp_path, journal)
    context = RunContext.default()

    asyncio.run(agent._exec_tool(_call(), run_context=context))
    assert runs["n"] == 1

    # Reopen the record as in-flight, then do what a restart does.
    action_id = _only_action_id(journal)
    journal.store.write_action(
        {**journal.store.read_action(action_id), "status": "started"}
    )
    assert journal.mark_inflight_unknown() == [action_id]

    out = asyncio.run(agent._exec_tool(_call(), run_context=context))
    assert runs["n"] == 1, "an unknown action must not be retried"
    assert "[unknown]" in out
    assert "Do not retry" in out
    assert out == UNKNOWN_RESULT
    journal.store.close()


def test_a_different_message_is_a_different_action(tmp_path):
    """Idempotency must not collapse two genuinely separate requests."""
    journal = _journal(tmp_path)
    agent, runs = _counting_agent(tmp_path, journal)

    asyncio.run(agent._exec_tool(_call(), run_context=RunContext.default()))
    asyncio.run(agent._exec_tool(_call(), run_context=RunContext.default()))
    assert runs["n"] == 2
    journal.store.close()
