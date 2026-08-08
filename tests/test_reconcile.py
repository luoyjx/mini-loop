"""Reconciling an action whose outcome a crash left unknown.

The harness already refused to retry an `unknown` action -- correct, and it
leaves the agent stuck on a question it cannot answer. A tool that can check
whether its own side effect landed turns that into an answerable one.

Three outcomes, and only one of them permits a re-run:

* it already landed      -> record it, do not run it again
* it provably did not    -> safe to run
* cannot tell            -> still unknown, still refuse

The asymmetry is the point: `write_file` is a typed call, so the request itself
says what should be on disk. `bash` is an opaque string and gets no verifier,
which is the concrete argument for promoting a side effect out of it.
"""

import asyncio
from pathlib import Path

from mini_loop.actions import RECONCILED_RESULT, UNKNOWN_RESULT, DurableActionJournal
from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import Hooks, Tool, ToolCall, ToolRegistry
from mini_loop.run_context import RunContext
from mini_loop.skills import SkillLoader
from mini_loop.storage import SQLiteStateStore

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, journal, registry, events=None):
    workspace = tmp_path / "ws"
    workspace.mkdir(exist_ok=True)
    async def emit(event):
        if events is not None:
            events.append(event)
    return Agent(
        client=FakeAsyncAnthropic(),
        settings=Settings(
            fake_llm=True, workspace_root=tmp_path / "root", skills_dir=SKILLS_DIR
        ),
        workspace=workspace,
        skills=SkillLoader(SKILLS_DIR),
        tools=registry,
        hooks=Hooks(),
        state={"session_id": "s1", "action_journal": journal},
        emit=emit,
    )


def _charging_registry(verify=None):
    runs = {"n": 0}

    async def charge(_ctx, amount="100"):
        runs["n"] += 1
        return f"charged {amount} (run {runs['n']})"

    registry = ToolRegistry()
    registry.register(
        Tool(
            "charge",
            "charge the customer",
            {"type": "object", "properties": {"amount": {"type": "string"}}},
            charge,
            verify=verify,
        )
    )
    return registry, runs


def _strand(agent, journal, call, context):
    """Run the call, then reopen it as an action nobody accounted for."""
    asyncio.run(agent._exec_tool(call, run_context=context))
    row = journal.store._db.execute("SELECT action_id FROM actions").fetchone()
    journal.store.write_action({**journal.store.read_action(row["action_id"]), "status": "started"})
    journal.mark_inflight_unknown()
    return row["action_id"]


# --- the three verdicts -----------------------------------------------------

def test_a_verified_landing_is_recorded_not_repeated(tmp_path):
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))

    async def landed(_ctx, _call):
        return True

    registry, runs = _charging_registry(verify=landed)
    agent = _agent(tmp_path, journal, registry)
    context = RunContext.default()
    action_id = _strand(agent, journal, ToolCall("charge", {}, "t1"), context)

    out = asyncio.run(agent._exec_tool(ToolCall("charge", {}, "t1"), run_context=context))
    assert runs["n"] == 1, "a confirmed side effect ran a second time"
    assert out == RECONCILED_RESULT
    # The journal now knows, so a third attempt is a plain replay.
    assert journal.get(action_id).status == "completed"
    journal.store.close()


def test_a_verified_non_landing_is_the_only_case_that_re_runs(tmp_path):
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))

    async def did_not_land(_ctx, _call):
        return False

    registry, runs = _charging_registry(verify=did_not_land)
    agent = _agent(tmp_path, journal, registry)
    context = RunContext.default()
    _strand(agent, journal, ToolCall("charge", {}, "t1"), context)

    out = asyncio.run(agent._exec_tool(ToolCall("charge", {}, "t1"), run_context=context))
    assert runs["n"] == 2, "a provably-unapplied action was not retried"
    assert "charged" in out
    journal.store.close()


def test_without_a_verifier_the_answer_stays_unknown(tmp_path):
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))
    registry, runs = _charging_registry(verify=None)
    agent = _agent(tmp_path, journal, registry)
    context = RunContext.default()
    _strand(agent, journal, ToolCall("charge", {}, "t1"), context)

    out = asyncio.run(agent._exec_tool(ToolCall("charge", {}, "t1"), run_context=context))
    assert runs["n"] == 1
    assert out == UNKNOWN_RESULT
    journal.store.close()


