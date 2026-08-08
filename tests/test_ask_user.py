"""The model can ask the human a question and wait for the answer.

OpenWorker's `ask_user` (research doc 3.1, human-in-the-loop) lets the model
pause and ask instead of guessing. Ours had no such seam: a model missing one
fact could only assume or hardcode. The approval broker (rounds 96/100) is
already the right machinery -- park, list, resolve over REST, expire on
restore -- and a question is just an approval whose answer has words. So
`ask_user` rides it whole, distinguished by `kind="question"`; `resolve`
carries free text instead of a boolean.

The distinction that keeps it honest: an approval's answer is allow/deny; a
question's is text or a decline. One code path, two shapes, each with the
outcome the model actually needs -- the recurring lesson (rounds 96/100) that
two different answers must not be collapsed into one value.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.actions import NOT_RUN_RESULT
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _asking_client(question="Which environment, staging or prod?"):
    return FakeAsyncAnthropic(responder=scripted([
        ([tool("ask_user", question=question, _id="q1")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))


def _manager(tmp_path, client, store=None):
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return SessionManager(settings, client, tool_registry=full_registry(),
                          **({"state_store": store} if store else {}))


async def _wait_pending(manager, session_id, *, timeout=2.0):
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        if manager.approvals.list(session_id):
            return manager.approvals.list(session_id)
        await asyncio.sleep(0.01)
    return manager.approvals.list(session_id)


def _results(session):
    return [
        block["content"]
        for message in session.agent.messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and block.get("type") == "tool_result"
    ]


@pytest.mark.asyncio
async def test_the_answer_reaches_the_model(tmp_path):
    manager = _manager(tmp_path, _asking_client())
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    [pending] = await _wait_pending(manager, session.id)
    assert pending["kind"] == "question"
    assert "environment" in pending["message"]

    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=True, answer="staging")
    await turn

    [result] = _results(session)
    assert "staging" in result and "user answered" in result.lower()


@pytest.mark.asyncio
async def test_a_declined_question_tells_the_model_to_proceed(tmp_path):
    manager = _manager(tmp_path, _asking_client())
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    [pending] = await _wait_pending(manager, session.id)
    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=False)
    await turn

    [result] = _results(session)
    assert "no answer" in result.lower()
    assert "best judgment" in result.lower()


@pytest.mark.asyncio
async def test_an_unanswered_question_times_out_to_proceed(tmp_path):
    manager = _manager(tmp_path, _asking_client())
    manager.approvals.timeout = 0.05
    session = manager.create()

    await asyncio.wait_for(session.run("deploy"), timeout=5)

    [result] = _results(session)
    assert "no answer" in result.lower()


@pytest.mark.asyncio
async def test_a_readonly_session_may_still_ask(tmp_path):
    """A question mutates nothing; readonly mode must not deny it."""

    manager = _manager(tmp_path, _asking_client())
    session = manager.create(permission_mode="readonly")

    turn = asyncio.create_task(session.run("deploy"))
    pending = await _wait_pending(manager, session.id)
    assert pending, "readonly mode denied a question that mutates nothing"
    manager.approvals.resolve(pending[0]["approval_id"], session_id=session.id,
                              allowed=True, answer="staging")
    await turn
    assert "staging" in _results(session)[0]


@pytest.mark.asyncio
async def test_the_answer_is_persisted_on_the_row(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, _asking_client(), store=store)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    [pending] = await _wait_pending(manager, session.id)
    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=True, answer="staging")
    await turn

    [row] = store.read_approvals(session.id)
    assert row["kind"] == "question"
    assert row["status"] == "answered"
    assert row["answer"] == "staging"
    store.close()


@pytest.mark.asyncio
async def test_a_question_parked_at_restart_is_not_run(tmp_path):
    """A crash with an unanswered question restores like any parked call: the
    tool_use never ran, so it is answered NOT_RUN, not UNKNOWN."""

    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages("s-crash", [
        {"role": "user", "content": "deploy"},
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": "q-parked", "name": "ask_user",
             "input": {"question": "which env?"}}]},
    ], epoch=1)
    store.write_approval({
        "approval_id": "apr_q0001", "session_id": "s-crash",
        "tool_use_id": "q-parked", "tool_name": "ask_user", "rule": "ask-user",
        "message": "which env?", "input_preview": "", "status": "pending",
        "created_at": 0.0, "resolved_at": None, "kind": "question", "answer": None,
    })
    from mini_loop.storage import SessionRecord
    store.upsert_session(SessionRecord(
        session_id="s-crash", workspace=str(tmp_path / "ws" / "s-crash"),
        system=None, created_at=0.0, run_count=1, status="idle", todos=[],
        event_cursor=0,
    ))

    manager = _manager(tmp_path, FakeAsyncAnthropic(), store=store)
    session = next(s for s in manager.restore_sessions() if s.id == "s-crash")

    [result] = _results(session)
    assert result == NOT_RUN_RESULT
    assert store.read_approvals("s-crash")[0]["status"] == "expired"
    store.close()
