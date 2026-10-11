"""Every ask leaves a durable row, and a restart knows what never ran.

Round 96's broker was stated non-durable: a restart lost pending approvals
and the turns parked on them. Worse than the loss was the mislabeling that
followed: restore answered every dangling tool_use with UNKNOWN_RESULT --
"do not retry; check whether it took effect" -- advice that is exactly wrong
for a call that was parked on an approval, because a parked call never
reached its handler and retrying it is safe. Two different absences, one
value, every consumer invited to pick the wrong one: the same type error
round 96 fixed with `_MISSING`, one layer up.

With the broker persisting each ask (pending -> allowed / denied / timeout /
cancelled / expired), a restart can tell the two apart: pending rows whose
process died are expired, and their tool_use ids are answered NOT_RUN_RESULT
instead of UNKNOWN_RESULT. What this round deliberately does not do: resume
the parked turn or execute on a late approval -- the row is the truth of what
was asked, not a replay journal.
"""

import asyncio
import json
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.actions import NOT_RUN_RESULT, UNKNOWN_RESULT
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.registry import Tool
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _external_tool(ran):
    async def deploy(ctx, target):
        ran.append(target)
        return f"deployed {target}"

    return Tool("mcp__ops__deploy", "[mcp:ops] Deploy a target.",
                {"type": "object", "properties": {"target": {"type": "string"}}},
                deploy, risk="external")


def _manager(tmp_path, store, *, ran=None, calls=()):
    registry = full_registry()
    registry.register(_external_tool(ran if ran is not None else []))
    client = FakeAsyncAnthropic(responder=scripted(
        [(blocks, "tool_use") for blocks in calls] + [([text("done")], "end_turn")]
    ))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return SessionManager(settings, client, tool_registry=registry,
                          state_store=store)


async def _wait_for_pending(manager, session_id, *, timeout=2.0):
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        if manager.approvals.list(session_id):
            return manager.approvals.list(session_id)
        await asyncio.sleep(0.01)
    return manager.approvals.list(session_id)


# -- the audit trail --------------------------------------------------------


@pytest.mark.asyncio
async def test_every_ask_leaves_a_row_and_every_outcome_updates_it(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store,
                       calls=[[tool("mcp__ops__deploy", target="prod", _id="tu1")]])
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    [pending] = await _wait_for_pending(manager, session.id)

    [row] = store.read_approvals(session.id)
    assert (row["status"], row["tool_use_id"]) == ("pending", "tu1")

    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=True)
    await turn

    [row] = store.read_approvals(session.id)
    assert row["status"] == "allowed"
    assert row["resolved_at"] is not None
    store.close()


@pytest.mark.asyncio
async def test_a_timeout_is_recorded_as_timeout_not_denied(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store,
                       calls=[[tool("mcp__ops__deploy", target="prod", _id="tu1")]])
    manager.approvals.timeout = 0.05
    session = manager.create()

    await asyncio.wait_for(session.run("deploy"), timeout=5)

    [row] = store.read_approvals(session.id)
    assert row["status"] == "timeout"
    store.close()


@pytest.mark.asyncio
async def test_a_json_escaping_secret_in_a_tool_argument_is_masked_in_the_preview(tmp_path):
    """`input_preview` is the durable record of what a tool was asked to do, and
    a model can write a credential straight into a tool argument -- which is why
    the secrets registry masks arguments, not only results.

    The preview was `json.dumps(call.input)` and then `mask()` -- the
    serialize-then-mask order round 120 found leaking in `teams` and `tasks`.
    `json.dumps` escapes a non-ASCII secret to `\\uXXXX`, so the raw-bytes mask
    slid past it into this durable row and the SSE event a human reads. This
    pins the fix at the approval flow's own durable sink; the ASCII case masked
    fine before and after, so the escaping secret is what tells the two orders
    apart.
    """
    secret = "clé-café-secret-Ω-0123456789"
    secrets = SecretRegistry()
    secrets.register("DEPLOY_KEY", secret)

    store = SQLiteStateStore(tmp_path / "state.db")
    registry = full_registry()
    registry.register(_external_tool([]))
    client = FakeAsyncAnthropic(responder=scripted(
        [([tool("mcp__ops__deploy", target=secret, _id="tu1")], "tool_use"),
         ([text("done")], "end_turn")]
    ))
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        client, tool_registry=registry, state_store=store, secrets=secrets,
    )
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    [pending] = await _wait_for_pending(manager, session.id)

    [row] = store.read_approvals(session.id)
    preview = row["input_preview"]
    escaped = json.dumps(secret)[1:-1]
    assert secret not in preview and escaped not in preview, (
        f"a json-escaped secret reached the durable approval preview: {preview!r}"
    )
    assert "<secret-hidden>" in preview

    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=True)
    await turn
    store.close()


@pytest.mark.asyncio
async def test_a_session_delete_is_recorded_as_cancelled(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store,
                       calls=[[tool("mcp__ops__deploy", target="prod", _id="tu1")]])
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    await _wait_for_pending(manager, session.id)
    manager.delete(session.id)
    await asyncio.wait_for(turn, timeout=5)

    [row] = store.read_approvals(session.id)
    assert row["status"] == "cancelled"
    store.close()


# -- what a restart knows ---------------------------------------------------


def _crashed_store(tmp_path, *, with_approval):
    """A store as a crash leaves it: dangling tool_use, optional pending row."""

    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages("s-crashed", [
        {"role": "user", "content": "deploy please"},
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": "tu-parked", "name": "mcp__ops__deploy",
             "input": {"target": "prod"}}]},
    ], epoch=1)
    if with_approval:
        store.write_approval({
            "approval_id": "apr_crashed00001", "session_id": "s-crashed",
            "tool_use_id": "tu-parked", "tool_name": "mcp__ops__deploy",
            "rule": "external-action", "message": "Tool acts outside this machine",
            "input_preview": '{"target": "prod"}', "status": "pending",
            "created_at": 0.0, "resolved_at": None,
        })
    from mini_loop.storage import SessionRecord

    store.upsert_session(SessionRecord(
        session_id="s-crashed", workspace=str(tmp_path / "ws" / "s-crashed"),
        system=None, created_at=0.0, run_count=1, status="idle", todos=[],
        event_cursor=0,
    ))
    return store


def _restored_result(manager, session_id):
    session = next(s for s in manager.restore_sessions() if s.id == session_id)
    return [
        part["content"]
        for message in session.agent.messages
        if isinstance(message.get("content"), list)
        for part in message["content"]
        if isinstance(part, dict) and part.get("type") == "tool_result"
    ]


def test_a_parked_call_restores_as_not_run(tmp_path):
    store = _crashed_store(tmp_path, with_approval=True)
    manager = _manager(tmp_path, store)

    [result] = _restored_result(manager, "s-crashed")
    assert result == NOT_RUN_RESULT, (
        "a call that never ran was answered with the do-not-retry advice"
    )
    [row] = store.read_approvals("s-crashed")
    assert row["status"] == "expired"
    store.close()


def test_a_dispatched_call_still_restores_as_unknown(tmp_path):
    """The distinction is the point: without a pending approval the old,
    correct advice stands -- the tool may have run, do not retry."""

    store = _crashed_store(tmp_path, with_approval=False)
    manager = _manager(tmp_path, store)

    [result] = _restored_result(manager, "s-crashed")
    assert result == UNKNOWN_RESULT
    store.close()
