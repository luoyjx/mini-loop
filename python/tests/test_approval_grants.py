"""Approval-as-learning: a yes can generalize, within hard limits.

Codex's `prefix_rule` mechanism, adapted: the human resolving an approval
may say `remember=True`, recording the pending's grant candidate for the
rest of the session -- shell commands anchor on exactly their first two
tokens, other tools on the tool name. Later calls that compute the same
candidate skip the ask.

The limits are the point:

* a banned head (`rm`, `sudo`, `bash`, `python`, ...) is never recorded --
  a prefix starting with an interpreter or a deleter covers effectively
  unbounded behavior, more than the human just reviewed;
* grants are runtime-only and die with the session (same doctrine as
  permission_mode: a restart asks again);
* `remember` on a deny is ignored -- only a yes can generalize.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.approvals import (
    GRANT_BANNED_HEADS,
    grant_banned,
    grant_candidate,
)
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


def _manager(tmp_path, calls, ran=None):
    registry = full_registry()
    registry.register(_external_tool(ran if ran is not None else []))
    client = FakeAsyncAnthropic(responder=scripted(
        [(blocks, "tool_use") for blocks in calls] + [([text("done")], "end_turn")]
    ))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return SessionManager(settings, client, tool_registry=registry)


async def _resolve_next(manager, session, *, allowed, remember=False):
    # A just-resolved pending lingers in the broker's map until the parked
    # ask() wakes and pops it; resolve() returning False is how the broker
    # says "already answered", so skip those rows rather than mistaking one
    # for the next genuine ask.
    stale: set = set()
    for _ in range(500):
        rows = [r for r in manager.approvals.list(session.id)
                if r["approval_id"] not in stale]
        if rows:
            row = rows[0]
            if manager.approvals.resolve(
                row["approval_id"], session_id=session.id,
                allowed=allowed, remember=remember,
            ):
                return row
            stale.add(row["approval_id"])
            continue
        await asyncio.sleep(0.01)
    raise AssertionError("no approval was parked")


# -- the candidate itself ----------------------------------------------------


def test_the_candidate_is_two_tokens_for_shell_and_the_name_otherwise():
    assert grant_candidate("bash", {"command": "git reset --hard HEAD~1"}) == (
        "bash", "git", "reset")
    assert grant_candidate("bash", {"command": "make"}) is None, (
        "a one-token command would be a head grant"
    )
    assert grant_candidate("mcp__ops__deploy", {"target": "prod"}) == (
        "mcp__ops__deploy",)
    assert grant_banned(("bash", "rm", "-rf"))
    assert grant_banned(("bash", "sudo", "reboot"))
    assert not grant_banned(("bash", "git", "reset"))
    assert not grant_banned(("mcp__ops__deploy",))
    assert "python" in GRANT_BANNED_HEADS and "curl" in GRANT_BANNED_HEADS


# -- remembered grants skip the ask ------------------------------------------


@pytest.mark.asyncio
async def test_an_allow_with_remember_skips_the_next_equivalent_ask(tmp_path):
    ran = []
    manager = _manager(tmp_path, [
        [tool("mcp__ops__deploy", target="staging", _id="t1")],
        [tool("mcp__ops__deploy", target="prod", _id="t2")],
    ], ran=ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy twice"))
    row = await _resolve_next(manager, session, allowed=True, remember=True)
    assert row["grant_candidate"] == ["mcp__ops__deploy"], (
        "the approver was not shown what remember would grant"
    )
    await asyncio.wait_for(turn, timeout=10)

    assert ran == ["staging", "prod"], "the second call still parked or denied"
    assert manager.approvals.list(session.id) == []


@pytest.mark.asyncio
async def test_without_remember_every_call_asks_again(tmp_path):
    ran = []
    manager = _manager(tmp_path, [
        [tool("mcp__ops__deploy", target="staging", _id="t1")],
        [tool("mcp__ops__deploy", target="prod", _id="t2")],
    ], ran=ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy twice"))
    await _resolve_next(manager, session, allowed=True, remember=False)
    await _resolve_next(manager, session, allowed=True, remember=False)
    await asyncio.wait_for(turn, timeout=10)
    assert ran == ["staging", "prod"]


@pytest.mark.asyncio
async def test_a_banned_head_is_allowed_once_but_never_remembered(tmp_path):
    """`rm -rf build` twice: the first yes with remember runs the command but
    refuses the rule; the second call parks again."""

    manager = _manager(tmp_path, [
        [tool("bash", command="rm -rf build", _id="t1")],
        [tool("bash", command="rm -rf dist", _id="t2")],
    ])
    session = manager.create()

    turn = asyncio.create_task(session.run("clean up"))
    await _resolve_next(manager, session, allowed=True, remember=True)
    # The second call must park -- the grant was refused.
    second = await _resolve_next(manager, session, allowed=False)
    assert second["tool"] == "bash"
    await asyncio.wait_for(turn, timeout=10)
    assert manager.approvals._grants.get(session.id, set()) == set()


@pytest.mark.asyncio
async def test_remember_on_a_deny_grants_nothing(tmp_path):
    ran = []
    manager = _manager(tmp_path, [
        [tool("mcp__ops__deploy", target="prod", _id="t1")],
    ], ran=ran)
    session = manager.create()

    turn = asyncio.create_task(session.run("deploy"))
    await _resolve_next(manager, session, allowed=False, remember=True)
    await asyncio.wait_for(turn, timeout=10)
    assert not ran
    assert manager.approvals._grants.get(session.id, set()) == set()


@pytest.mark.asyncio
async def test_grants_are_scoped_to_their_session(tmp_path):
    ran = []
    manager = _manager(tmp_path, [
        [tool("mcp__ops__deploy", target="staging", _id="t1")],
        [tool("mcp__ops__deploy", target="prod", _id="t2")],
    ], ran=ran)
    first = manager.create()
    turn = asyncio.create_task(first.run("deploy"))
    await _resolve_next(manager, first, allowed=True, remember=True)
    await asyncio.wait_for(turn, timeout=10)

    manager.client.responder = scripted([
        ([tool("mcp__ops__deploy", target="second", _id="t3")], "tool_use"),
        ([text("done")], "end_turn"),
    ])
    second = manager.create()
    turn2 = asyncio.create_task(second.run("deploy"))
    row = await _resolve_next(manager, second, allowed=False)
    assert row["tool"] == "mcp__ops__deploy", (
        "the second session inherited another conversation's grant"
    )
    await asyncio.wait_for(turn2, timeout=10)


@pytest.mark.asyncio
async def test_deleting_the_session_drops_its_grants(tmp_path):
    ran = []
    manager = _manager(tmp_path, [
        [tool("mcp__ops__deploy", target="staging", _id="t1")],
    ], ran=ran)
    session = manager.create()
    turn = asyncio.create_task(session.run("deploy"))
    await _resolve_next(manager, session, allowed=True, remember=True)
    await asyncio.wait_for(turn, timeout=10)
    assert manager.approvals._grants.get(session.id)

    manager.approvals.cancel_session(session.id)
    assert manager.approvals._grants.get(session.id) is None


# -- the model's own proposal (approval_prefix) ------------------------------


def test_the_proposal_gate_admits_only_honest_prefixes():
    from mini_loop.approvals import proposed_candidate

    honest = {"command": "git reset --hard HEAD",
              "approval_prefix": ["git", "reset", "--hard"]}
    assert proposed_candidate("bash", honest) == ("bash", "git", "reset", "--hard")

    lying = {"command": "rm -rf build", "approval_prefix": ["git", "pull"]}
    assert proposed_candidate("bash", lying) is None

    head_grant = {"command": "make test", "approval_prefix": ["make"]}
    assert proposed_candidate("bash", head_grant) is None, "one token = head grant"

    banned = {"command": "chmod -R 755 x", "approval_prefix": ["chmod", "-R"]}
    assert proposed_candidate("bash", banned) is None

    too_long = {"command": "git log --oneline -n 5 --graph --all extra",
                "approval_prefix": ["git", "log", "--oneline", "-n", "5",
                                    "--graph", "--all"]}
    assert proposed_candidate("bash", too_long) is None

    not_a_list = {"command": "git pull", "approval_prefix": "git pull"}
    assert proposed_candidate("bash", not_a_list) is None
    assert proposed_candidate("mcp__ops__deploy", honest) is None, (
        "proposals are a shell concept; other tools grant by name"
    )


@pytest.mark.asyncio
async def test_an_honest_proposal_is_shown_recorded_and_matched(tmp_path):
    """A 3-token model proposal rides the pending, is ratified with
    remember=True, and covers a later command sharing that 3-token prefix --
    variable-length matching, longer than the harness default."""

    manager = _manager(tmp_path, [
        [tool("bash", command="git reset --hard HEAD",
              approval_prefix=["git", "reset", "--hard"], _id="t1")],
        [tool("bash", command="git reset --hard HEAD~2", _id="t2")],
    ])
    session = manager.create()

    turn = asyncio.create_task(session.run("undo commits"))
    row = await _resolve_next(manager, session, allowed=True, remember=True)
    assert row["grant_candidate"] == ["bash", "git", "reset", "--hard"]
    assert row["grant_proposed"] is True
    await asyncio.wait_for(turn, timeout=10)

    assert manager.approvals.list(session.id) == [], (
        "the second command parked despite a covering grant"
    )
    assert ("bash", "git", "reset", "--hard") in manager.approvals._grants[session.id]


@pytest.mark.asyncio
async def test_a_lying_proposal_falls_back_to_the_default(tmp_path):
    """approval_prefix that is not the command's own leading words is
    ignored: the pending shows the harness default, unmarked as proposed."""

    manager = _manager(tmp_path, [
        [tool("bash", command="rm -rf build",
              approval_prefix=["git", "pull"], _id="t1")],
    ])
    session = manager.create()

    turn = asyncio.create_task(session.run("clean"))
    row = await _resolve_next(manager, session, allowed=True, remember=True)
    assert row["grant_candidate"] == ["bash", "rm", "-rf"], (
        "the lying proposal was taken at its word"
    )
    assert row["grant_proposed"] is False
    await asyncio.wait_for(turn, timeout=10)
    # And the default is rm-headed, so remember still refused it.
    assert manager.approvals._grants.get(session.id, set()) == set()


# -- over HTTP ---------------------------------------------------------------


def test_remember_rides_the_approvals_route(tmp_path):
    """The route plumbs `remember` through to the broker: a seeded pending
    resolved with remember=True lands its candidate in the session's grants,
    and the listing shows the candidate so the approver's yes is informed."""

    import time as _time

    from fastapi.testclient import TestClient

    from mini_loop.approvals import PendingApproval
    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager, settings=settings)
    with TestClient(app) as client:
        sid = client.post("/sessions", json={}).json()["id"]
        pending = PendingApproval(
            approval_id="apr_seeded000001", session_id=sid,
            tool="mcp__ops__deploy", rule="external-action",
            message="Tool acts outside this machine",
            input_preview='{"target": "prod"}', created_at=_time.time(),
            future=asyncio.new_event_loop().create_future(),
            grant_candidate=("mcp__ops__deploy",),
        )
        manager.approvals._pending[pending.approval_id] = pending

        [row] = client.get(f"/sessions/{sid}/approvals").json()["approvals"]
        assert row["grant_candidate"] == ["mcp__ops__deploy"]

        resolved = client.post(
            f"/sessions/{sid}/approvals/{pending.approval_id}",
            json={"decision": "allow", "remember": True},
        )
        assert resolved.status_code == 200
        assert pending.future.result() is True
        assert ("mcp__ops__deploy",) in manager.approvals._grants[sid]
