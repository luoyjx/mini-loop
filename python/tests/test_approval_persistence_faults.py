"""A swallowed approval-write is reported, because silence undoes round 100.

Round 100's guarantee: a restart tells "parked, never ran" (answer NOT_RUN,
safe to retry) from "dispatched, outcome unknown" (answer UNKNOWN, do not
retry) by reading the durable approval row. Round 100 also made `_persist`
swallow exceptions -- correctly, because a persistence fault must not fail or
hang the turn. But a swallow with no signal is the failure this harness keeps
re-learning (rounds 92, 100, 104): the guarantee silently degrades to the
pre-round-100 behavior, and nobody knows.

So the broker now carries a `ProblemLog`. The write still proceeds-on-fault
(the turn is authoritative in memory), but the fault is recorded, and the
audit's generic problem-channel sweep (round 92) surfaces it with no
audit-side change -- `manager.approvals` is just another subsystem with a
`problems` attribute.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import audit
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.registry import Tool
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


class BrokenApprovalStore(SQLiteStateStore):
    """A store that persists everything except approvals."""

    def write_approval(self, record):
        raise OSError("disk full")


def _external_tool(ran):
    async def deploy(ctx, target):
        ran.append(target)
        return f"deployed {target}"

    return Tool("mcp__ops__deploy", "[mcp:ops] Deploy.",
                {"type": "object", "properties": {"target": {"type": "string"}}},
                deploy, risk="external")


def _manager(tmp_path, store):
    ran = []
    registry = full_registry()
    registry.register(_external_tool(ran))
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("mcp__ops__deploy", target="prod", _id="t1")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    manager = SessionManager(settings, client, tool_registry=registry,
                             state_store=store)
    return manager, ran


async def _wait_pending(manager, session_id, *, timeout=2.0):
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        if manager.approvals.list(session_id):
            return manager.approvals.list(session_id)
        await asyncio.sleep(0.01)
    return manager.approvals.list(session_id)


@pytest.mark.asyncio
async def test_a_persist_fault_does_not_fail_the_turn(tmp_path):
    manager, ran = _manager(tmp_path, BrokenApprovalStore(tmp_path / "s.db"))
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    pending = await _wait_pending(manager, session.id)
    assert pending, "the persistence fault crashed or hung the turn"

    manager.approvals.resolve(pending[0]["approval_id"], session_id=session.id,
                              allowed=True)
    await turn
    assert ran == ["prod"], "the tool never ran after approval"


@pytest.mark.asyncio
async def test_a_persist_fault_is_reported_not_swallowed(tmp_path):
    manager, _ = _manager(tmp_path, BrokenApprovalStore(tmp_path / "s.db"))
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    await _wait_pending(manager, session.id)

    assert manager.approvals.problems, "a swallowed persist fault left no trace"
    assert any("restores as UNKNOWN" in p for p in manager.approvals.problems)

    manager.approvals.resolve(manager.approvals.list(session.id)[0]["approval_id"],
                              session_id=session.id, allowed=False)
    await turn


@pytest.mark.asyncio
async def test_the_audit_surfaces_the_broker_fault(tmp_path):
    """No audit change was needed: the round-92 sweep finds any subsystem with
    a `problems` log. This pins that the broker is one of them."""

    manager, _ = _manager(tmp_path, BrokenApprovalStore(tmp_path / "s.db"))
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    await _wait_pending(manager, session.id)

    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}
    assert "approvals-problems" in findings
    assert "persistence failed" in findings["approvals-problems"].detail

    manager.approvals.resolve(manager.approvals.list(session.id)[0]["approval_id"],
                              session_id=session.id, allowed=False)
    await turn


@pytest.mark.asyncio
async def test_repeated_faults_dedup_to_one_line(tmp_path):
    """A persistently broken store must not flood: the message omits the id so
    the ProblemLog collapses every occurrence to one distinct entry."""

    manager, _ = _manager(tmp_path, BrokenApprovalStore(tmp_path / "s.db"))
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    pending = await _wait_pending(manager, session.id)
    # pending write + resolve write = at least two failed persists.
    manager.approvals.resolve(pending[0]["approval_id"], session_id=session.id,
                              allowed=True)
    await turn

    assert len(manager.approvals.problems) == 1
    assert manager.approvals.problems.total() >= 2


@pytest.mark.asyncio
async def test_a_healthy_store_reports_nothing(tmp_path):
    """Not vacuous: the channel stays empty when persistence works."""

    manager, _ = _manager(tmp_path, SQLiteStateStore(tmp_path / "s.db"))
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    pending = await _wait_pending(manager, session.id)
    manager.approvals.resolve(pending[0]["approval_id"], session_id=session.id,
                              allowed=True)
    await turn

    assert not manager.approvals.problems