def test_a_verifier_that_cannot_tell_leaves_it_unknown(tmp_path):
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))

    async def shrug(_ctx, _call):
        return None

    registry, runs = _charging_registry(verify=shrug)
    agent = _agent(tmp_path, journal, registry)
    context = RunContext.default()
    _strand(agent, journal, ToolCall("charge", {}, "t1"), context)

    assert asyncio.run(
        agent._exec_tool(ToolCall("charge", {}, "t1"), run_context=context)
    ) == UNKNOWN_RESULT
    assert runs["n"] == 1
    journal.store.close()


def test_a_broken_verifier_must_not_become_a_no(tmp_path):
    """Failing to check is not evidence that nothing happened."""
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))

    async def explode(_ctx, _call):
        raise RuntimeError("the billing API is down")

    registry, runs = _charging_registry(verify=explode)
    agent = _agent(tmp_path, journal, registry)
    context = RunContext.default()
    _strand(agent, journal, ToolCall("charge", {}, "t1"), context)

    assert asyncio.run(
        agent._exec_tool(ToolCall("charge", {}, "t1"), run_context=context)
    ) == UNKNOWN_RESULT
    assert runs["n"] == 1, "a broken verifier caused a retry"
    journal.store.close()


def test_the_verdict_is_reported(tmp_path):
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))

    async def landed(_ctx, _call):
        return True

    registry, _ = _charging_registry(verify=landed)
    events: list[dict] = []
    agent = _agent(tmp_path, journal, registry, events=events)
    context = RunContext.default()
    _strand(agent, journal, ToolCall("charge", {}, "t1"), context)
    asyncio.run(agent._exec_tool(ToolCall("charge", {}, "t1"), run_context=context))

    reconciles = [e for e in events if e["type"] == "reconcile"]
    assert reconciles and reconciles[-1]["verdict"] == "already_applied"
    assert reconciles[-1]["verifiable"] is True
    journal.store.close()


# --- the built-in verifier --------------------------------------------------

def test_write_file_can_answer_for_itself(tmp_path):
    """A typed call carries what it intended; an opaque command does not."""
    from mini_loop.builtins import default_registry

    registry = default_registry()
    assert registry.get("write_file").verify is not None
    assert registry.get("bash").verify is None, (
        "a shell string cannot be reconciled; claiming otherwise would be worse "
        "than admitting it"
    )

    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))
    agent = _agent(tmp_path, journal, registry)
    context = RunContext.default()
    call = ToolCall("write_file", {"path": "note.txt", "content": "hello"}, "t1")
    _strand(agent, journal, call, context)

    # The file is there with exactly this content -> already applied.
    out = asyncio.run(agent._exec_tool(call, run_context=context))
    assert out == RECONCILED_RESULT

    # Remove it: now provably not applied, so the retry is allowed.
    (agent.workspace / "note.txt").unlink()
    journal.store.write_action(
        {**journal.store.read_action(
            journal.store._db.execute("SELECT action_id FROM actions").fetchone()["action_id"]
        ), "status": "started"}
    )
    journal.mark_inflight_unknown()
    asyncio.run(agent._exec_tool(call, run_context=context))
    assert (agent.workspace / "note.txt").read_text() == "hello"
    journal.store.close()


def test_reconcile_is_the_only_transition_out_of_unknown(tmp_path):
    """`finish()` stays strict; loosening it would make terminals rewritable."""
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "state.db"))
    journal.begin(
        action_id="a1", session_id="s1", message_id="m1", tool_use_id="t1",
        tool_name="charge", input_value={},
    )
    journal.finish("a1", status="completed", result="first")

    # A terminal record is not reconcilable -- it is already settled.
    assert journal.reconcile("a1", status="failed", result="second").result == "first"

    journal.begin(
        action_id="a2", session_id="s1", message_id="m1", tool_use_id="t2",
        tool_name="charge", input_value={},
    )
    journal.mark_inflight_unknown()
    assert journal.get("a2").status == "unknown"
    # `finish` refuses; `reconcile` is the sanctioned path.
    assert journal.finish("a2", status="completed", result="x").status == "unknown"
    assert journal.reconcile("a2", status="completed", result="y").status == "completed"
    journal.store.close()
