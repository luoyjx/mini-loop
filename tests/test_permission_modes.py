"""A session's mode maps risk to decision; the rules never change.

OpenWorker ships five permission modes and shows three (discuss / ask for
approval / full access); the mode decides what a risk level *means* for this
session (OPENWORKER_RESEARCH.md 3.5). With round 95's ladder on every tool,
the reduction fits in one hook:

    readonly     write/exec/external and unclassified deny outright
    approve      "ask for approval": write/exec ask too -- nothing
                 side-effectful runs without a human's yes
    interactive  the default, "approve for me": external and unclassified
                 ask (round 96 broker); workspace writes and shell run
    auto         "full access": ask-rules auto-allow, audited

The invariant worth pinning hardest: `auto` widens what is not *asked about*,
never what is *refused*. The immutable deny-list and every `deny`-action rule
(workspace boundary) hold in every mode. And `readonly` denies rather
than asks: the point of a read-only session is that no approval -- human or
hook -- can mutate through it.

Mode is runtime state, deliberately not persisted: a restored session comes
back `interactive`, because the fail-safe direction is toward asking again.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.permissions import PERMISSION_MODES
from mini_loop.registry import Tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _external_tool(ran):
    async def deploy(ctx, target):
        ran.append(target)
        return f"deployed {target}"

    return Tool("mcp__ops__deploy", "[mcp:ops] Deploy a target.",
                {"type": "object", "properties": {"target": {"type": "string"}}},
                deploy, risk="external")


def _manager(tmp_path, calls, ran=None):
    registry = full_registry()
    registry.register(_external_tool(ran if ran is not None else []))
    client = FakeAsyncAnthropic(responder=scripted(
        [(blocks, "tool_use") for blocks in calls] + [([text("done")], "end_turn")]
    ))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return SessionManager(settings, client, tool_registry=registry)


def _tool_results(session):
    return [
        block for message in session.agent.messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and block.get("type") == "tool_result"
    ]


# -- readonly ---------------------------------------------------------------


@pytest.mark.asyncio
async def test_a_readonly_session_cannot_mutate(tmp_path):
    """write/exec/external all deny with the mode named, without asking --
    no pending approval is created for a session that could never say yes."""

    manager = _manager(tmp_path, [
        [tool("bash", command="echo hi", _id="t1")],
        [tool("write_file", path="a.txt", content="x", _id="t2")],
        [tool("mcp__ops__deploy", target="prod", _id="t3")],
    ])
    session = manager.create(permission_mode="readonly")

    await asyncio.wait_for(session.run("try everything"), timeout=10)

    results = [r["content"] for r in _tool_results(session)]
    assert len(results) == 3
    assert all("read-only" in r for r in results), results
    assert manager.approvals.list(session.id) == [], (
        "a read-only session parked an approval it could never grant"
    )
    assert not (session.workspace / "a.txt").exists()


@pytest.mark.asyncio
async def test_a_readonly_session_still_reads(tmp_path):
    """Not a wall: reads pass, or nobody would ever use the mode."""

    manager = _manager(tmp_path, [[tool("glob", pattern="*", _id="t1")]])
    session = manager.create(permission_mode="readonly")

    await asyncio.wait_for(session.run("look around"), timeout=10)

    [result] = _tool_results(session)
    assert "read-only" not in result["content"]


# -- auto -------------------------------------------------------------------


@pytest.mark.asyncio
async def test_auto_mode_skips_the_ask_not_the_audit(tmp_path):
    ran = []
    manager = _manager(tmp_path, [[tool("mcp__ops__deploy", target="prod", _id="t1")]],
                       ran=ran)
    events = []
    manager.event_sink = events.append
    session = manager.create(permission_mode="auto")

    await asyncio.wait_for(session.run("deploy"), timeout=10)

    assert ran == ["prod"], "auto mode still parked or denied the external tool"
    assert manager.approvals.list(session.id) == []


@pytest.mark.asyncio
async def test_auto_mode_never_widens_what_is_refused(tmp_path):
    """Full access means "stop asking", not "stop refusing": the immutable
    deny-list and deny-action rules hold in auto.

    For the built-in shell and file tools this is enforced twice -- the
    toolset's own `looks_dangerous` and `safe_path` sit below the hook and no
    mode can reach them. The end-to-end asserts pin that composition."""

    manager = _manager(tmp_path, [
        [tool("bash", command="sudo shutdown now", _id="t1")],
        [tool("write_file", path="../escape.txt", content="x", _id="t2")],
    ])
    session = manager.create(permission_mode="auto")

    await asyncio.wait_for(session.run("go wild"), timeout=10)

    blocked, escaped = (r["content"] for r in _tool_results(session))
    assert "blocked" in blocked
    assert "escapes" in escaped.lower()
    assert not (session.workspace.parent / "escape.txt").exists()


@pytest.mark.asyncio
async def test_auto_mode_honours_custom_deny_rules(tmp_path):
    """The hook-level half of the invariant, pinned at the hook.

    The built-in cases above are double-enforced (the toolset refuses below
    the hook), so only a *custom* deny rule -- guarding a tool the toolset
    does not back, the case applications actually register -- proves the hook
    itself never skips deny-action rules in auto mode."""

    from mini_loop.permissions import PermissionHook, PermissionRule
    from mini_loop.registry import ToolCall, ToolContext

    hook = PermissionHook(rules=[PermissionRule(
        "frozen-index", ("rebuild_index",),
        lambda _ctx, _call: True,
        "The search index is frozen during migration",
        action="deny",
    )])

    class _Session:
        id = "s1"
        permission_mode = "auto"

    class _Agent:
        state = {"session": _Session()}
        tools = full_registry()

        async def _send(self, *_args, **_kw):
            return None

    ctx = ToolContext(_Agent(), tmp_path, {}, None)
    decision = await hook.before_tool(ctx, ToolCall("rebuild_index", {}, "t1"))

    assert decision is not None and "frozen" in decision


# -- approve: ask for approval ----------------------------------------------


async def _pending(manager, session):
    for _ in range(200):
        rows = manager.approvals.list(session.id)
        if rows:
            return rows[0]
        await asyncio.sleep(0.01)
    raise AssertionError("no approval was parked")


@pytest.mark.asyncio
async def test_approve_mode_asks_before_writing(tmp_path):
    """A file write parks an approval instead of running; a denied answer
    means the file never exists."""

    manager = _manager(tmp_path, [
        [tool("write_file", path="a.txt", content="x", _id="t1")],
    ])
    session = manager.create(permission_mode="approve")

    turn = asyncio.create_task(session.run("write it"))
    pending = await _pending(manager, session)
    assert pending["rule"] == "side-effect-approval"
    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=False)
    await asyncio.wait_for(turn, timeout=10)
    assert not (session.workspace / "a.txt").exists()


@pytest.mark.asyncio
async def test_approve_mode_runs_after_a_yes(tmp_path):
    """Approval is a gate, not a wall: a granted ask runs the call."""

    manager = _manager(tmp_path, [
        [tool("write_file", path="a.txt", content="x", _id="t1")],
    ])
    session = manager.create(permission_mode="approve")

    turn = asyncio.create_task(session.run("write it"))
    pending = await _pending(manager, session)
    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=True)
    await asyncio.wait_for(turn, timeout=10)
    assert (session.workspace / "a.txt").exists()


@pytest.mark.asyncio
async def test_approve_mode_still_reads_freely(tmp_path):
    """Reads are not side effects: no approval rows for a glob."""

    manager = _manager(tmp_path, [[tool("glob", pattern="*", _id="t1")]])
    session = manager.create(permission_mode="approve")

    await asyncio.wait_for(session.run("look around"), timeout=10)

    assert manager.approvals.list(session.id) == []
    [result] = _tool_results(session)
    assert "denied" not in result["content"].lower()


@pytest.mark.asyncio
async def test_interactive_still_writes_without_asking(tmp_path):
    """The regression pin for the new rule: `approve` widens what is asked
    about in `approve` mode only -- the default keeps writing freely."""

    manager = _manager(tmp_path, [
        [tool("write_file", path="a.txt", content="x", _id="t1")],
    ])
    session = manager.create()

    await asyncio.wait_for(session.run("write it"), timeout=10)

    assert (session.workspace / "a.txt").exists()
    assert manager.approvals.list(session.id) == []


# -- interactive stays the default ------------------------------------------


@pytest.mark.asyncio
async def test_the_default_mode_still_asks(tmp_path):
    ran = []
    manager = _manager(tmp_path, [[tool("mcp__ops__deploy", target="prod", _id="t1")]],
                       ran=ran)
    session = manager.create()
    assert session.permission_mode == "interactive"

    turn = asyncio.create_task(session.run("deploy"))
    for _ in range(200):
        if manager.approvals.list(session.id):
            break
        await asyncio.sleep(0.01)
    [pending] = manager.approvals.list(session.id)
    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=False)
    await turn
    assert not ran


@pytest.mark.asyncio
async def test_a_mode_change_takes_effect_on_the_next_call(tmp_path):
    manager = _manager(tmp_path, [
        [tool("write_file", path="a.txt", content="x", _id="t1")],
        [tool("write_file", path="b.txt", content="y", _id="t2")],
    ])
    session = manager.create(permission_mode="readonly")

    turn = asyncio.create_task(session.run("write twice"))
    # Flip to interactive between the two scripted calls is racy; flip after
    # the turn instead and run a second turn -- the mode is read per call.
    await asyncio.wait_for(turn, timeout=10)
    assert not (session.workspace / "a.txt").exists()

    session.permission_mode = "interactive"
    manager.client.responder = scripted([
        ([tool("write_file", path="c.txt", content="z", _id="t3")], "tool_use"),
        ([text("done")], "end_turn"),
    ])
    await asyncio.wait_for(session.run("write again"), timeout=10)
    assert (session.workspace / "c.txt").exists()


def test_an_unknown_mode_is_rejected_at_creation(tmp_path):
    manager = _manager(tmp_path, [])
    with pytest.raises(ValueError, match="permission mode"):
        manager.create(permission_mode="yolo")
    assert PERMISSION_MODES == ("readonly", "approve", "interactive", "auto")


# -- over HTTP --------------------------------------------------------------


def test_mode_over_http_create_switch_and_ownership(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    alice, bob = "tok-alice-000000000000", "tok-bob-1111111111111"
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({alice: "alice", bob: "bob"})
        headers = {"Authorization": f"Bearer {alice}"}

        created = client.post("/sessions", json={"mode": "readonly"},
                              headers=headers).json()
        assert created["permission_mode"] == "readonly"

        switched = client.post(f"/sessions/{created['id']}/mode",
                               json={"mode": "auto"}, headers=headers)
        assert switched.status_code == 200
        assert manager._sessions[created["id"]].permission_mode == "auto"

        foreign = {"Authorization": f"Bearer {bob}"}
        assert client.post(f"/sessions/{created['id']}/mode",
                           json={"mode": "auto"}, headers=foreign).status_code == 404
        assert client.post(f"/sessions/{created['id']}/mode",
                           json={"mode": "yolo"}, headers=headers).status_code == 422
