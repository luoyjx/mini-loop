"""A pending approval is an object someone can answer, not a dead end.

Round 95 made `external` tools ask before acting. On a server nothing could
answer, so every ask was a deny -- safe, and unusable: a gate nobody can open
is a wall, and walls get torn down. Following OpenWorker's Inbox principle
(OPENWORKER_RESEARCH.md section 11.1), the question is now an object: the turn
parks on it, `GET /sessions/{id}/approvals` shows it, one POST answers it,
and every unanswered path -- timeout, session delete, manager stop -- ends in
deny.

Not yet durable: a restart loses pending questions. That gap is stated in
approvals.py and is a future round, not an accident.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.registry import Tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _external_tool(ran):
    async def deploy(ctx, target):
        ran.append(target)
        return f"deployed {target}"

    return Tool("mcp__ops__deploy", "[mcp:ops] Deploy a target.",
                {"type": "object", "properties": {"target": {"type": "string"}}},
                deploy, risk="external")


def _manager(tmp_path, ran, **settings_over):
    registry = full_registry()
    registry.register(_external_tool(ran))
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("mcp__ops__deploy", target="prod")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, **settings_over)
    return SessionManager(settings, client, tool_registry=registry)


async def _wait_for_pending(manager, session_id, *, timeout=2.0):
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        pending = manager.approvals.list(session_id)
        if pending:
            return pending
        await asyncio.sleep(0.01)
    return manager.approvals.list(session_id)


# -- ask -> answer -> act ---------------------------------------------------


@pytest.mark.asyncio
async def test_an_allowed_approval_lets_the_parked_turn_act(tmp_path):
    ran = []
    manager = _manager(tmp_path, ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy prod"))
    [pending] = await _wait_for_pending(manager, session.id)
    assert pending["tool"] == "mcp__ops__deploy"
    assert "prod" in pending["input_preview"]
    assert not ran, "the tool ran before anyone answered"

    assert manager.approvals.resolve(pending["approval_id"],
                                     session_id=session.id, allowed=True)
    await turn
    assert ran == ["prod"]
    assert manager.approvals.list(session.id) == []


@pytest.mark.asyncio
async def test_a_denied_approval_keeps_the_tool_unrun(tmp_path):
    ran = []
    manager = _manager(tmp_path, ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy prod"))
    [pending] = await _wait_for_pending(manager, session.id)
    manager.approvals.resolve(pending["approval_id"],
                              session_id=session.id, allowed=False)
    await turn

    assert not ran
    transcript = str(session.agent.messages)
    assert "Permission denied" in transcript


@pytest.mark.asyncio
async def test_nobody_answering_is_a_deny_not_a_hang(tmp_path):
    ran = []
    manager = _manager(tmp_path, ran, approval_timeout=0.05)
    session = manager.create()

    await asyncio.wait_for(session.run("deploy prod"), timeout=5)

    assert not ran, "an unanswered approval fell through to allow"
    assert manager.approvals.list(session.id) == []


# -- the tenancy rule -------------------------------------------------------


@pytest.mark.asyncio
async def test_a_foreign_approval_id_behaves_like_a_missing_one(tmp_path):
    ran = []
    manager = _manager(tmp_path, ran)
    session = manager.create()
    other = manager.create()

    turn = asyncio.create_task(session.run("deploy prod"))
    [pending] = await _wait_for_pending(manager, session.id)

    assert not manager.approvals.resolve(pending["approval_id"],
                                         session_id=other.id, allowed=True)
    assert not ran
    manager.approvals.resolve(pending["approval_id"],
                              session_id=session.id, allowed=False)
    await turn


@pytest.mark.asyncio
async def test_resolving_twice_is_a_no_op(tmp_path):
    ran = []
    manager = _manager(tmp_path, ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy prod"))
    [pending] = await _wait_for_pending(manager, session.id)
    assert manager.approvals.resolve(pending["approval_id"],
                                     session_id=session.id, allowed=False)
    assert not manager.approvals.resolve(pending["approval_id"],
                                         session_id=session.id, allowed=True)
    await turn
    assert not ran


# -- reclamation (round 94's rule, applied here) ----------------------------


@pytest.mark.asyncio
async def test_deleting_the_session_denies_its_pending_approvals(tmp_path):
    ran = []
    manager = _manager(tmp_path, ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy prod"))
    await _wait_for_pending(manager, session.id)
    manager.delete(session.id)
    await asyncio.wait_for(turn, timeout=5)

    assert not ran
    assert manager.approvals.list(session.id) == []


@pytest.mark.asyncio
async def test_stop_denies_every_pending_approval(tmp_path):
    ran = []
    manager = _manager(tmp_path, ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy prod"))
    await _wait_for_pending(manager, session.id)
    await manager.stop()
    await asyncio.wait_for(turn, timeout=5)

    assert not ran


@pytest.mark.asyncio
async def test_a_missing_tool_is_dispatchs_problem_not_an_approval(tmp_path):
    """A call to a tool that does not exist must not park the turn: there is
    nothing to approve, and the model needs the "unknown tool" answer now.
    Found as a 300-second stall: missing collapsed into unclassified, and the
    turn waited for a human to authorize a tool with no handler."""

    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("no_such_tool", x=1)], "tool_use"),
        ([text("ok")], "end_turn"),
    ]))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    manager = SessionManager(settings, client, tool_registry=full_registry())
    session = manager.create()

    await asyncio.wait_for(session.run("try it"), timeout=5)

    assert manager.approvals.list(session.id) == []
    transcript = str(session.agent.messages)
    assert "no_such_tool" in transcript


# -- the HTTP surface -------------------------------------------------------


def _seed_pending(manager, session_id):
    """Park a synthetic approval on the broker, no live turn required."""

    import time as _time

    from mini_loop.approvals import PendingApproval

    pending = PendingApproval(
        approval_id="apr_seeded000001", session_id=session_id,
        tool="mcp__ops__deploy", rule="external-action",
        message="Tool acts outside this machine",
        input_preview='{"target": "prod"}', created_at=_time.time(),
        future=asyncio.new_event_loop().create_future(),
    )
    manager.approvals._pending[pending.approval_id] = pending
    return pending


def test_approvals_over_http_are_owner_scoped(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    alice = "tok-alice-000000000000"
    bob = "tok-bob-1111111111111"
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({alice: "alice", bob: "bob"})
        headers = {"Authorization": f"Bearer {alice}"}
        session_id = client.post("/sessions", json={}, headers=headers).json()["id"]
        pending = _seed_pending(manager, session_id)

        listed = client.get(f"/sessions/{session_id}/approvals", headers=headers)
        assert listed.status_code == 200
        assert [p["approval_id"] for p in listed.json()["approvals"]] == [pending.approval_id]

        # Bob owns no such session: list and resolve both read as missing.
        foreign = {"Authorization": f"Bearer {bob}"}
        assert client.get(f"/sessions/{session_id}/approvals",
                          headers=foreign).status_code == 404
        assert client.post(f"/sessions/{session_id}/approvals/{pending.approval_id}",
                           json={"decision": "allow"}, headers=foreign).status_code == 404
        assert not pending.future.done(), "a stranger resolved the approval"

        resolved = client.post(f"/sessions/{session_id}/approvals/{pending.approval_id}",
                               json={"decision": "allow"}, headers=headers)
        assert resolved.status_code == 200
        assert pending.future.result() is True
        # Answered means gone: a second answer reads as missing.
        assert client.post(f"/sessions/{session_id}/approvals/{pending.approval_id}",
                           json={"decision": "deny"}, headers=headers).status_code == 404
